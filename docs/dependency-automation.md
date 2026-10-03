# Dependabot updates and Nix

Dependabot security updates currently maintain `web/package.json` and
`web/package-lock.json`. This repository has no `dependabot.yml`; adding one
would configure version updates, not teach Dependabot to update `npmDepsHash`.
The helper keeps Dependabot and the existing version range syntax.

## Repair workflow

After CI completes, `dependency-nix.yml` validates an open, non-draft Dependabot
PR from this repository to `main`. The head must contain the current main commit.
It compares complete Git trees, not only the PR title or a branch regex. Only
`web/package.json`, `web/package-lock.json`, and the single `npmDepsHash` value in
`flake.nix` may differ. Files must be regular blobs. Manifest scripts, overrides,
metadata, dependency names, and range operators must remain unchanged.

The initial policy accepts stable patch/minor upgrades within one major version;
for 0.x only patch upgrades within one minor are accepted. Downgrades, prereleases,
new/deleted package paths (including transitive ones), and unsupported ranges need
manual handling. The lock must use version 3, agree with the manifest, and contain
only HTTPS npm registry tarballs with SHA-512 integrity. These bounds deliberately
exclude some legitimate updates; they do not prove a package is trustworthy.

The calculation job has only `contents: read`. It checks out the trusted workflow
commit, obtains the PR lockfile as data through the API, and runs
`prefetch-npm-deps` from main's pinned nixpkgs revision. It does not evaluate the PR
flake, install npm dependencies, or execute PR scripts. No secrets are provided;
the GitHub token is removed from the prefetch subprocess environment.

A fresh runner with `contents: write` repeats the validation. It does not run Nix,
npm, or PR code. It writes a Git commit whose only change is the calculated hash,
whose parent is the validated head, then performs a non-force ref update. It
rechecks the PR state and base immediately before updating; a concurrent divergent
head is rejected by GitHub. Checkout never persists credentials. No artifact from
an untrusted workflow is consumed.

PR validation must also remain unprivileged after the helper writes a commit:
the CI and Lighthouse workflows never receive the Cachix upload token on
`pull_request`, regardless of the triggering actor. Lighthouse explicitly uses
`contents: read`. The existing preview author guard skips Dependabot PRs. These
restrictions preserve the trust boundary after a human approves or reruns CI.

## Loop prevention and recovery

This helper listens only to completion of **CI**, not to itself. It writes only
when the calculated hash differs. It neither force-pushes, rebases, dispatches
another workflow, nor retries a declined API mutation. Per-PR concurrency is
serialized. A subsequent successful CI event computes the same hash and cannot
create another identical commit. Main movement, stale heads, unsupported updates,
and absent/ambiguous workflow-to-PR associations fail closed.

After installation on main, maintainers can run the helper manually:

```sh
gh workflow run dependency-nix.yml --ref main -f pull_request=123
```

Manual invocation repairs only and never merges. Policy refusals are visible as
failed jobs with a reason. Rebase stale PRs through the normal Dependabot workflow;
do not repeatedly rerun a refused policy case.

GitHub documents that PR updates made using `GITHUB_TOKEN` can leave resulting
CI runs awaiting a maintainer's **Approve workflows to run** action. Push-triggered
workflows generally do not run from that token. If needed, approve the new PR CI;
rerunning the old commit is insufficient. No PAT or GitHub App is installed to
bypass this. The privileged helper is not expected to run in this configuration
PR: `workflow_run` workflows must first exist on the default branch.

## Automatic merge: protected, currently blocked by the main ruleset

`AUTO_MERGE` is `true`. The publisher has `contents: write` plus the read-only
`actions`, `checks`, and `statuses` permissions needed to verify CI. No additional
credentials, `pull-requests: write`, administration permission, or protection
changes are included. Set `AUTO_MERGE` to `false` to disable merging independently
of hash repair. Dependency auto-merge authorization does not authorize merging this
configuration PR.

Merging requires the same conservative dependency policy, an unchanged hash,
successful `CI` for the exact current PR head, successful `test` and `macOS desktop`
jobs (including the explicit Nix package build), and no pending or failed
checks/statuses. GitHub must report the PR as clean. The merge API receives the
validated head SHA and cannot merge a newer head.

A client-side base check alone cannot prevent main advancing between that check
and the merge API call. Therefore the helper additionally requires an **active
repository ruleset on main with strict required checks, GitHub Actions' `test`
context, and an explicitly empty bypass list**. GitHub enforces that rule at merge
time. The script reads effective rules and then verifies the matching ruleset's
parameters, enforcement, and bypass list; missing or unreadable data is a refusal.
Legacy branch protection alone and organization-only rulesets are not supported
by this initial verifier. No administrative API access or bypass is used.

At implementation time, ruleset `18796090` was active and had no bypass actors,
but `strict_required_status_checks_policy` was **false**. Automatic merging is
therefore blocked until a maintainer explicitly approves and enables the strict
setting in that ruleset. Hash repair can run without that change. This PR does
**not** modify the ruleset. A current-main requirement can require more Dependabot
rebases and CI runs; that is an intentional tradeoff for safe autonomous merging.

If another check is still pending when CI completes, merging is refused. There is
no timer or retry loop. After all checks finish, a maintainer may rerun the exact
current-head CI to produce a new completion event. Manual helper dispatch remains
repair-only. Merge-queue or branch-rule restrictions are visible refusals, not a
reason to bypass protection.

## Verification

```sh
python3 -B -m unittest discover -s tests/dependency_bot -v
nix run github:nixos/nixpkgs/29916453413845e54a65b8a1cf996842300cd299#actionlint -- .github/workflows/dependency-nix.yml
```

Tests cover identity, semantic update limits, lock URLs, workflow/other-file
changes, symlinks, stale heads, exact-head CI, idempotence, and conditional writes.
The normal CI also runs these tests and explicitly builds `.#track-cli`, which
checks the pinned npm dependency hash and frontend package build.

## Official references

- [Dependabot configuration options](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference)
- [Nixpkgs prefetch-npm-deps](https://github.com/NixOS/nixpkgs/blob/master/doc/languages-frameworks/javascript.section.md#prefetch-npm-deps)
- [GitHub token workflow triggering](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)
- [Merge endpoint and permissions](https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request)
- [Renovate postUpgradeTasks](https://docs.renovatebot.com/configuration-options/#postupgradetasks): a supported alternative, but requires operator command permissions and an explicit migration decision.
