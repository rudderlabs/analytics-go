# Published Go SDK acceptance

The published-package workflow runs the full central E2E suite for the exact stable release version. It uses the existing release App and waits for delivery validation and cleanup. The central workflow checks the release tag against the immutable event commit and `master` ancestry. It installs the exact version from the public Go module proxy and verifies module metadata and integrity evidence.

The release tag makes the package available to the registry. Acceptance starts on the published GitHub release event. This is post-tag acceptance, not a pre-publication gate. Once acceptance finishes, the release workflow sends a short Slack root and puts release links and check results in a non-broadcast thread reply. Failure is announced as "published; validation failed or incomplete"; it cannot roll back the tag.

The pre-tag source-candidate gate is separate work in [SDK-5973](https://linear.app/rudderstack/issue/SDK-5973). Do not treat native tests or this post-publication check as that gate.

## Validation-only retry

1. Open the failed central run from the shared action summary.
2. Resolve the validation failure.
3. Run this workflow at the existing release tag:

```sh
gh workflow run published-sdk-e2e.yml --repo rudderlabs/analytics-go --ref vX.Y.Z \
  -f release_tag=vX.Y.Z -f module_version=vX.Y.Z
```

The retry has no upload path or publishing credential. It verifies that the supplied tag and module version match each other, the selected tag ref, and an existing published GitHub release. The selected tag supplies the exact event commit. It does not use `latest`, publish a package, recreate a tag, or send another release announcement. Tags predating this workflow need an exact-version dispatch through the central workflow. Stable `v4.X.Y` versions are supported; prerelease acceptance is not implemented.

A published Go module remains published if acceptance fails. Do not recreate or move its tag to retry E2E. A successful validation-only retry does not change an earlier failed run or its notification. Record the new acceptance run in the release ticket and existing Slack release thread.

## Rollout

Tracked in [SDK-5964](https://linear.app/rudderstack/issue/SDK-5964). Keep this PR draft until [central PR #32](https://github.com/rudderlabs/rudder-sdk-e2e-tests/pull/32) resolves [SDK-5978](https://linear.app/rudderstack/issue/SDK-5978) and its cleanup budget, and the shared workflow from [SDK-5977](https://linear.app/rudderstack/issue/SDK-5977) has a reviewed full-SHA pin. The current shared-action pin comes from [PR #5](https://github.com/rudderlabs/github-action-sdk-e2e/pull/5); review or replace it before merge. Hosted acceptance remains pending the next normal release. Do not create a release solely to test this wiring.
