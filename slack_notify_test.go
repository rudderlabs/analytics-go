package analytics

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestReleaseNotificationFollowsAcceptance(t *testing.T) {
	if _, err := os.Stat(".github/workflows/slack-notify.yml"); !os.IsNotExist(err) {
		t.Fatalf("independent early release notification still exists: %v", err)
	}
	workflow, err := os.ReadFile(".github/workflows/published-sdk-e2e.yml")
	if err != nil {
		t.Fatalf("read published acceptance workflow: %v", err)
	}
	text := string(workflow)
	start := strings.Index(text, "\n  notify:")
	if start < 0 {
		t.Fatal("missing notification job")
	}
	notify := text[start:]
	for _, required := range []string{
		"if: always() && github.event_name == 'release'",
		"needs: validate",
		"github.run_attempt == '1'",
		"VALIDATION_RESULT: ${{ needs.validate.result }}",
		"\"thread_ts\": ",
		"\"reply_broadcast\": false",
		"\"unfurl_links\": false",
		"\"unfurl_media\": false",
		"slackapi/slack-github-action@dcb1066f776dd043e64d0e8ba94ca15cc7e1875d",
		"secrets.SLACK_RELEASE_CHANNEL_ID",
		"secrets.SLACK_BOT_TOKEN",
	} {
		if !strings.Contains(notify, required) {
			t.Errorf("notification job is missing %q", required)
		}
	}
	if strings.Contains(notify, "continue-on-error:") {
		t.Fatal("Slack delivery errors must remain visible")
	}
	if strings.Contains(notify, "RESOLVE") {
		t.Fatal("notification still has an unresolved mention placeholder")
	}
	if strings.Index(notify, "id: root") > strings.Index(notify, "\"thread_ts\":") {
		t.Fatal("thread reply precedes its root message")
	}
}

func TestReleaseNotificationContent(t *testing.T) {
	requireWorkflowTools(t, "bash", "python3")
	workflow, err := os.ReadFile(".github/workflows/published-sdk-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	notify := strings.SplitN(string(workflow), "- name: Prepare short root and detailed thread reply", 2)
	if len(notify) != 2 {
		t.Fatal("missing message preparation step")
	}
	script, err := yamlLiteralBlock(notify[1], "run: |")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, result, status, checks string
	}{
		{"success", "success", "published; validation passed", "passed"},
		{"failure", "failure", "published; validation failed or incomplete", "failed or incomplete"},
		{"skipped", "skipped", "published; validation failed or incomplete", "failed or incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := t.TempDir() + "/output"
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(),
				"RELEASE_TAG=v4.5.6", "RELEASE_DATE=2026-10-28T12:00:00Z",
				"VALIDATION_RESULT="+tc.result,
				"RELEASE_URL=https://github.com/rudderlabs/analytics-go/releases/tag/v4.5.6",
				"WORKFLOW_URL=https://github.com/rudderlabs/analytics-go/actions/runs/123",
				"CENTRAL_RUN_URL=https://github.com/rudderlabs/rudder-sdk-e2e-tests/actions/runs/456",
				"GITHUB_OUTPUT="+output)
			cmd.Env = append(cmd.Env, "GITHUB_STEP_SUMMARY="+t.TempDir()+"/summary")
			if result, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("render notification: %v: %s", err, result)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			fields := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if !ok {
					t.Fatalf("invalid output %q", line)
				}
				var decoded string
				if err := json.Unmarshal([]byte(value), &decoded); err != nil {
					t.Fatalf("invalid JSON output: %v", err)
				}
				fields[key] = decoded
			}
			root, detail := fields["root_json"], fields["detail_json"]
			if !strings.Contains(root, "Go SDK v4.5.6 | 2026-10-28 | "+tc.status) {
				t.Fatalf("wrong root: %q", root)
			}
			if strings.Contains(root, "https://") || strings.Contains(root, "Go proxy") {
				t.Fatalf("root contains thread detail: %q", root)
			}
			if !strings.Contains(detail, "Go proxy, delivery, and cleanup: "+tc.checks) ||
				!strings.Contains(detail, "Central E2E run") ||
				!strings.Contains(detail, "https://proxy.golang.org/github.com/rudderlabs/analytics-go/v4/@v/v4.5.6.info") {
				t.Fatalf("missing thread check results: %q", detail)
			}
		})
	}
}

func requireWorkflowTools(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is required for this workflow script test", name)
		}
	}
}

func yamlLiteralBlock(document, marker string) (string, error) {
	lines := strings.Split(document, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != marker {
			continue
		}

		markerIndent := len(line) - len(strings.TrimLeft(line, " "))
		contentIndent := markerIndent + 2
		var block []string
		for _, contentLine := range lines[i+1:] {
			if strings.TrimSpace(contentLine) == "" {
				block = append(block, "")
				continue
			}

			indent := len(contentLine) - len(strings.TrimLeft(contentLine, " "))
			if indent <= markerIndent {
				break
			}
			if indent < contentIndent {
				return "", fmt.Errorf("invalid indentation after %q", marker)
			}
			block = append(block, contentLine[contentIndent:])
		}
		return strings.Join(block, "\n"), nil
	}

	return "", fmt.Errorf("could not find %q", marker)
}
