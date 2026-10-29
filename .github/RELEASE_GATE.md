# Go SDK candidate and release validation

Tracked in [SDK-5979](https://linear.app/rudderstack/issue/SDK-5979).
Merge [PR #91](https://github.com/rudderlabs/analytics-go/pull/91) first. Its
published-module acceptance and notification remain separate from the source gate.

## One publication path

Release Please generates release PRs with `skip-github-release: true`. It cannot
create tags or releases. There is no opt-in variable or fallback publisher.
Merging this workflow changes publication to the gated path.

A master push selects a merged PR labeled `autorelease: pending`. Selection
checks the full PR, exact merge commit, module path, manifest, SDK version,
changelog, and absent release tag. An ordinary commit generates the next release
PR. Invalid release metadata fails instead of silently skipping the gate.

For a selected release, the full shared action validates the exact source commit.
The caller pins v1.0.0 at `b6de40cc65abe8ee6c6593aac1787aea746b671f`. The publisher
requires the authenticated common result: `selection=candidate`, SDK, package,
repository, commit, version, `artifact.source_tree`, delivery, restoration, and
lock release. No legacy Go report fields are accepted.

Immediately before tag creation, the publisher checks current master again.
The tag always targets the exact validated SHA. Tag creation publishes the Go
module. GitHub Release creation follows with the reviewed changelog notes.
Only after successful release creation does the publisher add `autorelease: tagged`
and remove `autorelease: pending`. Pending merged releases block new release PRs.

The publication token needs Contents write, Pull requests read, and Issues write.
The release-PR token also needs Pull requests write. Both tokens use the existing
release App and stay scoped to this repository. The central dispatch token has
Actions write only for `rudder-sdk-e2e-tests`.

If release creation or label completion fails after tagging, recover the existing
release and labels manually. Do not create or move the tag again. Confirm the
existing tag targets the validated SHA. Keep the pending label until the release
exists. Add the tagged label before removing the pending label. Complete recovery
before generating the next release PR.

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
