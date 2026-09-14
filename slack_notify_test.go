package analytics

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSlackNotificationPayload(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/slack-notify.yml")
	if err != nil {
		t.Fatalf("read Slack notification workflow: %v", err)
	}

	payload, err := yamlLiteralBlock(string(workflow), "payload: |")
	if err != nil {
		t.Fatal(err)
	}

	var message struct {
		Blocks []map[string]json.RawMessage `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(payload), &message); err != nil {
		t.Fatalf("parse Slack notification payload: %v", err)
	}
	if len(message.Blocks) == 0 {
		t.Fatal("Slack notification payload has no blocks")
	}

	sectionHasAccessory := false
	for i, block := range message.Blocks {
		rawType, ok := block["type"]
		if !ok {
			t.Fatalf("Slack notification block %d has no type", i)
		}

		var blockType string
		if err := json.Unmarshal(rawType, &blockType); err != nil || blockType == "" {
			t.Fatalf("Slack notification block %d has an invalid type", i)
		}

		if blockType == "section" {
			_, sectionHasAccessory = block["accessory"]
		}
	}
	if !sectionHasAccessory {
		t.Fatal("Slack notification section has no accessory")
	}

	workflowText := string(workflow)
	if !strings.Contains(workflowText, "continue-on-error: true") {
		t.Fatal("Slack notification step must not fail the release workflow")
	}
	if !strings.Contains(workflowText, "errors: true") {
		t.Fatal("Slack notification step must report Slack API errors")
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
