---
name: upstream-sync
description: '**WORKFLOW SKILL** - Sync this fork with mostlygeek/llama-swap, update from upstream, rebase onto an upstream release, prepare or refresh an upstream candidate, organize multiple PR candidates, or rebuild release/staging while preserving fork-only changes. USE WHEN the user says "sync the fork", "pull upstream", "merge upstream vNNN", "upstream candidate", "PR candidate", "candidate branch", or asks for a fork release based on a newer upstream tag. Covers durable candidate branches, issue-bound open PR branches, first-parent fork-delta discovery, dirty-worktree preservation, validation, review checkpoints, force-with-lease staging promotion, and cleanup. DO NOT USE FOR routine fixes unrelated to upstream or candidate history.'
---

# Upstream Sync and Candidate Workflow

Use this workflow to keep the fork close to upstream while preserving two
separate kinds of work:

1. independently reviewable changes that may be proposed upstream; and
2. code and documentation that intentionally exist only in this fork.

The distinction must remain visible in Git history. Do not infer it from a
commit subject or from whether an issue already exists.

## Repository identity

- `origin` is `sousekd/llama-swap`.
- `upstream` is `mostlygeek/llama-swap`.
- `upstream` fetches only `main` using
  `+refs/heads/main:refs/remotes/upstream/main`.
- Never push to `upstream` or modify an `upstream/*` ref.

Verify before changing history:

```bash
git remote -v
git config --get-all remote.upstream.fetch
git fetch upstream --prune --tags
git fetch origin --prune
```

## Ref taxonomy

| Ref | Lifetime | Meaning |
| --- | --- | --- |
| `main` | durable | Exact, fast-forward-only mirror of `upstream/main`. |
| `release/staging` | durable | Tested integration of upstream, candidate merges, and local fork delta. |
| `release/stable` | durable | Explicitly promoted deployable checkpoint. Never move it during a sync unless the maintainer separately asks. |
| `candidate/<name>` | durable while unresolved | Issue-free, upstream-clean change that may be proposed upstream. |
| `open-pr/issue-<number>-<name>` | life of an actual PR | Issue-bound branch used only after the maintainer identifies or opens the required upstream issue. |
| `sync/upstream-<label>` | temporary | Integration branch for one upstream sync. |
| `test/<name>` | temporary | Candidate plus the current fork composition for integration or deployment testing. |
| `local/<name>` | temporary | Optional scratch branch for fork-only work. |
| `fork-pre-<label>` | permanent tag | Exact rollback point before rewriting `release/staging`. |

`candidate/*` is not a synonym for an open PR. A candidate may exist before an
issue, may wait indefinitely, and may never be submitted. Several candidate
branches may coexist even though upstream limits a new contributor to one open
PR at a time.

The old local `pr/*` tag convention is retired. Do not create, preserve, or use
`pr/*` tags. Candidate state belongs in `candidate/*`; live PR state belongs in
`open-pr/issue-*`; rollback state belongs in `fork-pre-*`.

## Staging history contract

Build `release/staging` with this graph shape:

```text
upstream main
  |\
  | candidate commit E
  |/
  M  Merge candidate/model-action-errors
  |\
  | candidate commit F
  |/
  M  Merge candidate/freeze-swaps
  |
  P  local code commit
  W  fork-docs commit
  R  fork-docs commit
```

Rules:

- Merge every candidate with `--no-ff`. The candidate commit remains the exact
  second-parent commit; do not cherry-pick a duplicate onto staging.
- Use merge subjects of the form `Merge candidate/<name>`.
- Put fork-only code and `fork-docs:` commits directly on the first-parent
  line after candidate merges.
- Do not merge a local-only feature branch. Its commit itself is the canonical
  first-parent fork delta.
- Keep candidate commits free of local README, workflow, deployment, PIN, or
  other fork-only context.

These are the authoritative discovery commands:

```bash
# Candidate integration points and their second parents.
git log --first-parent --merges --oneline main..release/staging

# Local-only code and fork documentation, in replay order.
git log --first-parent --no-merges --reverse --oneline \
  main..release/staging

# The complete composed difference, for review rather than replay selection.
git diff --stat main..release/staging
```

Never use an unrestricted `git log main..release/staging` as the replay list.
It traverses candidate second parents and would misclassify or duplicate them.

## Commit categories

| Category | Subject | Placement |
| --- | --- | --- |
| Candidate code | `area: summary` | `candidate/<name>`, then staging second parent via `--no-ff` merge. |
| Local-only code | `area: summary` | Staging first-parent line. |
| Fork-only docs/meta | `fork-docs: summary` | Staging first-parent line. |
| Candidate integration | `Merge candidate/<name>` | Staging first-parent merge commit. |

All code uses an upstream-style subject and an honest body. A local-only code
commit is distinguished by graph position, not by a `fork-` subject prefix.
Reserve `fork-docs:` for material that makes sense only inside this fork.

Follow the message and test rules in `AGENTS.md`, including hard-wrapping commit
messages to 80 columns.

