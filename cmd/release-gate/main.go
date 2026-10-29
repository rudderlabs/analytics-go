// Command release-gate selects and verifies immutable Go release candidates.
// It makes no central E2E requests; the pinned candidate action owns validation.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const repository = "rudderlabs/analytics-go"

var (
	errNotFound    = errors.New("GitHub resource not found")
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

type githubAPI struct {
	url, token string
	client     *http.Client
}

func (a githubAPI) get(path string, result any) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, a.url+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("Cache-Control", "no-cache")
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub GET %s: status %d", path, res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(result)
}

func (a githubAPI) post(path string, body any, result any) error {
	return a.write(http.MethodPost, path, body, result, http.StatusCreated)
}

func (a githubAPI) write(method, path string, body any, result any, expected int) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), method, a.url+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != expected {
		return fmt.Errorf("GitHub %s %s: status %d", method, path, res.StatusCode)
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(result)
}

type prEvent struct {
	Number      int `json:"number"`
	PullRequest struct {
		Number int `json:"number"`
		Base   struct {
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
		Head struct {
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
}

type pull struct {
	Number         int     `json:"number"`
	State          string  `json:"state"`
	Merged         bool    `json:"merged"`
	MergeCommitSHA string  `json:"merge_commit_sha"`
	Base           prRef   `json:"base"`
	Head           prRef   `json:"head"`
	User           prUser  `json:"user"`
	Labels         []label `json:"labels"`
}

type prRef struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo struct {
		FullName string `json:"full_name"`
	} `json:"repo"`
}
type prUser struct {
	Login string `json:"login"`
}
type label struct {
	Name string `json:"name"`
}

type candidate struct {
	SHA, Version, Tag string
}

func prClass(p pull) (excluded bool, err error) {
	if p.Base.Repo.FullName != repository || p.Base.Ref != "master" || p.State != "open" || p.Number < 1 {
		return false, errors.New("unsupported PR base or state")
	}
	// GitHub withholds App credentials for fork and Dependabot PRs. Author
	// names other than Dependabot have no bearing on eligibility.
	if p.Head.Repo.FullName == "" || !shaPattern.MatchString(p.Head.SHA) || !shaPattern.MatchString(p.Base.SHA) {
		return false, errors.New("unsupported PR candidate")
	}
	return p.Head.Repo.FullName != repository || p.User.Login == "dependabot[bot]", nil
}

func (a githubAPI) mergeSHA(number int) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := a.get(fmt.Sprintf("/repos/%s/git/ref/pull/%d/merge", repository, number), &ref)
	if err != nil {
		return "", err
	}
	if !shaPattern.MatchString(ref.Object.SHA) {
		return "", errors.New("unsupported merge ref")
	}
	return ref.Object.SHA, nil
}

func (a githubAPI) currentPR(number int, eventSHA string) (pull, bool, error) {
	var p pull
	if err := a.get(fmt.Sprintf("/repos/%s/pulls/%d", repository, number), &p); err != nil {
		return p, false, err
	}
	if p.Number != number {
		return p, false, errors.New("PR number changed")
	}
	excluded, err := prClass(p)
	if err != nil {
		return p, false, err
	}
	sha, err := a.mergeSHA(number)
	if err != nil {
		return p, false, err
	}
	if !shaPattern.MatchString(eventSHA) || sha != eventSHA {
		return p, false, errors.New("stale PR merge SHA")
	}
	master, err := a.masterSHA()
	if err != nil {
		return p, false, err
	}
	var commit struct {
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := a.get("/repos/"+repository+"/git/commits/"+sha, &commit); err != nil {
		return p, false, err
	}
	if p.Base.SHA != master || len(commit.Parents) != 2 ||
		commit.Parents[0].SHA != master || commit.Parents[1].SHA != p.Head.SHA {
		return p, false, errors.New("PR merge parents no longer match current master and head")
	}
	return p, excluded, nil
}

func (a githubAPI) masterSHA() (string, error) {
	var branch struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	err := a.get("/repos/"+repository+"/branches/master", &branch)
	if err != nil {
		return "", err
	}
	if !shaPattern.MatchString(branch.Commit.SHA) {
		return "", errors.New("invalid master SHA")
	}
	return branch.Commit.SHA, nil
}

func (a githubAPI) file(sha, path string) (string, error) {
	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		SHA      string `json:"sha"`
	}
	if err := a.get("/repos/"+repository+"/contents/"+path+"?ref="+sha, &blob); err != nil {
		return "", err
	}
	if blob.Encoding != "base64" || !shaPattern.MatchString(blob.SHA) {
		return "", errors.New("invalid release file response")
	}
	content, err := base64.StdEncoding.DecodeString(blob.Content)
	if err != nil {
		return "", err
	}
	if len(content) > 1<<20 {
		return "", errors.New("release file too large")
	}
	return string(content), nil
}

func sdkVersion(source string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "analytics.go", source, 0)
	if err != nil {
		return "", err
	}
	if file.Name.Name != "analytics" {
		return "", errors.New("wrong SDK package")
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if name.Name != "Version" {
					continue
				}
				if i >= len(value.Values) {
					return "", errors.New("SDK version must be an explicit string literal")
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return "", errors.New("SDK version must be a string literal")
				}
				return strconv.Unquote(literal.Value)
			}
		}
	}
	return "", errors.New("SDK version constant missing")
}

