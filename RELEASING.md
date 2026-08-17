# Releasing

protoc-go releases when a change is worth a version number, and not otherwise.
The decision is made in the pull request, by a label, before the merge.

## The label

Every pull request that changes shipped code carries exactly one:

| label | meaning |
| --- | --- |
| `release: major` | breaking. Reserved while the module is on v0 — see [Versioning](README.md#versioning) |
| `release: minor` | new capability, or a change to what the compiler outputs |
| `release: patch` | bug fix, no API change |
| `release: none` | refactor, test, or anything else a user cannot observe |

`release: none` is the common answer and a perfectly good one. The point is not
which label but that somebody chose, while the change is fresh and the person
who knows its blast radius is already looking at it.

Pull requests that touch no shipped code — docs, workflows, `testdata/`, the
weekly compliance publish — need no label at all.
[`release-label.yml`](.github/workflows/release-label.yml) enforces this, and
fails closed: a forgotten label costs a red check, never a spurious tag.

## What happens on merge

[`release.yml`](.github/workflows/release.yml) runs on every push to main and
reads the labels of every pull request merged since the last tag.

- All `release: none` (or unlabeled) → **no tag**. Those changes are not lost;
  they ride along in the next release that happens.
- Otherwise → **one release**, sized by the largest label in the range.

So three docs pull requests followed by one `release: patch` produce a single
patch release containing all four. That batching is the whole design: a version
number should mark a change a user can observe, not count merges.

Then, in the same run:

1. **The suite is re-run on the exact tree being tagged**, against the C++
   protoc the binary claims to mirror. Not "the `test` check was green on the
   merge" — run again, here. A tag is a compliance claim and has to be earned by
   the commit it points at.
2. **A target bump is floored at minor.** If `upstreamVersion` in
   `compiler/cli/cli.go` moved since the last tag, the release is at least a
   minor whatever the label said: `--version` output and the emitted descriptors
   both change, and nobody should get that in a patch.
3. **An empty release is refused.** If nothing outside docs, CI and `testdata/`
   changed since the last tag, a sizing label is a mislabel, and the run fails
   rather than publishing a release with nothing in it.
4. **The tag is pushed, then GoReleaser publishes.** One job, because tags
   pushed with `GITHUB_TOKEN` do not trigger workflows — a separate
   tag-triggered publish job would never fire.

Release notes are generated, never typed: a header from
[`scripts/release-notes`](scripts/release-notes) stating which C++ protoc this
build reproduces and how many comparisons matched, over GitHub's changelog of
merged pull requests. The release page is the compatibility record.

Six binaries ship per release — linux, darwin and windows × amd64 and arm64,
plus `checksums.txt`. protoc-go is pure Go, so `CGO_ENABLED=0` cross-compiles
all of them on one runner.

## Cutting one by hand

Run the **Release** workflow from the Actions tab and pick a bump. It skips the
label scan and does everything else — including the compliance run, which is not
skippable by design.

That is also how the first release is cut, since the labels post-date every
merged pull request.

There is no local release script. Version arithmetic lives in
[`scripts/next-version`](scripts/next-version) so it can be read and run by
hand (`scripts/next-version minor`), but tagging goes through the workflow so
that no release can exist without a verification run behind it.

## Two version numbers

Covered in [Versioning](README.md#versioning); the mechanical part:

- `protoc-go --version` prints `libprotoc <upstream>` and must stay
  byte-identical to C++ protoc's output. The `cli` suite in `scripts/test`
  compares it, and tooling parses it expecting protoc's answer.
- `protoc-go --protoc_go_version` prints our version and the upstream one
  together, e.g. `protoc-go v0.1.0 (libprotoc 35.1)`. It is deliberately absent
  from `--help`, which is also compared byte-for-byte.
- Release archives are stamped through `-X ...compiler/cli.moduleVersion`.
  Binaries from `go install ...@v0.1.0` are not stamped, but the go tool records
  the module version in the build info, so they report it anyway.

## Setup

One-time, on the repository: create the four labels `release: major`,
`release: minor`, `release: patch` and `release: none`. Nothing else — no
secrets, no tokens. `GITHUB_TOKEN` covers tagging and publishing.

---

<details>
<summary>Why this shape, and what was considered instead</summary>

[gh-slackdump](https://github.com/wham/gh-slackdump) has the right pieces —
a semver tag triggers GoReleaser, notes come from GitHub — but the release
decision is made after the fact, by a human whose default is "I merged
something, so release". Nothing ever asks whether the change was worth a
version, so the number drifts into being a merge counter: `v0.6.1` exists
because a workflow file gained a `permissions:` block.

That would land harder here. `compliance.yml` merges a README-and-badge-only
pull request every Monday, so release-per-merge means ~52 tags a year containing
no code, and every tag is a library version somebody has to diff.

Three alternatives were weighed:

- **Manual, batched, with a nag.** Keep `scripts/release`, never run it from a
  merge; a scheduled job keeps an "unreleased changes" issue current. Total
  control, but the sizing decision is still made later and from memory, which
  is the original problem in a slower form.
- **[release-please](https://github.com/googleapis/release-please) with
  conventional commits.** The most automatic option, batches by construction,
  produces a real `CHANGELOG.md`. Rejected because it dictates commit titles
  and this repo's history is deliberately readable prose, and because a
  permanently open bot pull request is a lot of machinery for one maintainer.
  Worth revisiting if a `CHANGELOG.md` becomes something we want.
- **Path gating with no human input.** Release on every push to main, skipping
  docs-only diffs. Zero ceremony and it does fix the docs case, but for code
  changes it is still a merge counter and cannot tell a comment fix from a
  feature. Its path check survives as the empty-release guard above.

</details>
