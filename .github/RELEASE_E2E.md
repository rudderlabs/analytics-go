# Published Go SDK acceptance

The published-package workflow runs the full central E2E suite for the exact stable release version. It uses the existing release App and waits for delivery validation and cleanup. The central workflow checks the release tag against the immutable event commit and `master` ancestry. It installs the exact version from the public Go module proxy and verifies module metadata and integrity evidence.

The release tag makes the package available to the registry. Acceptance starts on the published GitHub release event. This is post-tag acceptance, not a pre-publication gate. Once acceptance finishes, the release workflow sends a short Slack root and puts the exact public Go module/version link, release and run links, and check results in a non-broadcast thread reply. Failure is announced as "published; validation failed or incomplete"; it cannot roll back the tag.

The pre-tag source-candidate gate is separate work in [SDK-5973](https://linear.app/rudderstack/issue/SDK-5973). Do not treat native tests or this post-publication check as that gate.

## Validation-only retry

1. Open the failed central run from the shared action summary.
2. Resolve the validation failure.
3. Run this workflow at the existing release tag:

```sh
gh workflow run published-sdk-e2e.yml --repo rudderlabs/analytics-go --ref vX.Y.Z \
  -f release_tag=vX.Y.Z -f module_version=vX.Y.Z
```

The retry has no upload path or publishing credential. It verifies that the supplied tag and module version match each other, the selected tag ref, and an existing published GitHub release. The selected tag supplies the exact event commit. It does not use `latest`, publish a package, recreate a tag, or send another release announcement. The retry workflow must exist on the default branch and selected tag. A tag retry uses workflow code and pins stored at that tag; it cannot load a fix merged later to master. Use the central fallback below when the tag predates this workflow or a workflow fix is required. Stable `v4.X.Y` versions are supported; prerelease acceptance is not implemented.

A published Go module remains published if acceptance fails. Do not recreate or move its tag to retry E2E. A successful validation-only retry does not change an earlier failed run or its notification. Record the new acceptance run in the release ticket and existing Slack release thread.

## Retry after a workflow fix

Use the current central workflow when the caller or shared workflow needs a fix.
Copy the full release-event commit SHA from the original release run. Do not use
the latest master commit or move the release tag. Replace the example version,
commit placeholder, and request ID before running:

```sh
gh workflow run server-sdk-validation.yml --repo rudderlabs/rudder-sdk-e2e-tests --ref main \
  -f selection=auto -f source_repository=rudderlabs/analytics-go \
  -f source_sha=RELEASE_EVENT_COMMIT_SHA -f package_version=v4.X.Y \
  -f suite=full -f request_id=go-published-v4_X_Y-retry-UNIQUE_ID
```

The central workflow validates the exact published version and release commit.
The central fallback sends no release notification. Release-run reruns also send
no announcement after the first attempt. Record new evidence in the release ticket
and existing release thread. If notification itself fails, repair that thread
separately; do not rerun all release jobs to send another announcement.

## Rollout

Tracked in [SDK-5964](https://linear.app/rudderstack/issue/SDK-5964).
Central #32/#33/#35 and shared-action v1.0.0 are merged. The caller pins the
released workflow at `b6de40cc65abe8ee6c6593aac1787aea746b671f`. The existing
release App ID configuration is preserved. The pre-tag source gate remains
separate in [PR #92](https://github.com/rudderlabs/analytics-go/pull/92).
Hosted acceptance remains pending the next normal release. Do not create a
release solely to test this wiring.
