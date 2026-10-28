package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func testAPI(t *testing.T, handler func(*http.Request) (int, any)) (githubAPI, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		code, response := handler(r)
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return githubAPI{server.URL, "test", server.Client()}, &requests
}

func makePR() pull {
	p := pull{Number: 42, State: "open", User: prUser{Login: "attacker"}}
	p.Base.Ref, p.Base.SHA, p.Base.Repo.FullName = "master", shaB, repository
	p.Head.SHA, p.Head.Repo.FullName = shaA, repository
	return p
}

func TestPRClass(t *testing.T) {
	for _, tc := range []struct {
		name     string
		edit     func(*pull)
		excluded bool
		bad      bool
	}{
		{"ordinary", func(*pull) {}, false, false},
		{"spoofed bot branch and name", func(p *pull) { p.User.Login = "release-please[bot]" }, false, false},
		{"fork", func(p *pull) { p.Head.Repo.FullName = "someone/analytics-go" }, true, false},
		{"dependabot", func(p *pull) { p.User.Login = "dependabot[bot]" }, true, false},
		{"unsupported base", func(p *pull) { p.Base.Ref = "other" }, false, true},
		{"closed", func(p *pull) { p.State = "closed" }, false, true},
		{"missing candidate", func(p *pull) { p.Head.SHA = "" }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := makePR()
			tc.edit(&p)
			excluded, err := prClass(p)
			if (err != nil) != tc.bad || excluded != tc.excluded {
				t.Fatalf("excluded=%t err=%v", excluded, err)
			}
		})
	}
}

func TestCurrentPRRejectsStaleAndUnsupported(t *testing.T) {
	p := makePR()
	a, _ := testAPI(t, func(r *http.Request) (int, any) {
		if strings.Contains(r.URL.Path, "/pulls/42") {
			return 200, p
		}
		return 200, map[string]any{"object": map[string]string{"sha": shaA}}
	})
	if _, _, err := a.currentPR(42, shaA); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.currentPR(42, shaB); err == nil {
		t.Fatal("stale merge passed")
	}
	p.Base.Ref = "main"
	if _, _, err := a.currentPR(42, shaA); err == nil {
		t.Fatal("unsupported PR passed")
	}
}

