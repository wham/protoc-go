# Release flow — proposal

> **Status: proposal, not policy.** Nothing here is wired up yet; protoc-go has
> no tags and no release workflow. Pick an option below and the rest of this
> file gets trimmed down to just that one, which then becomes the policy doc.

## What we're fixing

[gh-slackdump](https://github.com/wham/gh-slackdump) has the right shape:
`scripts/release <patch|minor|major>` bumps the semver tag, pushes it, and a
tag-triggered workflow runs GoReleaser with GitHub-generated release notes.
Small, no bot PRs, no changelog to hand-maintain.

The drawback is real and worth naming precisely: **the release decision is made
after the fact, by a human, whose default is "I just merged something, so
release."** Nothing in the flow ever asks whether the change was worth a
version. So the version number drifts into being a merge counter —
`v0.6.1` exists because a workflow file gained a `permissions:` block, and
`v0.6.0` bundles an auth rewrite with "switch release workflow to
macos-latest". The number stops telling a user anything.

For protoc-go that would be worse, for four reasons that don't apply to
gh-slackdump:

1. **The weekly compliance run commits to main.** `compliance.yml` opens a PR
   every Monday that changes only `README.md`, `docs/badge.json` and
   `docs/history.jsonl`. Under a release-per-merge habit that's ~52 tags a
   year containing zero code.
2. **This is a library, not just a CLI.** Every tag is a version somebody can
   `require` and then has to diff. `go install @latest` resolves off tags, so
   tags are the release whether or not we attach binaries.
3. **`--version` is not ours to stamp.** `scripts/test`'s `cli` suite compares
   `protoc-go --version` stdout byte-for-byte against C++ protoc, and the
   README promises tooling can parse it. Whatever we do, the module version
   must **not** be linked into that string — it needs its own flag.
4. **A release is a compliance claim.** "protoc-go v0.3.0" means "this
   reproduces C++ protoc 35.1". A tag cut from a commit whose suite wasn't run
   is the one thing this project can't ship.

So the goal: **attach the release decision to the change, at the time the
change is made, with "no release" as the default.**

## Recommended — Option A: release label on the pull request

Each pull request carries exactly one label:

| label | meaning |
| --- | --- |
| `release: major` | breaking; at v0.x this is reserved, see [Versioning](README.md#versioning) |
| `release: minor` | new capability, or observable behaviour change |
| `release: patch` | bug fix, no API change |
| `release: none` | docs, CI, refactors, tests — nothing a user can observe |

On push to main, `release.yml` looks up the merged pull request for the commit.
No release label, or `release: none` → it exits 0 and does nothing. A sizing
label → it collects the labels of *every* pull request merged since the last
tag, takes the largest, and cuts that bump.

That last detail is what makes this batch instead of firing per merge: an
unlabeled or `release: none` pull request isn't "never released", it's
"released alongside the next thing that matters". Three docs PRs followed by
one `release: patch` produce exactly one tag, and the docs ride along in it.

**Forgetting to label** is the obvious failure mode, so make it impossible:
`tests.yml` gets a job that fails when a pull request touching `**/*.go` has no
`release:` label. `release: none` is an explicit, one-click answer — the point
is that *somebody chose*, not which way they chose. Failing closed also means a
forgotten label costs you a red check, never a spurious tag.

**Why this one:** it fixes the drawback structurally rather than by discipline,
it keeps commit titles as prose sentences (the history reads like
"Fixed relative type resolution across parent packages", and it should keep
reading that way), the decision happens during review where the person who knows
the blast radius is already looking, and it stays auditable — the label is on
the pull request forever.

**Cost:** two GitHub labels and roughly 60 lines of workflow. No bot PR, no new
file the release process has to write into the repo.

## Alternatives

### Option B — manual, batched, with a nag

Keep gh-slackdump's `scripts/release`, but never run it from a merge. Instead a
`workflow_dispatch` "Release" workflow lists everything unreleased since the
last tag and cuts one version covering all of it, and a scheduled job keeps a
single "Unreleased changes" issue (or a draft release) up to date so it doesn't
get forgotten.

*Pros:* total control over cadence; releases are deliberate and always batched;
zero per-pull-request ceremony. *Cons:* the sizing decision is still made after
the fact and from memory, which is the original problem in a slower form. Good
if releases should be rare and hand-picked.

### Option C — release-please / conventional commits

Squash-merge titles become `feat:` / `fix:` / `docs:` / `chore:`;
[release-please](https://github.com/googleapis/release-please) keeps an open
"chore: release v0.5.0" pull request with the computed version and a generated
`CHANGELOG.md`. Merging it tags; the tag triggers the build.

*Pros:* the most automatic option, batches by construction, gives a real
in-repo changelog, and `docs:`/`ci:`/`chore:` genuinely produce nothing — the
weekly compliance commit is inert as long as its title is prefixed. Well-trodden.
*Cons:* it dictates commit titles, and this repo's history is deliberately
readable prose; it parks a permanently open bot pull request on the repo; and
it's a lot of machinery for a project with one maintainer. Choose it if a
`CHANGELOG.md` is something you actually want.

### Option D — path gating, no human input

Release automatically on every push to main, but skip when the diff touches
only `*.md`, `docs/`, `.github/` and `testdata/`. Always patch, unless a label
says otherwise.

*Pros:* zero ceremony and it does fix the docs case. *Cons:* for code changes
it's still a merge counter, and it can't tell a comment fix from a new feature.
**Not recommended on its own** — but its path check is worth keeping as a guard
inside whichever option wins: refuse to cut a tag when nothing but docs changed
since the last one, so a mislabeled bot pull request can't produce an empty
release.

### Side by side

| | A: label | B: manual | C: release-please | D: paths |
| --- | --- | --- | --- | --- |
| docs/CI changes bump the version | no | no | no | no |
| releases batch | yes | yes | yes | no |
| decision made while the change is fresh | yes | no | yes | n/a |
| commit titles stay prose | yes | yes | **no** | yes |
| in-repo `CHANGELOG.md` | no | no | yes | no |
| standing bot pull request | no | no | yes | no |
| can be forgotten | no (check fails) | yes | no | n/a |

## Rules that apply whichever option wins

These are the protoc-go-specific parts, and they matter more than the choice
above.

1. **A tag is only ever cut from a green commit.** The release job runs
   `scripts/test --summary --json results/summary.json` against the C++ protoc
   the binary claims to mirror (via `.github/install-target-protoc.sh`, same as
   everything else) and refuses to tag on any failure. Not "check that
   `tests.yml` passed" — run it, at tag time, on the exact tree being tagged.

2. **The release notes carry the compliance claim.** Generated, not typed:

   > protoc-go v0.3.0 reproduces **C++ protoc 35.1** — 5497 / 5497 comparisons
   > byte-identical. Go 1.24.7 on ubuntu24.

   Everything under it is GitHub's auto-generated pull request list, as in
   gh-slackdump (`changelog: use: github`). The release page becomes the
   compatibility record, which is the thing users actually need from a protoc
   drop-in.

3. **A target bump is at least a minor.** If the `libprotoc X.Y` string in
   `compiler/cli/cli.go` changed since the last tag, the release is minor or
   larger, whatever the labels say — `--version` output changes and generated
   descriptors change, so no user should get that in a patch. Cheap to enforce
   in the release job, and it takes a judgement call off the table.

4. **The module version never touches `--version`.** `--version` stays
   byte-identical to C++ protoc's — the `cli` suite compares it and the README
   promises it. Stamp the build with
   `-X github.com/wham/protoc-go/compiler/cli.moduleVersion={{.Version}}` and
   expose it under a *new* flag (`--protoc-go-version`), defaulting to `dev`
   for `go build` and to the pseudo-version's blank for `go install`. Adding a
   line to `--version` would turn the compliance suite red, and rightly so.

5. **Binaries are cheap here, so ship them.** protoc-go is pure Go —
   `CGO_ENABLED=0` cross-compiles linux/darwin/windows × amd64/arm64 in one
   ubuntu-latest job, with `-trimpath` and a `checksums.txt`. (gh-slackdump
   needs macOS runners and CGO for the Keychain; nothing here does.) A
   GoReleaser config roughly the size of gh-slackdump's covers it.

6. **Stay on v0 deliberately.** Per [Versioning](README.md#versioning) the
   module version tracks *our* API, not upstream's. While the README carries
   the experimental warning, minor bumps are allowed to break, and there is no
   `/v2` import path to worry about until after a v1.

## What implementing Option A looks like

- `.github/workflows/release.yml` — on push to main: resolve the merged pull
  request, read labels since the last tag, apply the guards from rules 1–3,
  tag, then run GoReleaser.
- `.github/workflows/tests.yml` — one job that fails a pull request touching Go
  files with no `release:` label.
- `.goreleaser.yml` — six binaries, checksums, `changelog: use: github`.
- `scripts/release` — the local escape hatch, same shape as gh-slackdump's, for
  cutting a release by hand when the automation isn't the right tool.
- `scripts/release-notes` — renders the compliance header from
  `results/summary.json`, reusing what `scripts/render-readme` already knows.
- Two labels on the repo, and a line in `AGENTS.md` telling agents to set one.
