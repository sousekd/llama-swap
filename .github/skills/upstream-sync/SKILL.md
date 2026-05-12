---
name: upstream-sync
description: '**WORKFLOW SKILL** — Sync this fork (sousekd/llama-swap) with upstream (mostlygeek/llama-swap) while preserving the fork-only feature commits. USE WHEN the user says "update from upstream", "sync the fork", "pull upstream", "rebase on upstream", "merge upstream vNNN", or asks to release a new fork build that tracks an upstream tag. Covers both small upstream patches and major version bumps. Handles branch layout (main / release/staging / release/stable), archiving, conflict-as-feature replay, validation, README maintenance, force-with-lease promotion, and branch cleanup. DO NOT USE FOR: routine bug fixes that do not involve upstream, dependency bumps, or local feature work.'
---

# Upstream Sync

A repeatable, low-risk procedure for advancing this fork onto a newer `upstream/main` while preserving the small set of fork-only commits.

> **Mental model.** The fork is a thin, public delta on top of `upstream/main`. A sync is "rebuild the delta on top of a newer base, prove it still works, then move the public branch pointers atomically".

## What you must know before starting

This skill assumes **zero prior context**. Read this section in full before touching the repo.

### Repository identity

- **Fork remote (`origin`)**: `sousekd/llama-swap`
- **Upstream remote (`upstream`)**: `mostlygeek/llama-swap`
- The `upstream` remote is intentionally restricted to its `main` branch only. The fetch refspec must be `+refs/heads/main:refs/remotes/upstream/main`. If it isn't, fix it with `git remote set-branches upstream main` before doing anything else. Never push to `upstream`. Never create or modify any `upstream/*` ref.

### Long-lived branches

| Branch            | Role                                                                                          | Force-push allowed?              |
| ----------------- | --------------------------------------------------------------------------------------------- | -------------------------------- |
| `main`            | Strict mirror of `upstream/main`. Never carries fork commits. Always fast-forwarded.          | No. Fast-forward only.           |
| `release/staging` | Integration branch: `upstream/main` + the fork-only commits. Used for testing.                | Yes, with `--force-with-lease`.  |
| `release/stable`  | Promoted, deployable state. Moves forward from `release/staging` only after explicit sign-off.| Yes, with `--force-with-lease`.  |

Anything else (e.g. `sync/upstream-vNNN`, `pr/*`, `local/*`) is temporary scaffolding and should be deleted after the sync.

### Fork-only commits (the "delta")

The fork delta is **never hard-coded**. It drifts as features are added, removed, or upstreamed. Discover it fresh on every sync — the answers come from three sources, in this order:

1. **`README.md` of the fork** is the authoritative human-readable description of what this fork adds on top of upstream. Read it first; it tells you *what the features are* and *why they exist*.
2. **`git log --oneline main..release/staging`** is the authoritative machine-readable replay set. Each commit is one fork feature (or a follow-up to one). Subjects, bodies, and per-commit diffs are your spec.
3. **`git log -p main..release/staging -- <path>`** narrows the spec to a single subsystem when you need to understand *how* a feature was implemented before re-applying it.

From these three sources, build a mental model per commit: *what user-visible behavior does this commit add, which subsystems does it touch, and what would "the same feature on the new upstream" look like?* Do this **before** cherry-picking, not during a conflict.

Do not assume the file footprint of a feature is stable across upstream versions — upstream regularly renames, relocates, or restructures files. The README + commit-body pair tells you *intent*; the new upstream code tells you *where intent now belongs*.

### Conventions baked into this workflow

- **Tag before rewriting.** Every sync first creates an annotated tag `fork-pre-vNNN` on the current `release/staging` and pushes it. That tag is the only ref that needs an explicit rollback point, because `release/staging` is the only branch that gets force-rewritten (`main` fast-forwards, `release/stable` is left alone). Pre-sync tags are permanent.
- **Cherry-pick one commit at a time and validate after each.** This isolates conflicts and keeps test signal clean.
- **Conflicts are features, not patches.** When upstream has moved, renamed, or restructured the surrounding code, re-implement the fork commit's *intent* (as captured by its message and the README) on the new code, then commit with the original message. Do not "improve" upstream code while reintegrating.
- **Files may have moved.** Cherry-picks that touch a relocated file produce "deleted by us / modified by them" conflicts. Find the new path with `git log --follow -- <old-path>` or `git log --diff-filter=R --name-status main` and reapply the change at the new location.
- **Force-pushing**: only ever `--force-with-lease`, never `--force`.
- **`release/stable` is never auto-promoted.** Promotion is a separate, explicit decision by the maintainer.
- **No unrelated refactors during a sync.** Minimal, tightly-scoped changes only.

