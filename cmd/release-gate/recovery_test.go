package main

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareReleasePRRecovery(t *testing.T) {
	for _, mode := range []string{"success", "ordinary push", "stale event", "current release", "tag exists", "tag appears", "tag API failure", "foreign ancestor", "master advances", "invalid metadata", "prerelease baseline", "label write fails"} {
		t.Run(mode, func(t *testing.T) {
			p := makePR()
			p.Merged, p.MergeCommitSHA, p.Labels = true, shaA, []label{{"autorelease: pending"}}
			master, tagReads, masterReads, deleted := shaB, 0, 0, false
			if mode == "stale event" || mode == "current release" {
				master = shaA
			}
			a, requests := testAPI(t, func(r *http.Request) (int, any) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/branches/master"):
					masterReads++
					if mode == "master advances" && masterReads > 1 {
						master = shaA
					}
					return 200, map[string]any{"commit": map[string]string{"sha": master}}
				case strings.HasSuffix(r.URL.Path, "/releases/latest"):
					return 200, map[string]any{"tag_name": "v4.4.0", "published_at": "2026-10-28T00:00:00Z", "prerelease": mode == "prerelease baseline"}
				case strings.HasSuffix(r.URL.Path, "/pulls"):
					if mode == "ordinary push" || deleted {
						return 200, []pull{}
					}
					return 200, []pull{p}
				case strings.HasSuffix(r.URL.Path, "/pulls/42"):
					return 200, p
				case strings.Contains(r.URL.Path, "/contents/"):
					if r.URL.Query().Get("ref") != shaA && r.URL.Query().Get("ref") != shaB {
						t.Fatal("recovery must inspect the original immutable release files")
					}
					f := files()
					if mode == "invalid metadata" {
						f["analytics.go"] = "package other"
					}
					name := strings.TrimPrefix(r.URL.Path, "/repos/"+repository+"/contents/")
					return 200, map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(f[name])), "encoding": "base64", "sha": shaB}
				case strings.Contains(r.URL.Path, "/compare/"):
					status := "ahead"
					if mode == "foreign ancestor" {
						status = "diverged"
					}
					return 200, map[string]any{"status": status, "merge_base_commit": map[string]string{"sha": shaA}}
				case strings.Contains(r.URL.Path, "/git/ref/tags/"):
					tagReads++
					if mode == "tag API failure" {
						return 503, nil
					}
					if mode == "tag exists" || (mode == "tag appears" && tagReads > 1) {
						return 200, map[string]string{"ref": "refs/tags/v4.5.0"}
					}
					return 404, nil
				case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/labels/autorelease: pending"):
					if mode == "label write fails" {
						return 403, nil
					}
					deleted = true
					p.Labels = nil
					return 200, []label{}
				default:
					t.Fatalf("unexpected recovery mutation/request: %s %s", r.Method, r.URL)
					return 500, nil
				}
			})
			outputPath := filepath.Join(t.TempDir(), "output")
			if err := os.WriteFile(outputPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GITHUB_OUTPUT", outputPath)
			sha := shaB
			if mode == "current release" {
				sha = shaA
			}
			err := a.prepareReleasePR(sha)
			valid := mode == "success" || mode == "ordinary push"
			if (err == nil) != valid {
				t.Fatalf("recovery err=%v", err)
			}
			if deleted != (mode == "success") {
				t.Fatalf("unsafe label transition: deleted=%t err=%v", deleted, err)
			}
			if valid {
				data, err := os.ReadFile(outputPath)
				if err != nil || !strings.Contains(string(data), "baseline-version=4.4.0\n") {
					t.Fatalf("must use published baseline, not unpublished 4.5.0: %s %v", data, err)
				}
			}
			for _, request := range *requests {
				if strings.HasPrefix(request, "POST ") || strings.HasPrefix(request, "PATCH ") {
					t.Fatal("recovery must not publish, move refs, or alter source", request)
				}
			}
		})
	}
}
