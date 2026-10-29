# Go SDK candidate and release validation

Tracked in [SDK-5979](https://linear.app/rudderstack/issue/SDK-5979).
Published-module acceptance and notification remain separate from the source gate.

## One publication path

The release PR entry point calls only Release Please's `createPullRequests`.
It cannot create tags or releases. Release Please is locked at 17.6.0, the same
library version bundled in the previous action. There is no fallback publisher.

A master push selects a merged PR labeled `autorelease: pending`. Selection
checks the full PR, exact merge commit, module path, manifest, SDK version,
changelog, and absent release tag. An ordinary commit generates the next release
PR. Invalid release metadata fails instead of silently skipping the gate.
Generation uses the latest published stable GitHub release as its version
baseline. The merged manifest may describe an unpublished candidate and cannot
establish that baseline. Release Please still calculates versions and release
notes using the repository's existing configuration.

For a selected release, the full shared action validates the exact source commit.
The caller pins v1.0.0 at `b6de40cc65abe8ee6c6593aac1787aea746b671f`. The publisher
requires the authenticated common result: `selection=candidate`, SDK, package,
repository, commit, version, `artifact.source_tree`, delivery, restoration, and
lock release. No legacy Go report fields are accepted.

Immediately before tag creation, the publisher checks current master again.
The tag always targets the exact validated SHA. Tag creation publishes the Go
module. GitHub Release creation follows with the reviewed changelog notes.
Only after successful release creation does the publisher add `autorelease: tagged`
and remove `autorelease: pending`. An existing tag with incomplete release/label
state blocks generation until that publication is recovered.

The publication and release-PR tokens need Contents write and Pull requests write.
Pull requests write authorizes PR label updates; no Issues permission is needed.
Both tokens use the existing release App and stay scoped to this repository.
The central dispatch token has Actions write only for `rudder-sdk-e2e-tests`.

If release creation or label completion fails after tagging, recover the existing
release and labels manually. Do not create or move the tag again. Confirm the
existing tag targets the validated SHA. Keep the pending label until the release
exists. Add the tagged label before removing the pending label. Complete recovery
before generating the next release PR.

## Recovery when master advances before tagging

If the full gate fails, publication stops. Merge a separate fix PR into master.
That push automatically prepares a replacement release PR:

1. Verify the event is current master and read the last published stable version.
2. Validate every pending merged release's files and ancestry to current master.
3. Require each superseded release tag to be absent.
4. Require all changelog entries newer than the published baseline to be untagged.
5. Recheck master and tag absence before removing the superseded pending labels.
6. Generate a new Release Please PR from current master and the published baseline.
7. Replace unpublished changelog entries with regenerated notes, retaining published history.

The App-created or updated release PR runs normal PR CI automatically. The
existing `always-update` configuration refreshes its branch even when a CI-only
fix does not change release notes. Approval and merge remain required. After
merge, the replacement commit must pass fresh full E2E and evidence checks before
the publisher can create its tag. Nothing publishes solely because recovery ran.

If generation fails after clearing superseded labels, rerun the current generation
workflow or merge the next fix. The published baseline remains authoritative, so
the next run can rebuild the PR without restoring obsolete pending labels. A tag
or API error stops recovery; an existing tag always requires the post-tag path.
No tag is moved, deleted or recreated. A retry of an old validation still tests
its original commit and cannot publish newer source code.

## PR candidate check

The stable check is `Go SDK candidate gate`, from GitHub Actions. The caller checks
the event merge SHA, both merge parents, and current master before and after the
isolated reliability run. Stale events fail. A PR event rerun cannot validate a
newer merge commit. Update the PR branch to produce a fresh event after base changes.

Fork and Dependabot PRs do not receive the central App token. Their aggregate
passes only for an explicit exclusion with a fresh candidate and skipped validator.
Missing, failed, cancelled, or unexpectedly skipped validation fails eligible PRs.
Same-repository writers use the existing trusted workflow model.

## Rollout evidence

The effective master rules read on 29 October 2026 do not require the candidate
check. Required-check enforcement remains rollout work; the workflow alone does
not enforce review or branch freshness.

1. Merge and deploy the published acceptance caller in PR #91.
2. Verify the release App permissions for both scoped tokens.
3. Verify the new action pins against the organization allowlist.
4. Verify ordinary and Release Please PR checks at their current merge SHAs.
5. Configure the candidate check with strict branch freshness after verification.
6. Verify branch and tag write controls before enabling normal releases.
7. Record full source and published acceptance during the next normal release.

The final master read and tag write are separate API calls. They cannot exclude
a concurrent branch advance atomically. The tag still targets the tested SHA.
Strict exclusion of branch advances during that interval needs verified branch
write controls. No repository rules, variables, secrets, or live releases are
changed by the review task. Manual E2E retries cannot publish or announce.