### Known noise that is not your problem

Before the sync, capture a baseline of pre-existing warnings on `release/staging`:

```bash
gofmt -l . > /tmp/pre-sync-gofmt.txt
staticcheck ./... > /tmp/pre-sync-staticcheck.txt 2>&1 || true
```

After the sync, diff against the new state. Anything that was already there is upstream's problem and must not be "fixed" as part of a sync. Anything new is yours to address.

`proxy/ui_dist/` is gitignored on working branches; the release pipeline regenerates it. Never commit it from a sync.

## Decision: minor sync vs major sync

Gather facts first:

```bash
git fetch upstream --prune --tags
git log --oneline upstream/main ^main                          # what upstream added
git diff --stat main..upstream/main                            # where upstream changed things
git tag --merged upstream/main --sort=-v:refname | head -5     # find candidate vNNN

# Compute the set of files the fork delta touches, then intersect with upstream's diff:
FORK_FILES=$(git diff --name-only main release/staging)
echo "$FORK_FILES" | xargs -I{} git diff --name-status main..upstream/main -- {}
```

The last command tells you exactly which fork-touched files upstream also changed (or renamed). Empty output → expect a clean cherry-pick run. Non-empty output → expect real conflict work on the listed files.

- **Minor sync** — the intersection above is empty or trivial. Cherry-picking should be conflict-free.
- **Major sync** — the intersection lists meaningful files, *or* upstream tagged a new version, *or* upstream renamed/moved any file the fork touched. Expect conflicts and apply the "features, not patches" rule.

The phases below are identical for both cases; only the conflict-handling step differs in effort.

## The sync, phase by phase

> Throughout the rest of this document, `vNNN` is a placeholder for the upstream version label. Pick it from the freshest piece of upstream identity available, in this preference order:
>
> 1. The latest upstream **tag** merged into `upstream/main` (e.g. `v211`, `v212`).
> 2. If upstream has commits past the latest tag, append the count: `<latest-tag>-plus-<N>` (e.g. `v211-plus-4`). Compute `N` with `git rev-list --count <latest-tag>..upstream/main`.
> 3. If there is no upstream tag at all, use the upstream short SHA: `sha-<7-char>`.
>
> The label only needs to be unique within this fork's tags and informative at a glance — do not invent free-form names like "big-update" or "merge-2".

### Phase 0 — Preflight

1. Working tree is clean: `git status --short --branch`. Stash or commit anything pending.
2. Confirm remotes and pinned upstream tracking:
   ```bash
   git remote -v
   git config --get-all remote.upstream.fetch
   ```
   The upstream fetch refspec must be `+refs/heads/main:refs/remotes/upstream/main`. Fix with `git remote set-branches upstream main` if not.
3. Read `AGENTS.md` for repo-wide conventions (commit message style, test commands, code-review rubric).
4. Fetch: `git fetch upstream --prune` and `git fetch origin --prune`.

### Phase 1 — Tag the current `release/staging`

Create a single annotated rollback tag and push it. `release/staging` is the only branch this sync will rewrite; `main` will only fast-forward and `release/stable` is left alone, so they don't need separate snapshots.

```bash
TAG=vNNN
git tag -a "fork-pre-$TAG" release/staging \
  -m "release/staging state immediately before syncing onto upstream $TAG"
git push origin "fork-pre-$TAG"
```

If something goes wrong later, `git reset --hard fork-pre-$TAG` restores the pre-sync state of `release/staging` exactly. Do not skip this phase, even for "trivial" syncs.

### Phase 2 — Advance `main`

`main` is a strict mirror of `upstream/main`. Always fast-forward; never merge.

```bash
git checkout main
git merge --ff-only upstream/main
git push origin main
```

If a fast-forward is impossible, somebody committed onto `main` that should not have. **Stop and investigate** before proceeding.