## GitHub contribution boundary

Read the current upstream `CONTRIBUTING.md` before preparing an upstream
submission.

An agent may:

- prepare and review candidate code;
- push candidate or issue-bound branches to this fork;
- provide diffs, test results, changed-commit mappings, and review facts.

An agent must not:

- search for, create, write, edit, close, or comment on an issue or PR on the
  maintainer's behalf;
- invent an issue merely because a candidate exists;
- draft the maintainer's issue or PR description.

The maintainer identifies or opens the required issue, writes the issue and PR,
and opens the PR. Only then derive an `open-pr/issue-<number>-<name>` branch.
Never open an upstream PR from `release/staging` or `test/*`.

## Dirty-worktree preservation

Do not discard or casually move uncommitted work during a sync. Classify it
first. If it belongs to a candidate that must move onto the new upstream base:

```bash
git status --short --branch
git stash push --include-untracked \
  --message "candidate/<name> before <label>"
git stash list -n 1
git stash show --stat --include-untracked 'stash@{0}'
git status --short --branch
```

On PowerShell, quote `stash@{0}` exactly as shown. Apply the named stash only on
the intended candidate branch. Do not drop it until the candidate commit is
validated, pushed, and its remote SHA is verified.

If pending changes mix unrelated work, split or preserve them before the sync.
Do not hide them in a sync commit.

## Candidate lifecycle

### Pending candidate

Start from the current mirror, never from staging:

```bash
git switch main
git pull --ff-only
git switch -c candidate/<name> main
```

Implement only the upstream-capable change. Keep tests in the candidate when
they verify that behavior. Run focused validation immediately after the first
substantive edit, then the full relevant suite.

At the candidate review checkpoint show:

```bash
git status --short --branch
git diff --check
git diff --stat main
git diff main -- <candidate paths>
```

Stop before finalizing or pushing unless the maintainer already approved an
execution plan that explicitly authorizes those steps. After approval, commit
and push:

```bash
git push --set-upstream origin candidate/<name>
```

A pushed candidate is still not an issue or PR.

### Integration testing

To test one or several candidates with the current fork composition, build a
fresh branch from `main`; do not rebase the whole staging branch onto a
candidate:

```bash
git switch -c test/<name> main
git merge --no-ff candidate/<first> -m "Merge candidate/<first>"
git merge --no-ff candidate/<second> -m "Merge candidate/<second>"
```

Then replay only the old staging first-parent non-merge commits, oldest first.
Record the old `main` SHA before changing it so the range remains unambiguous:

```bash
git rev-list --first-parent --reverse --no-merges \
  <old-main>..release/staging
```

Cherry-pick those local commits one at a time and validate after each. If the
new candidate composition needs a local compatibility fix, commit that fix on
the first-parent line. Never fold local compatibility code into a candidate
unless it genuinely belongs in the upstream submission.

### Live upstream PR

After the maintainer supplies the existing issue number:

```bash
git switch -c open-pr/issue-<number>-<name> candidate/<name>
```

Add only submission-specific metadata required by the issue discussion. The
maintainer writes and opens the PR. Keep this branch while the PR is open and
delete it after merge or closure.

### Resolution

- If upstream merges the candidate, advance `main`, omit the now-upstream
  feature from candidate merges, and delete its candidate branch after the next
  staging sync proves the upstream implementation.
- If upstream rejects it or the maintainer makes it permanently local, document
  that decision in the fork README. Rebuild it as a first-parent local commit
  before deleting the candidate branch.
- If no decision exists, keep the candidate branch. Do not delete it merely
  because a test branch or sync finished.

## Upstream sync

### 1. Preflight and baseline

1. Read `AGENTS.md`, `CONTRIBUTING.md`, and the fork README.
2. Fetch both remotes and verify that `upstream/main` still points to the
   reviewed SHA.
3. Record:
   - old `main`, staging, stable, and candidate SHAs;
   - the candidate merge list;
   - the first-parent local replay list;
   - working-tree and stash state.
4. Capture format, static-analysis, Go-test, and UI-test baselines. Upstream
   failures are not sync regressions, but they must be reported.
5. Preserve dirty work as described above.

Compute the upstream label from the newest tag merged into upstream. Use
`vNNN-plus-N` when commits exist beyond that tag, or `sha-<short>` when no tag
exists.

### 2. Create the rollback tag

```bash
git tag -a fork-pre-<label> release/staging \
  -m "release/staging immediately before syncing onto upstream <label>"
git push origin fork-pre-<label>
```

The tag is permanent. Verify the remote object before rewriting any branch.

### 3. Advance `main`

```bash
git switch main
git merge --ff-only upstream/main
git push origin main
```

Confirm local `main`, `origin/main`, and `upstream/main` are identical. Stop if
fast-forward is impossible.

### 4. Port every active candidate

For each durable `candidate/*` branch:

