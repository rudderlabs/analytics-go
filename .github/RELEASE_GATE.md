# Go release PR E2E

Release Please creates and updates the release PR normally. The open release PR
must pass full live E2E before merging. Merging then runs the official Release
Please action to create the tag and GitHub release. There is no second full
pre-publication run, custom publisher, or release recovery generator.

## Which checks run

- Ordinary same-repository PRs run the shared credential-free reliability suite,
  in addition to the SDK's normal build and unit tests.
- Release Please branches (`release-please--...`) run the full suite: the exact
  current PR merge candidate sends events through the development RudderStack
  pipeline, and the trusted receiver verifies delivery. Success also requires
  pipeline restoration and lock release.
- Fork and Dependabot PRs skip the credential-bearing caller. They cannot be used
  as release PRs. The native build/test checks still apply.
- After publication, the existing `published-sdk-e2e.yml` workflow checks the
  exact public Go module and reports acceptance separately. A failure there
  needs investigation; it cannot undo publication.

The caller never checks out or executes PR code. The shared harness runs SDK
code in a disposable container. Only the development source write key, data
plane address and test identifiers enter that container. GitHub and workspace
administration credentials remain with the trusted harness. A branch prefix
selects the test suite; it does not grant the candidate additional permissions.

## Failed release PR check

Keep the release PR open. Merge the fix through a separate normal PR. Release
Please updates the open release PR, and the new candidate runs CI again. The
existing `always-update` configuration also updates the release branch for
changes that do not add release notes. An infrastructure-only failure can be
retried on the current candidate without changing versions, labels or tags.

A passing result belongs to the tested PR merge candidate. The central harness
rejects changed PR heads/base commits before validation and before success.
Require the release branch to be up to date before merging. The GitHub merge
commit may have a different SHA from the synthetic test merge; the merge must
retain the tested source contents. This is source validation, not a claim that
an arbitrary later packaging step produces byte-identical artifacts.

## Activation order

1. Merge the central full-PR support (SDK-6012).
2. Merge and approve the shared action full-PR result support (SDK-6013).
3. Pin this caller to that reviewed action commit and confirm the organization allows it.
4. Require `Go SDK candidate gate` on master and retain strict up-to-date checks.
5. Merge this caller change only after the dependencies and required check are ready.
6. Refresh release PR #94 and record a successful **full live** central run before merging it.

Steps 1–4 are prerequisites for replacing the old post-merge gate. Code-only
CI and a green reliability run do not establish full live acceptance or merge
protection. The PR remains draft until these prerequisites are satisfied.

Tracking: [SDK-5979](https://linear.app/rudderstack/issue/SDK-5979).