### Phase 3 — Build a sync integration branch

Create a throwaway branch from the new `main` and replay the fork commits.

```bash
git checkout -b "sync/upstream-$TAG"
```

Identify the replay set — do not hard-code SHAs:

```bash
git log --oneline release/staging..main           # should be empty after Phase 2
git log --oneline main..release/staging           # the fork commits to replay (oldest last)
# also visible from the tag: fork-pre-$TAG
```

Cherry-pick **one commit at a time, in original (oldest-first) order**, validating after each:

```bash
git cherry-pick <SHA-1>
# run focused tests for that commit (see Phase 4)
git cherry-pick <SHA-2>
# run focused tests
# ...continue
```

After the last cherry-pick, sanity-check that nothing was dropped:

```bash
diff <(git log --format=%s "fork-pre-$TAG" ^main) \
     <(git log --format=%s HEAD ^main)
```

The two commit-subject lists must match (order included). If they don't, you either skipped a commit, reordered them, or accidentally added an extra one — fix before continuing.

#### Conflict handling

- **Trivial conflicts** (whitespace, nearby edits): resolve, `git add`, `git cherry-pick --continue`. The reapplied logic must be semantically identical to the original commit. Compare against the pre-sync tag when in doubt:
  ```bash
  git diff "fork-pre-$TAG" -- <file>
  ```
- **Non-trivial conflicts** (upstream rewrote the surrounding code, replaced a subsystem, etc.): treat the fork commit as a *feature spec*. Re-implement the feature on top of the new upstream code, then commit with the original commit message. Do not bundle unrelated changes.
- **File renamed/moved by upstream** ("deleted by us / modified by them" or vice versa): find the new location (`git log --follow -- <old-path>` or `git log --diff-filter=R --name-status main`), apply the fork change there, `git add` both the deletion and the new file, then `git cherry-pick --continue`.
- **Feature obsoleted upstream**: if upstream now solves the same problem natively, skip the fork commit (`git cherry-pick --skip`) and explicitly note it in the README and PR description. Do not silently drop it.
- **Dependency changes**: if upstream changed `go.mod`/`go.sum`, run `go mod tidy` once after all cherry-picks complete and commit the result as `chore: go mod tidy after upstream $TAG sync` only if it produces a real diff. If `ui-svelte/package-lock.json` changed upstream, run `npm ci` (not `npm install`) before any UI tests.

### Phase 4 — Validate

Validate progressively: focused tests after each cherry-pick, full suite once all commits are applied.

#### Focused tests per fork commit

Derive the test set from the commit itself — do not rely on a hard-coded list. For each cherry-picked commit:

1. Inspect what it changed:
   ```bash
   git show --stat HEAD
   ```
2. Pick the smallest test invocation that exercises the changed code:
   - Go: any new or modified `Test*` functions in the diff are the obvious targets. Run them with `go test -v -run '^TestName1|TestName2$' ./<package>/...`. If the commit only changed non-test Go code, run the test files in the same package: `go test -v ./<package>/...`.
   - UI (`ui-svelte/`): run `npm run check` for type/syntax regressions, plus `npm test -- <pattern>` if the commit added or modified test files.
3. Fix any failure **before** the next cherry-pick. Do not stack commits on a failing branch.

If you cannot identify focused tests for a commit (e.g. a pure docs commit), say so and skip to the full pass.

#### Full validation pass (once at the end)

On Linux/macOS, the project's Makefile is the canonical entry point:

```bash
gofmt -l .                       # must print nothing new vs. baseline
make test-dev                    # go test + staticcheck (proxy/ scope)
make test-all                    # adds long-running concurrency tests
go build ./...

cd ui-svelte
npm ci                           # only if package-lock.json changed
make test-ui                     # or: npm run check && npm test
npm run build                    # also refreshes proxy/ui_dist
cd ..
```

Compare `staticcheck`/`gofmt` output against the baseline you captured at the start (see *Known noise that is not your problem*). Anything new is yours; anything pre-existing stays.

If any focused test fails after a cherry-pick, **fix it before the next cherry-pick**. Do not stack additional commits on a failing branch.

If proxy tests complain about a missing helper binary, build whatever they ask for under `cmd/` (the test failure usually names the path). The build command is `go build -o <path> ./cmd/<name>` (with a `.exe` suffix on Windows).

