# Published Go SDK acceptance

The published-package workflow runs the full central E2E suite for the exact stable release version. It uses the existing release App and waits for delivery validation and cleanup. The central workflow checks the release tag against the immutable event commit and `master` ancestry. It installs the exact public registry package and verifies native package metadata and integrity evidence.

The release tag makes the package available to the registry. Acceptance starts on the published GitHub release event. This is post-tag acceptance, not a pre-publication gate.

Full source-candidate E2E before publication is a separate rollout. Do not treat native tests or this post-publication check as that gate.

## Validation-only retry

1. Open the failed central run from the shared action summary.
2. Resolve the validation failure.
3. Run this workflow at the existing release tag:

```sh
gh workflow run published-sdk-e2e.yml --repo rudderlabs/analytics-go --ref vX.Y.Z
```

The retry has no upload path or publishing credential. It derives the exact version and commit from the selected tag. It does not use `latest`, publish a package, or recreate a tag. Tags predating this workflow need an exact-version dispatch through the central workflow. Stable `X.Y.Z` versions are supported; prerelease acceptance is not implemented.

A published package remains published if acceptance fails. Do not rerun a successful upload job to retry E2E. A successful validation-only retry does not change an earlier failed run or its notification. Record the new acceptance run in the release ticket and existing Slack release thread.

## Rollout

Tracked in [SDK-5964](https://linear.app/rudderstack/issue/SDK-5964). Merge only after [central PR #32](https://github.com/rudderlabs/rudder-sdk-e2e-tests/pull/32) and [shared-action PR #5](https://github.com/rudderlabs/github-action-sdk-e2e/pull/5) are reviewed and merged. The action pin is an existing commit from PR #5 and must be verified or refreshed before merge. Hosted acceptance remains pending the next normal release. Do not create a release solely to test this wiring.