func releaseFiles(module, manifest, sdk, changelog string) (string, string, error) {
	if !strings.HasPrefix(module, "module github.com/rudderlabs/analytics-go/v4\n") {
		return "", "", errors.New("wrong Go module path")
	}
	var versions map[string]string
	if err := json.Unmarshal([]byte(manifest), &versions); err != nil {
		return "", "", err
	}
	v := versions["."]
	if len(versions) != 1 || !versionPattern.MatchString(v) || !strings.HasPrefix(v, "4.") {
		return "", "", errors.New("unsupported release manifest")
	}
	actual, err := sdkVersion(sdk)
	if err != nil || actual != v {
		return "", "", errors.New("SDK version mismatch")
	}
	tag := "v" + v
	start := "## [" + v + "]("
	index := strings.Index(changelog, start)
	if index == -1 || strings.Index(changelog[:index], "\n## [") != -1 {
		return "", "", errors.New("release not at top of changelog")
	}
	end := strings.Index(changelog[index+len(start):], "\n## [")
	notes := changelog[index:]
	if end >= 0 {
		notes = changelog[index : index+len(start)+end]
	}
	if !strings.Contains(strings.SplitN(changelog[index:], "\n", 2)[0], tag+")") {
		return "", "", errors.New("changelog tag mismatch")
	}
	return v, notes, nil
}

// ErrNoReleaseCandidate means the pushed commit has no release PR. A claimed
// release that fails file, identity, or tag verification must still fail closed.
var ErrNoReleaseCandidate = errors.New("no merged release PR with autorelease: pending label")

func (a githubAPI) releaseCandidate(sha string) (candidate, string, error) {
	var c candidate
	if !shaPattern.MatchString(sha) {
		return c, "", errors.New("invalid source SHA")
	}
	master, err := a.masterSHA()
	if err != nil {
		return c, "", err
	}
	if master != sha {
		return c, "", errors.New("master advanced during validation")
	}
	if _, err := a.releasePR(sha); err != nil {
		return c, "", err
	}
	v, notes, err := a.releaseMetadata(sha)
	if err != nil {
		return c, "", err
	}
	c = candidate{sha, v, "v" + v}
	var tag struct {
		Ref string `json:"ref"`
	}
	err = a.get("/repos/"+repository+"/git/ref/tags/"+c.Tag, &tag)
	if err == nil {
		return c, "", errors.New("tag already exists")
	}
	if !errors.Is(err, errNotFound) {
		return c, "", err
	}
	return c, notes, nil
}

func (a githubAPI) releasePR(sha string) (pull, error) {
	var selected pull
	for page := 1; page <= 10; page++ {
		var prs []pull
		if err := a.get(fmt.Sprintf("/repos/%s/commits/%s/pulls?per_page=100&page=%d", repository, sha, page), &prs); err != nil {
			return selected, err
		}
		for _, listed := range prs {
			if !hasReleaseLabel(listed.Labels) {
				continue
			}
			// Commit-associated PR responses omit the merged boolean.
			var detail pull
			if err := a.get(fmt.Sprintf("/repos/%s/pulls/%d", repository, listed.Number), &detail); err != nil {
				return selected, err
			}
			if !detail.Merged {
				continue
			}
			if detail.Number != listed.Number || detail.MergeCommitSHA != sha || !isReleasePR(detail) ||
				!hasReleaseLabel(detail.Labels) || selected.Number != 0 {
				return selected, errors.New("invalid or ambiguous merged release PR")
			}
			selected = detail
		}
		if len(prs) < 100 {
			if selected.Number == 0 {
				return selected, ErrNoReleaseCandidate
			}
			return selected, nil
		}
	}
	return selected, errors.New("release PR scan exceeded pagination limit")
}

