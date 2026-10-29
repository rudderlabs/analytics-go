package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func workflow(t *testing.T, file string) map[any]any {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var result map[any]any
	if err := yaml.UnmarshalStrict(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func field(t *testing.T, obj map[any]any, key string) map[any]any {
	t.Helper()
	value, ok := obj[key].(map[any]any)
	if !ok {
		t.Fatalf("missing %s in %+v", key, obj)
	}
	return value
}

func TestPublicationDependencyGraph(t *testing.T) {
	w := workflow(t, "../../.github/workflows/release-please.yaml")
	jobs := field(t, w, "jobs")
	pr := field(t, jobs, "release-please")
	if pr["needs"] != "select-release" || !strings.Contains(asString(pr["if"]), "outputs.release == 'false'") {
		t.Fatal("PR generation must wait for release selection")
	}
	steps := pr["steps"].([]any)
	if field(t, steps[len(steps)-1].(map[any]any), "with")["skip-github-release"] != true {
		t.Fatal("Release Please must never create tags")
	}
	data, err := os.ReadFile("../../.github/workflows/release-please.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "GO_RELEASE_GATE_ENABLED") {
		t.Fatal("publication must have one gated path")
	}
	full := field(t, jobs, "validate-release")
	if full["needs"] != "select-release" || !strings.Contains(asString(full["if"]), "needs.select-release.outputs.release == 'true'") {
		t.Fatal("full suite must only run for a selected release")
	}
	publisher := field(t, jobs, "publish-release")
	deps := publisher["needs"].([]any)
	if len(deps) != 2 || deps[0] != "select-release" || deps[1] != "validate-release" {
		t.Fatal("publisher does not depend on full check")
	}
	if !strings.Contains(publisher["if"].(string), "needs.validate-release.result == 'success'") ||
		!strings.Contains(publisher["if"].(string), "needs.select-release.outputs.release == 'true'") {
		t.Fatal("non-release or skipped/failed validation can publish")
	}
	var fullAction, publisherWrites, validatorWrites bool
	for _, raw := range full["steps"].([]any) {
		step := raw.(map[any]any)
		if strings.Contains(asString(step["uses"]), "github-action-sdk-e2e@b6de40cc65abe8ee6c6593aac1787aea746b671f") && field(t, step, "with")["suite"] == "full" {
			fullAction = true
		}
		if strings.Contains(asString(step["run"]), "publish") {
			validatorWrites = true
		}
	}
	for _, raw := range publisher["steps"].([]any) {
		step := raw.(map[any]any)
		if strings.Contains(asString(step["run"]), "release-gate publish") {
			publisherWrites = true
		}
		if asString(step["id"]) == "token" {
			permissions := field(t, step, "with")
			for _, name := range []string{"permission-contents", "permission-pull-requests", "permission-issues"} {
				if permissions[name] == nil {
					t.Fatalf("publication token missing %s", name)
				}
			}
		}
	}
	if !fullAction || validatorWrites || !publisherWrites {
		t.Fatal("full E2E/publisher boundary changed")
	}
}

func asString(v any) string { s, _ := v.(string); return s }

func TestGateCommandsHavePinnedGo(t *testing.T) {
	for _, file := range []string{"../../.github/workflows/release-please.yaml", "../../.github/workflows/server-sdk-e2e.yml"} {
		jobs := field(t, workflow(t, file), "jobs")
		for name, raw := range jobs {
			job := raw.(map[any]any)
			setup := false
			for _, item := range job["steps"].([]any) {
				step := item.(map[any]any)
				if strings.HasPrefix(asString(step["uses"]), "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e") &&
					field(t, step, "with")["go-version-file"] == "go.mod" && field(t, step, "with")["cache"] == false {
					setup = true
				}
				if strings.Contains(asString(step["run"]), "go run ./cmd/release-gate") && !setup {
					t.Fatalf("%s %s runs gate without pinned Go", file, name)
				}
			}
		}
	}
}

func TestPRGateUsesEventMergeSHA(t *testing.T) {
	jobs := field(t, workflow(t, "../../.github/workflows/server-sdk-e2e.yml"), "jobs")
	for _, name := range []string{"select-pr", "validate", "go-sdk-candidate-gate"} {
		job := field(t, jobs, name)
		checkout := false
		for _, item := range job["steps"].([]any) {
			step := item.(map[any]any)
			if strings.HasPrefix(asString(step["uses"]), "actions/checkout@") {
				if field(t, step, "with")["ref"] != "${{ github.sha }}" {
					t.Fatalf("%s must check out the event merge SHA containing the gate", name)
				}
				checkout = true
			}
		}
		if !checkout {
			t.Fatalf("%s does not check out gate code", name)
		}
	}
}

func TestManualValidationCannotPublish(t *testing.T) {
	w := workflow(t, "../../.github/workflows/server-sdk-e2e.yml")
	jobs := field(t, w, "jobs")
	gate := field(t, jobs, "go-sdk-candidate-gate")
	if gate["name"] != "Go SDK candidate gate" || !strings.Contains(gate["if"].(string), "always()") {
		t.Fatal("aggregate missing")
	}
	if !strings.Contains(field(t, jobs, "validate-other")["if"].(string), "workflow_dispatch") {
		t.Fatal("manual validation missing")
	}
	for _, raw := range jobs {
		job := raw.(map[any]any)
		for _, item := range job["steps"].([]any) {
			step := item.(map[any]any)
			if strings.Contains(asString(step["run"]), "release-gate publish") {
				t.Fatal("E2E workflow publishes")
			}
		}
	}
}