### Phase 5 — Reconcile fork features with new upstream surface

Before touching the README, walk through each meaningful new or substantially-changed upstream feature (use `git log --oneline main..HEAD` against the new `main`, plus `git log -p` on anything that looks relevant) and ask:

> Does this new upstream feature interact with, overlap with, or undermine any fork feature documented in `README.md`?

Think in terms of **categories of interaction**, not specific examples:

- **Surface overlap** — upstream now exposes something the fork already exposes. Does the fork's variant still add value? Should it be deprecated, scoped, or removed?
- **Gating gaps** — the fork added an access-control or scoping mechanism. Does any new upstream surface bypass it? If so, extend the mechanism to cover the new surface.
- **Schema/config drift** — upstream extended a config struct/schema the fork also extended. Make sure both extensions still co-exist and validate.
- **UI duplication or inconsistency** — upstream added a UI control that conflicts visually or semantically with a fork-added one. Reconcile.
- **Obsoleted feature** — upstream now solves what a fork commit solved. Skip the commit, document the change.

If integration work is needed, add it as a **new, separately-named commit** on the integration branch (not as an amendment to a replayed fork commit). It becomes a new permanent member of the fork delta and will be replayed by future syncs.

Then update `README.md` so it accurately describes the post-sync state. Minimal edits only. The README is itself part of the fork delta and its replayed commit may need a small follow-up.

### Phase 6 — Promote `release/staging`

Once the integration branch is fully green and the README is accurate:

```bash
git checkout release/staging
git reset --hard "sync/upstream-$TAG"
git push --force-with-lease origin release/staging
```

`--force-with-lease` is mandatory; never `--force`. The `fork-pre-$TAG` tag from Phase 1 is your safety net.

### Phase 7 — `release/stable` promotion (only if explicitly requested)

When (and only when) the maintainer asks to ship:

```bash
git checkout release/stable
git merge --ff-only release/staging   # preferred path
# If FF is impossible because release/staging history was rewritten:
git reset --hard release/staging
git push --force-with-lease origin release/stable
```

If the user has not asked to promote stable, leave `release/stable` alone and tell them it is still pointing at the previous version.

### Phase 8 — Branch cleanup

After a successful sync, the only durable branches that should exist on `origin` are:

- `main`, `release/staging`, `release/stable`
- All `fork-pre-vNNN` tags from this and previous syncs (these are the historical record of pre-sync states)

Delete the temporary integration branch (locally and on `origin` if it was pushed):

```bash
git branch -D "sync/upstream-$TAG"
git push origin --delete "sync/upstream-$TAG"   # only if it was pushed
```

For other stray branches (`pr/*`, `local/*`, abandoned feature branches), confirm their content is preserved either in a tag or in integrated history, then:

```bash
git branch -D <name>                         # local
git push origin --delete <name>              # remote
```

Prune stale remote-tracking refs:

```bash
git fetch --prune origin
git fetch --prune upstream
git remote prune origin
```

**Never** delete or rename anything under `refs/remotes/upstream/`. **Never** delete `fork-pre-*` tags created by previous syncs.

### Phase 9 — Traceability PR (mandatory)

Open a fork-internal PR from the integration branch into `release/staging` *before* the Phase 6 reset. This is **standard practice for every sync**, not optional. The PR is the permanent, reviewable record of:

- which upstream commits were absorbed,
- how each fork commit was reapplied (verbatim, conflict-resolved, or rewritten),
- any reconciliation commits added,
- anything skipped or deprecated.

```bash
gh pr create --base release/staging --head "sync/upstream-$TAG" \
  --title "sync: upstream $TAG + reapply fork features" \
  --body "<concise summary of upstream highlights, replayed fork commits, reconciliation decisions>"
```

Self-approval is blocked on GitHub, so the PR is for visibility, not gating. Closing it via the Phase 6 reset is normal and expected; the PR survives in the repo's PR history as the audit trail.

## Quick checklist (TL;DR)

