package analytics

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedWorkflowExactVersionAndRetryIsolation(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/published-sdk-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		"types: [published]",
		"release_tag:", "module_version:",
		"package-version: ${{ steps.release.outputs.version }}",
		"suite: full",
		"permission-actions: write",
		"repositories: rudder-sdk-e2e-tests",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("published workflow is missing %q", required)
		}
	}
	parts := strings.SplitN(workflow, "\n  retry:", 2)
	if len(parts) != 2 {
		t.Fatal("missing validation-only retry job")
	}
	if !strings.Contains(parts[0], "\n  validate:\n") || !strings.Contains(parts[0], "runs-on: ubuntu-latest") {
		t.Fatal("release validation must use ubuntu-latest")
	}
	retry := strings.SplitN(parts[1], "\n  notify:", 2)
	if len(retry) != 2 {
		t.Fatal("missing release notification job")
	}
	for _, required := range []string{
		"if: github.event_name == 'workflow_dispatch'",
		"runs-on: ubuntu-latest",
		"MODULE_VERSION: ${{ inputs.module_version }}",
		"RELEASE_TAG: ${{ inputs.release_tag }}",
		"$GITHUB_REF",
		"gh api \"repos/$GITHUB_REPOSITORY/releases/tags/$RELEASE_TAG\"",
		".draft == false and .prerelease == false and .published_at != null",
		"package-version: ${{ steps.release.outputs.version }}",
		"suite: full",
	} {
		if !strings.Contains(retry[0], required) {
			t.Errorf("retry job is missing %q", required)
		}
	}
	for _, prohibited := range []string{"slackapi/", "release-please", "git push", "upload"} {
		if strings.Contains(retry[0], prohibited) {
			t.Errorf("retry job unexpectedly contains %q", prohibited)
		}
	}
}

func TestReleaseEventTagGuard(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/published-sdk-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(data), "- name: Require the release event's exact stable tag", 2)
	if len(parts) != 2 {
		t.Fatal("missing release event tag guard")
	}
	script, err := yamlLiteralBlock(parts[1], "run: |")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tag, ref string
		wantSuccess    bool
	}{
		{"canonical", "v4.5.6", "refs/tags/v4.5.6", true},
		{"wrong major", "v3.5.6", "refs/tags/v3.5.6", false},
		{"no leading v", "4.5.6", "refs/tags/4.5.6", false},
		{"mismatched ref", "v4.5.6", "refs/tags/v4.5.7", false},
		{"noncanonical", "v4.05.6", "refs/tags/v4.05.6", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := t.TempDir() + "/output"
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "RELEASE_TAG="+tc.tag, "GITHUB_REF="+tc.ref, "GITHUB_OUTPUT="+output)
			_, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("tag guard success = %t, want %t", err == nil, tc.wantSuccess)
			}
			if tc.wantSuccess {
				contents, err := os.ReadFile(output)
				if err != nil || string(contents) != "version="+tc.tag+"\n" {
					t.Fatalf("version output = %q, error = %v", contents, err)
				}
			}
		})
	}
}

func TestRetryRequiresPublishedReleaseMetadata(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required for the workflow's release metadata check")
	}
	data, err := os.ReadFile(".github/workflows/published-sdk-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(data), "- name: Verify exact tag, version, and existing published release", 2)
	if len(parts) != 2 {
		t.Fatal("missing retry metadata check")
	}
	script, err := yamlLiteralBlock(parts[1], "run: |")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte("#!/bin/sh\nprintf '%s' \"$FAKE_RELEASE_JSON\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gh, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tag, version, ref, metadata string
		wantSuccess                       bool
	}{
		{"published", "v4.5.6", "v4.5.6", "refs/tags/v4.5.6", `{"tag_name":"v4.5.6","draft":false,"prerelease":false,"published_at":"2026-10-28T12:00:00Z"}`, true},
		{"wrong version", "v4.5.6", "v4.5.7", "refs/tags/v4.5.6", `{"tag_name":"v4.5.6","draft":false,"prerelease":false,"published_at":"2026-10-28T12:00:00Z"}`, false},
		{"wrong ref", "v4.5.6", "v4.5.6", "refs/tags/v4.5.7", `{"tag_name":"v4.5.6","draft":false,"prerelease":false,"published_at":"2026-10-28T12:00:00Z"}`, false},
		{"metadata mismatch", "v4.5.6", "v4.5.6", "refs/tags/v4.5.6", `{"tag_name":"v4.5.7","draft":false,"prerelease":false,"published_at":"2026-10-28T12:00:00Z"}`, false},
		{"draft", "v4.5.6", "v4.5.6", "refs/tags/v4.5.6", `{"tag_name":"v4.5.6","draft":true,"prerelease":false,"published_at":null}`, false},
		{"prerelease", "v4.5.6", "v4.5.6", "refs/tags/v4.5.6", `{"tag_name":"v4.5.6","draft":false,"prerelease":true,"published_at":"2026-10-28T12:00:00Z"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "output")
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+dir+":"+os.Getenv("PATH"), "GITHUB_REPOSITORY=rudderlabs/analytics-go",
				"RELEASE_TAG="+tc.tag, "MODULE_VERSION="+tc.version,
				"GITHUB_REF="+tc.ref, "FAKE_RELEASE_JSON="+tc.metadata,
				"GITHUB_OUTPUT="+output)
			_, err := cmd.CombinedOutput()
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("retry metadata check success = %t, want %t", err == nil, tc.wantSuccess)
			}
			if tc.wantSuccess {
				contents, err := os.ReadFile(output)
				if err != nil || string(contents) != "version="+tc.version+"\n" {
					t.Fatalf("version output = %q, error = %v", contents, err)
				}
			}
		})
	}
}