1. Start from the new `main`.
2. Reapply or rebase only that candidate's commits.
3. Treat conflicts as feature ports: preserve intent on the new owning code,
   not stale surrounding text.
4. Verify the candidate has no local-only imports, docs, configuration, or
   assumptions.
5. Run focused and full relevant validation.
6. Review, then update the remote candidate with `--force-with-lease` when its
   history changed.

A candidate must build and test independently. Do not use another candidate or
the PIN/local delta to make it pass.

### 5. Build the integration branch

```bash
git switch -c sync/upstream-<label> main
git merge --no-ff candidate/<first> -m "Merge candidate/<first>"
git merge --no-ff candidate/<second> -m "Merge candidate/<second>"
```

Use a stable documented order. Validate after each merge. Candidate branches
must remain independent even though staging composes them.

### 6. Replay local-only commits

Use the rollback tag and saved old-main SHA:

```bash
git rev-list --first-parent --reverse --no-merges \
  <old-main>..fork-pre-<label>
```

Record an old-to-new mapping when commits are intentionally consolidated,
split, replaced, or dropped. Replay each remaining local commit one at a time.
Validate after every code commit. Pure docs commits may be consolidated when the
planned mapping says so.

Conflict rules:

- preserve upstream behavior added since the old base;
- preserve every candidate already merged into integration;
- reimplement local intent against the new owning abstraction;
- do not pull candidate changes onto the local first-parent commit;
- skip a feature only when upstream demonstrably replaces it, and document why.

### 7. Reconcile and document

Review each new upstream surface against every candidate and local feature:

- duplicated or obsoleted behavior;
- new routes that bypass a local UI or access-control boundary;
- config/schema overlap;
- sidebar or other UI collisions;
- changed scheduler, router, process, or response contracts.

Keep reconciliation scoped. Candidate fixes belong on the candidate branch and
must be remerged. Local compatibility fixes belong on integration's
first-parent line. Update the README to classify active candidates, local-only
features, and previously retired features accurately.

### 8. Validate

Derive focused tests from each changed commit. Then run the complete available
suite.

Linux/macOS:

```bash
gofmt -l .
make test-dev
make test-all
go build ./...
cd ui && npm run check && npm test && npm run build
```

Windows in this repository:

```powershell
.dev/check-go-format.ps1
go test -short -count=1 ./internal/...
go test -race -count=1 -short ./internal/...
staticcheck ./internal/...
go build ./...
npm --prefix ui run check
npm --prefix ui test
npm --prefix ui run build
```

Build `build/simple-responder.exe` if proxy tests request it. Compare all
warnings and failures with the captured upstream/pre-sync baseline. Do not fix
unrelated upstream noise in a sync.

Manually exercise user-visible interactions when automated tests cannot cover
them. A production build alone is not a smoke test.

### 9. Mandatory integration review checkpoint

Before moving or pushing `release/staging`, show:

```bash
git log --graph --decorate --oneline main..HEAD
git log --first-parent --merges --oneline main..HEAD
git log --first-parent --no-merges --oneline main..HEAD
git diff --stat main..HEAD
git diff --check main..HEAD
git status --short --branch
git branch --list 'candidate/*' 'open-pr/*' 'sync/*' 'test/*' 'local/*'
git tag --list 'fork-pre-*' --sort=-creatordate
```

Also report:

- old-to-new commit mapping;
- candidate branch and remote SHAs;
- conflicts and their semantic resolutions;
- focused/full test results and known baseline failures;
- stable branch SHA and the fact that it was not moved.

Push the integration branch only when useful for review; do not open or draft a
PR. Stop and wait for explicit approval. Do not reset or push staging at this
checkpoint.

### 10. Promote staging after approval

```bash
git switch release/staging
git reset --hard sync/upstream-<label>
git push --force-with-lease origin release/staging
git branch --set-upstream-to=origin/release/staging release/staging
```

Never use plain `--force`. Verify local and remote staging SHAs and clean status.
Do not move `release/stable` unless separately requested.

### 11. Cleanup

After successful promotion:

- keep every unresolved `candidate/*` branch;
- keep a real `open-pr/*` branch only while its PR is open;
- permanently keep all `fork-pre-*` tags;
- delete `sync/*`, `test/*`, and `local/*` branches and worktrees after their
  content is preserved;
- drop consumed preservation stashes;
- prune both remotes;
- finish on clean `release/staging` tracking `origin/release/staging`.

Do not delete or rename anything under `refs/remotes/upstream/`.

## Fast acceptance checklist

- `main == origin/main == upstream/main`.
- Every `candidate/*` branch is based on `main` and works independently.
- Staging first-parent merges are exactly the intended candidate set.
- Staging first-parent non-merges are exactly local code and `fork-docs:`.
- No `pr/*` tags exist.
- No issue or PR was created or written by the agent.
- `release/stable` is unchanged unless explicitly promoted.
- Validation has no candidate/fork regressions beyond documented baseline noise.
- The worktree, temporary refs, worktrees, and stashes are clean after promotion.
