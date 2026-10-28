package analytics

import (
	"os"
	"strings"
	"testing"
)

// These assertions guard the source-candidate behavior until the shared action
// supports validation of an exact published Go module version.
func TestServerSDKE2ESourceSuiteSelection(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/server-sdk-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, required := range []string{
		"types: [published]",
		"github.event.pull_request.head.repo.full_name == github.repository",
		"github.event.pull_request.user.login != 'dependabot[bot]'",
		"github.event_name == 'pull_request' && 'reliability'",
		"github.event_name == 'release' && 'full'",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("source validation workflow missing %q", required)
		}
	}
	selection := "github.event_name == 'release' && 'full' || inputs.suite || vars.SDK_E2E_SUITE"
	if !strings.Contains(text, selection) {
		t.Error("release source suite must not inherit the manual or repository-variable fallback")
	}
}