1. Working tree clean. Read `AGENTS.md` and the fork `README.md`. `git fetch upstream --prune --tags`.
2. Discover the fork delta: `git log --oneline main..release/staging`. Read each commit body. Compute the file overlap with the upstream diff.
3. Capture a baseline of pre-existing `gofmt`/`staticcheck` output on `release/staging`.
4. Tag: `git tag -a fork-pre-vNNN release/staging -m '...' && git push origin fork-pre-vNNN`.
5. Fast-forward `main` to `upstream/main`, push.
6. Branch `sync/upstream-vNNN` from `main`. Cherry-pick `main..release/staging` commits in oldest-first order. Derive focused tests from each commit's diff and run them after each pick. Treat conflicts as features and file renames as relocations.
7. Sanity-check: replayed commit subjects equal the `fork-pre-vNNN..` subject list captured before the sync.
8. Run `go mod tidy` and `npm ci` if upstream changed deps. Run the full validation pass; only the pre-sync baseline warnings are allowed.
9. Reconcile fork features with any new/changed upstream surface; add follow-up integration commits if needed.
10. Update `README.md` only where reality changed.
11. Reset `release/staging` to the integration branch and `--force-with-lease` push.
12. Delete the temporary branch (local and remote). Leave the `fork-pre-vNNN` tag in place.
13. Do not touch `release/stable` unless explicitly asked.

## Environment-specific notes

The procedure above is shell-agnostic and works on Linux, macOS, and any environment with a recent Git, Go toolchain, Node toolchain, and `gh` CLI. Adjust binary suffixes and path separators as your platform requires.

### Windows + VS Code (the maintainer's environment)

This environment has a few quirks worth knowing:

- The `Makefile` `test-dev` and `test-all` targets use Unix-only commands (`mkdir -p`) and fail on Windows. Substitute these direct commands for the full validation pass:
  ```powershell
  .dev/check-go-format.ps1
  go test -short -count=1 ./proxy/...
  go test -race -count=1 -short ./proxy/...
  staticcheck ./proxy/...
  go build ./...

  cd ui-svelte
  npm ci          # only if package-lock.json changed
  npm run check
  npm test
  npm run build
  cd ..
  ```
- The `simple-responder` helper used by some proxy tests must be built with the `.exe` suffix:
  ```powershell
  go build -o build/simple-responder.exe cmd/simple-responder/simple-responder.go
  ```
- PowerShell variable syntax for the tag phase:
  ```powershell
  $tag = "vNNN"
  git tag -a "fork-pre-$tag" release/staging `
    -m "release/staging state immediately before syncing onto upstream $tag"
  git push origin "fork-pre-$tag"
  ```
- `.dev/` contains private helper scripts (`check-go-format.ps1`, `sync-main.ps1`, etc.) and reference diffs of the fork commits. They are not part of the public workflow but are useful when debugging a sync on this machine.
- VS Code's integrated terminal is PowerShell by default; chain with `;` rather than `&&`, and prefer pipelines over `xargs`.

## Lessons learned

Concise pattern notes from past syncs. Add new entries here when a sync teaches something non-obvious; do not rewrite history.

- **README "wholesale replace" conflicts.** Some fork commits exist solely to *replace* an upstream file (e.g. the fork `README.md`). When git reports conflicts, do not hand-merge upstream's new sections in — resolve with `git checkout <fork-commit-sha> -- <file>` to take the fork version verbatim, then `git cherry-pick --continue`. The whole point of those commits is that they overwrite, not merge.
- **Classifying new upstream surfaces vs fork access controls.** When upstream adds a new observability/admin surface (Prometheus `/metrics`, performance dashboard, debug endpoints), explicitly decide whether each fork-introduced gate (e.g. admin PIN) extends to it. The default for this fork is *no extension*: PIN protects only the Activity capture bodies; new upstream surfaces stay exposed unless the maintainer says otherwise. Capture the decision in the README in the same sync.
- **Tag count is absolute, not per-sync.** `vNNN-plus-N` uses `N = git rev-list --count <latest-tag>..upstream/main` measured at sync time, not the count of commits absorbed *this* sync. If `main` was already ahead of the latest upstream tag, `N` will exceed the visible delta and that is correct.
- **PR auto-merge after promotion is fine.** When `release/staging` is force-pushed to the same SHA as the integration branch's tip and the integration branch is deleted, GitHub marks the open PR as `MERGED` automatically. The traceability artefact survives in PR history, which is exactly what we want; no manual PR close is needed.