func TestPRGateResultsAndFreshness(t *testing.T) {
	p := makePR()
	event := prEvent{Number: 42}
	event.PullRequest.Number = 42
	event.PullRequest.Head.SHA, event.PullRequest.Base.SHA = shaA, shaB
	event.PullRequest.Head.Repo.FullName, event.PullRequest.Base.Repo.FullName = repository, repository
	event.PullRequest.User.Login = p.User.Login
	path := filepath.Join(t.TempDir(), "event.json")
	writeEvent := func() {
		data, _ := json.Marshal(event)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeEvent()
	ref := shaA
	a, _ := testAPI(t, func(r *http.Request) (int, any) {
		if strings.Contains(r.URL.Path, "/pulls/42") {
			return 200, p
		}
		return 200, map[string]any{"object": map[string]string{"sha": ref}}
	})
	env := map[string]string{"GITHUB_EVENT_PATH": path, "GITHUB_SHA": shaA, "GITHUB_REPOSITORY": repository, "SELECT_RESULT": "success", "VALIDATE_RESULT": "success", "ELIGIBLE": "true"}
	runGate := func(cmd string) error { return run([]string{cmd}, a, func(k string) string { return env[k] }) }
	if err := runGate("pr-gate"); err != nil {
		t.Fatal(err)
	}
	for _, result := range []string{"failure", "cancelled", "skipped"} {
		env["VALIDATE_RESULT"] = result
		if err := runGate("pr-gate"); err == nil {
			t.Fatalf("%s passed", result)
		}
	}
	env["VALIDATE_RESULT"] = "success"
	ref = shaB
	if err := runGate("pr-gate"); err == nil {
		t.Fatal("advanced merge ref passed")
	}
	ref = shaA
	p.Head.Repo.FullName = "fork/analytics-go"
	event.PullRequest.Head.Repo.FullName = p.Head.Repo.FullName
	writeEvent()
	env["VALIDATE_RESULT"], env["EXCLUDED"], env["ELIGIBLE"] = "skipped", "true", "false"
	if err := runGate("pr-gate"); err != nil {
		t.Fatal(err)
	}
	if err := runGate("pr-check"); err == nil {
		t.Fatal("fork dispatched")
	}
}

func files() map[string]string {
	return map[string]string{
		"go.mod":                        "module github.com/rudderlabs/analytics-go/v4\n\ngo 1.21\n",
		".release-please-manifest.json": `{".":"4.5.0"}`,
		"analytics.go":                  `package analytics` + "\n" + `const Version = "4.5.0" // x-release-please-version`,
		"CHANGELOG.md":                  "# Changelog\n\n## [4.5.0](https://github.com/rudderlabs/analytics-go/compare/v4.4.0...v4.5.0) (today)\n\nnotes\n\n## [4.4.0](old)\n",
	}
}

func TestReleaseFiles(t *testing.T) {
	for _, tc := range []struct {
		name, file, text string
		valid            bool
	}{
		{"valid", "", "", true},
		{"module", "go.mod", "module github.com/other/v4\n", false},
		{"manifest", ".release-please-manifest.json", `{".":"5.0.0"}`, false},
		{"SDK", "analytics.go", `package analytics
const Version = "4.4.0" // x-release-please-version`, false},
		{"spoofed version comment", "analytics.go", `package analytics
// const Version = "4.5.0" // x-release-please-version
const Version = "4.4.0" // x-release-please-version`, false},
		{"spoofed version string", "analytics.go", `package analytics
const note = "const Version = \"4.5.0\" // x-release-please-version"
const Version = "4.4.0"`, false},
		{"non-literal version", "analytics.go", `package analytics
const prefix = "4.5."
const Version = prefix + "0"`, false},
		{"wrong package", "analytics.go", `package other
const Version = "4.5.0"`, false},
		{"changelog", "CHANGELOG.md", "## [4.5.0](other)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := files()
			if tc.file != "" {
				f[tc.file] = tc.text
			}
			_, _, err := releaseFiles(f["go.mod"], f[".release-please-manifest.json"], f["analytics.go"], f["CHANGELOG.md"])
			if (err == nil) != tc.valid {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestReleaseCandidateAndPublish(t *testing.T) {
	p := makePR()
	p.Merged, p.MergeCommitSHA, p.Labels = true, shaA, []label{{"autorelease: pending"}}
	content := files()
	master, tagExists := shaA, false
	var tagSHA string
	a, requests := testAPI(t, func(r *http.Request) (int, any) {
		switch {
		case r.URL.Path == "/repos/"+repository+"/branches/master":
			return 200, map[string]any{"commit": map[string]string{"sha": master}}
		case strings.HasSuffix(r.URL.Path, "/commits/"+shaA+"/pulls"):
			return 200, []pull{p}
		case strings.HasSuffix(r.URL.Path, "/pulls/42"):
			return 200, p
		case strings.Contains(r.URL.Path, "/contents/"):
			if r.URL.Query().Get("ref") != shaA {
				t.Error("wrong file ref")
			}
			name := strings.TrimPrefix(r.URL.Path, "/repos/"+repository+"/contents/")
			return 200, map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(content[name])), "encoding": "base64", "sha": shaB}
		case strings.Contains(r.URL.Path, "/git/ref/tags/"):
			if tagExists {
				return 200, map[string]string{"ref": "refs/tags/v4.5.0"}
			}
			return 404, nil
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/git/refs"):
			var body struct {
				SHA string `json:"sha"`
				Ref string `json:"ref"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			tagSHA = body.SHA
			return 201, map[string]string{"ref": body.Ref}
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/releases"):
			return 201, map[string]string{"tag_name": "v4.5.0"}
		default:
			t.Errorf("unexpected request %s", r.URL)
			return 500, nil
		}
	})
	c, _, err := a.releaseCandidate(shaA)
	if err != nil || c != (candidate{shaA, "4.5.0", "v4.5.0"}) {
		t.Fatalf("candidate=%+v err=%v", c, err)
	}
	if err := a.publish(c); err != nil || tagSHA != shaA {
		t.Fatalf("tag target=%s err=%v", tagSHA, err)
	}
	if err := a.publish(candidate{shaA, "4.5.1", "v4.5.1"}); err == nil {
		t.Fatal("mismatched version and tag published")
	}
	count := len(*requests)
	master = shaB
	if err := a.publish(c); err == nil {
		t.Fatal("stale master published")
	}
	for _, request := range (*requests)[count:] {
		if strings.HasPrefix(request, "POST ") {
			t.Fatal("stale candidate wrote", request)
		}
	}
	master = shaA
	tagExists = true
	if _, _, err := a.releaseCandidate(shaA); err == nil {
		t.Fatal("existing tag passed")
	}
	p.Labels = nil
	if _, _, err := a.releaseCandidate(shaA); err == nil {
		t.Fatal("unlabelled bot/branch passed")
	}
	p.Labels = []label{{"autorelease: pending"}}
	p.MergeCommitSHA = shaB
	if _, _, err := a.releaseCandidate(shaA); err == nil {
		t.Fatal("unmatched PR merge commit passed")
	}
}

func TestSelectReleaseSkipsOnlyAbsentRelease(t *testing.T) {
	p := makePR()
	p.Merged, p.MergeCommitSHA = true, shaA
	a, _ := testAPI(t, func(r *http.Request) (int, any) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/branches/master"):
			return 200, map[string]any{"commit": map[string]string{"sha": shaA}}
		case strings.HasSuffix(r.URL.Path, "/commits/"+shaA+"/pulls"):
			return 200, []pull{p}
		case strings.HasSuffix(r.URL.Path, "/pulls/42"):
			return 200, p
		default:
			return 500, nil
		}
	})
	outputFile := filepath.Join(t.TempDir(), "output")
	f, err := os.Create(outputFile)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	t.Setenv("GITHUB_OUTPUT", outputFile)
	env := map[string]string{"GITHUB_REPOSITORY": repository, "GITHUB_REF": "refs/heads/master", "GITHUB_SHA": shaA}
	selectRelease := func() error { return run([]string{"select-release"}, a, func(k string) string { return env[k] }) }
	if err := selectRelease(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputFile)
	if err != nil || string(data) != "release=false\n" {
		t.Fatalf("non-release selection: %q %v", data, err)
	}
	p.Labels = []label{{"autorelease: pending"}}
	if err := selectRelease(); err == nil || errors.Is(err, ErrNoReleaseCandidate) {
		t.Fatalf("claimed but invalid release silently skipped: %v", err)
	}
}

func TestReleaseCommandsRejectManualRetry(t *testing.T) {
	a := githubAPI{}
	for _, cmd := range []string{"select-release", "verify-release", "publish"} {
		err := run([]string{cmd}, a, func(k string) string {
			if k == "GITHUB_REPOSITORY" {
				return repository
			}
			return "refs/heads/other"
		})
		if err == nil {
			t.Fatal(fmt.Sprint(cmd, " accepted non-master event"))
		}
	}
}