func isReleasePR(p pull) bool {
	return p.Base.Ref == "master" && p.Base.Repo.FullName == repository && p.Head.Repo.FullName == repository
}

func hasReleaseLabel(labels []label) bool {
	for _, l := range labels {
		if l.Name == "autorelease: pending" {
			return true
		}
	}
	return false
}

func (a githubAPI) verifyEvidence(c candidate, report, validatedSHA string) error {
	var evidence struct {
		Schema     int    `json:"schema_version"`
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		SDK        string `json:"sdk"`
		Selection  string `json:"selection"`
		Package    string `json:"package"`
		Artifact   struct {
			SourceTree string `json:"source_tree"`
		} `json:"artifact"`
		Version  string `json:"version"`
		Run      int64  `json:"central_run_id"`
		Attempt  int    `json:"central_run_attempt"`
		Request  string `json:"request_id"`
		Delivery bool   `json:"delivery_passed"`
		Restored bool   `json:"restoration_verified"`
		Unlocked bool   `json:"lock_released"`
	}
	if len(report) > 1<<20 || json.Unmarshal([]byte(report), &evidence) != nil || validatedSHA != c.SHA ||
		evidence.Schema != 1 || evidence.Repository != repository || evidence.Commit != c.SHA ||
		evidence.SDK != "go" || evidence.Selection != "candidate" ||
		evidence.Package != "github.com/rudderlabs/analytics-go/v4" || evidence.Version != c.Version ||
		evidence.Run <= 0 || evidence.Attempt <= 0 || evidence.Request == "" ||
		!evidence.Delivery || !evidence.Restored || !evidence.Unlocked || !shaPattern.MatchString(evidence.Artifact.SourceTree) {
		return errors.New("missing or inconsistent verified full candidate evidence")
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := a.get("/repos/"+repository+"/git/commits/"+c.SHA, &commit); err != nil {
		return err
	}
	if commit.Tree.SHA != evidence.Artifact.SourceTree {
		return errors.New("validated source tree differs from release commit")
	}
	return nil
}

func (a githubAPI) releaseMetadata(sha string) (string, string, error) {
	files := make([]string, 4)
	for i, path := range []string{"go.mod", ".release-please-manifest.json", "analytics.go", "CHANGELOG.md"} {
		var err error
		files[i], err = a.file(sha, path)
		if err != nil {
			return "", "", err
		}
	}
	return releaseFiles(files[0], files[1], files[2], files[3])
}

func (a githubAPI) pendingReleases() ([]pull, error) {
	var pending []pull
	for page := 1; ; page++ {
		var prs []pull
		if err := a.get(fmt.Sprintf("/repos/%s/pulls?state=closed&base=master&sort=updated&direction=desc&per_page=100&page=%d", repository, page), &prs); err != nil {
			return nil, err
		}
		for _, listed := range prs {
			if !hasReleaseLabel(listed.Labels) {
				continue
			}
			var detail pull
			if err := a.get(fmt.Sprintf("/repos/%s/pulls/%d", repository, listed.Number), &detail); err != nil {
				return nil, err
			}
			if !detail.Merged {
				continue
			}
			if !hasReleaseLabel(detail.Labels) {
				continue
			}
			if detail.Number != listed.Number || !isReleasePR(detail) || !shaPattern.MatchString(detail.MergeCommitSHA) {
				return nil, errors.New("invalid pending release identity")
			}
			pending = append(pending, detail)
		}
		if len(prs) < 100 {
			return pending, nil
		}
	}
}

// A later master push supersedes an untagged candidate, never an existing tag.
// Release Please then builds a fresh reviewed PR from the last published version.
func (a githubAPI) prepareReleasePR(sha string) error {
	master, err := a.masterSHA()
	if err != nil {
		return err
	}
	if !shaPattern.MatchString(sha) || master != sha {
		return errors.New("stale release PR generation event")
	}
	var published struct {
		Tag         string `json:"tag_name"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
		PublishedAt string `json:"published_at"`
	}
	if err := a.get("/repos/"+repository+"/releases/latest", &published); err != nil {
		return err
	}
	baseline := strings.TrimPrefix(published.Tag, "v")
	if published.Tag != "v"+baseline || !versionPattern.MatchString(baseline) ||
		!strings.HasPrefix(baseline, "4.") || published.Draft || published.Prerelease || published.PublishedAt == "" {
		return errors.New("invalid published release baseline")
	}
	pending, err := a.pendingReleases()
	if err != nil {
		return err
	}
	tags := make([]string, len(pending))
	for i, p := range pending {
		if p.MergeCommitSHA == sha {
			return errors.New("current release candidate must finish validation before recovery")
		}
		version, _, err := a.releaseMetadata(p.MergeCommitSHA)
		if err != nil {
			return err
		}
		tags[i] = "v" + version
		var comparison struct {
			Status string `json:"status"`
			Base   struct {
				SHA string `json:"sha"`
			} `json:"merge_base_commit"`
		}
		if err := a.get("/repos/"+repository+"/compare/"+p.MergeCommitSHA+"..."+sha, &comparison); err != nil {
			return err
		}
		if comparison.Status != "ahead" || comparison.Base.SHA != p.MergeCommitSHA {
			return errors.New("pending release is not an ancestor of current master")
		}
	}
	// Release Please must replace only unpublished changelog entries. Protect
	// every entry it will replace, even after a prior generation attempt failed.
	changelog, err := a.file(sha, "CHANGELOG.md")
	if err != nil {
		return err
	}
	foundBaseline := false
	for _, header := range regexp.MustCompile(`(?m)^## \[([0-9]+\.[0-9]+\.[0-9]+)\]\(`).FindAllStringSubmatch(changelog, -1) {
		if header[1] == baseline {
			foundBaseline = true
			break
		}
		tags = append(tags, "v"+header[1])
	}
	if !foundBaseline {
		return errors.New("published baseline is missing from changelog")
	}
	// Validate the whole plan before removing any label. Recheck each tag and
	// master immediately before mutation; workflow concurrency serializes writers.
	for _, tag := range tags {
		if err := a.requireAbsentTag(tag); err != nil {
			return err
		}
	}
	for i, p := range pending {
		current, err := a.masterSHA()
		if err != nil {
			return err
		}
		if current != sha {
			return errors.New("master advanced during recovery")
		}
		if err := a.requireAbsentTag(tags[i]); err != nil {
			return err
		}
		path := fmt.Sprintf("/repos/%s/issues/%d/labels/autorelease:%%20pending", repository, p.Number)
		if err := a.write(http.MethodDelete, path, nil, nil, http.StatusOK); err != nil {
			return err
		}
		fmt.Printf("Superseded unpublished release PR #%d at %s; replacement baseline is %s\n", p.Number, p.MergeCommitSHA, published.Tag)
	}
	if err := output("superseded", strconv.Itoa(len(pending))); err != nil {
		return err
	}
	return output("baseline-version", baseline)
}

func (a githubAPI) requireAbsentTag(tag string) error {
	var ref any
	err := a.get("/repos/"+repository+"/git/ref/tags/"+tag, &ref)
	if errors.Is(err, errNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("release tag exists; finish its release and label recovery before generating another PR")
}

func (a githubAPI) publish(c candidate) error {
	verified, notes, err := a.releaseCandidate(c.SHA)
	if err != nil {
		return err
	}
	if verified != c {
		return errors.New("candidate identity changed")
	}
	p, err := a.releasePR(c.SHA)
	if err != nil {
		return err
	}
	master, err := a.masterSHA()
	if err != nil {
		return err
	}
	if master != c.SHA {
		return errors.New("master advanced before tag creation")
	}
	// Tag creation is the Go module publication boundary. Never retry or move an
	// existing tag if GitHub Release creation fails; recover it manually.
	var ref any
	if err := a.post("/repos/"+repository+"/git/refs", map[string]string{"ref": "refs/tags/" + c.Tag, "sha": c.SHA}, &ref); err != nil {
		return err
	}
	var release any
	if err := a.post("/repos/"+repository+"/releases", map[string]any{"tag_name": c.Tag, "target_commitish": c.SHA, "name": c.Tag, "body": notes, "draft": false, "prerelease": false}, &release); err != nil {
		return err
	}
	labels := fmt.Sprintf("/repos/%s/issues/%d/labels", repository, p.Number)
	if err := a.write(http.MethodPost, labels, map[string]any{"labels": []string{"autorelease: tagged"}}, nil, http.StatusOK); err != nil {
		return err
	}
	return a.write(http.MethodDelete, labels+"/autorelease:%20pending", nil, nil, http.StatusOK)
}

func output(key, value string) error {
	f, err := os.OpenFile(filepath.Clean(os.Getenv("GITHUB_OUTPUT")), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s=%s\n", key, value)
	return err
}

func run(args []string, a githubAPI, env func(string) string) error {
	if len(args) != 1 {
		return errors.New("expected one command")
	}
	switch args[0] {
	case "pr-select", "pr-check", "pr-gate":
		data, err := os.ReadFile(env("GITHUB_EVENT_PATH"))
		if err != nil {
			return err
		}
		var event prEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		if event.Number != event.PullRequest.Number || event.Number < 1 || env("GITHUB_REPOSITORY") != repository {
			return errors.New("invalid PR event")
		}
		p, excluded, err := a.currentPR(event.Number, env("GITHUB_SHA"))
		if err != nil {
			return err
		}
		if p.Head.SHA != event.PullRequest.Head.SHA || p.Base.SHA != event.PullRequest.Base.SHA ||
			p.Head.Repo.FullName != event.PullRequest.Head.Repo.FullName || p.Base.Repo.FullName != event.PullRequest.Base.Repo.FullName ||
			p.User.Login != event.PullRequest.User.Login {
			return errors.New("PR identity, head or base changed")
		}
		if args[0] == "pr-select" {
			if err := output("eligible", fmt.Sprint(!excluded)); err != nil {
				return err
			}
			return output("excluded", fmt.Sprint(excluded))
		}
		if args[0] == "pr-check" {
			if excluded {
				return errors.New("excluded PR cannot request a central run")
			}
			return nil
		}
		if env("SELECT_RESULT") != "success" {
			return errors.New("PR selection failed or skipped")
		}
		if excluded && env("EXCLUDED") == "true" && env("VALIDATE_RESULT") == "skipped" {
			return nil
		}
		if !excluded && env("ELIGIBLE") == "true" && env("VALIDATE_RESULT") == "success" {
			return nil
		}
		return errors.New("candidate validation failed, cancelled or skipped unexpectedly")
	case "select-release", "verify-release", "verify-evidence", "prepare-release-pr", "publish":
		if env("GITHUB_REPOSITORY") != repository || env("GITHUB_REF") != "refs/heads/master" {
			return errors.New("not a master release event")
		}
		if args[0] == "prepare-release-pr" {
			return a.prepareReleasePR(env("GITHUB_SHA"))
		}
		sha := env("GITHUB_SHA")
		if args[0] != "select-release" {
			sha = env("RELEASE_SHA")
		}
		if args[0] == "verify-evidence" {
			return a.verifyEvidence(candidate{sha, env("RELEASE_VERSION"), env("RELEASE_TAG")}, env("CANDIDATE_REPORT"), env("VALIDATED_SHA"))
		}
		if args[0] == "publish" {
			if sha != env("GITHUB_SHA") {
				return errors.New("publication source differs from triggering commit")
			}
			c := candidate{sha, env("RELEASE_VERSION"), env("RELEASE_TAG")}
			if err := a.verifyEvidence(c, env("CANDIDATE_REPORT"), env("VALIDATED_SHA")); err != nil {
				return err
			}
			return a.publish(c)
		}
		c, _, err := a.releaseCandidate(sha)
		if args[0] == "select-release" && errors.Is(err, ErrNoReleaseCandidate) {
			return output("release", "false")
		}
		if err != nil {
			return err
		}
		if args[0] == "select-release" {
			if err := output("release", "true"); err != nil {
				return err
			}
			if err := output("sha", c.SHA); err != nil {
				return err
			}
			if err := output("version", c.Version); err != nil {
				return err
			}
			return output("tag", c.Tag)
		}
		if sha != env("GITHUB_SHA") || c != (candidate{sha, env("RELEASE_VERSION"), env("RELEASE_TAG")}) {
			return errors.New("release candidate changed")
		}
		return nil
	default:
		return errors.New("unknown release-gate command")
	}
}

func main() {
	url := os.Getenv("GITHUB_API_URL")
	if url == "" {
		url = "https://api.github.com"
	}
	a := githubAPI{url: url, token: os.Getenv("GITHUB_TOKEN"), client: &http.Client{Timeout: 15 * time.Second}}
	if err := run(os.Args[1:], a, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
