## Guidelines

- See below for how to run and test.
- Only add code comments for really tricky parts; otherwise keep it clean.
- Don't commit changes to `status.txt` — it's managed by ralph.sh.
- Use past tense in commit messages (e.g., "Fix bug" → "Fixed bug").
- Keep pull-request descriptions extremely short — one sentence, and stop. No bullet lists, no markdown headers, no summary of the diff; the diff is right there.
- Put exactly one `release:` label on every PR that changes shipped code (`.go`, `go.mod`, `go.sum`, `.goreleaser.yml`). `release: none` is the usual answer; a sizing label cuts a release on merge. See [Versioning](README.md#versioning).
- After a PR is merged, always reset the branch to latest main before starting new work. Never push additional commits to a branch whose PR is already merged — create a fresh branch or reset the existing one.
- When I prompt you to make changes that are radically different from what's documented here, please update this file accordingly.

## What This Is

This is a port of the Protocol Buffers compiler (`protoc`) from C++ to Go. The Go implementation must produce **identical CodeGeneratorRequest payloads** to the C++ protoc when invoking plugins. This means: same FileDescriptorProto structure, same source code info, same error messages.

## Directory Structure

```
/
├── cmd/protoc-go/          # Entry point (mirrors C++ compiler/main.cc)
├── compiler/
│   ├── cli/                # CommandLineInterface (mirrors command_line_interface.cc)
│   ├── parser/             # Parser (mirrors compiler/parser.cc)
│   ├── importer/           # Importer + source tree (mirrors compiler/importer.cc)
│   └── plugin/             # Plugin subprocess (mirrors compiler/subprocess.cc + plugin.cc)
├── io/tokenizer/           # Tokenizer (mirrors io/tokenizer.cc)
├── descriptor/             # DescriptorPool (mirrors descriptor.cc)
├── testdata/               # Test .proto fixtures (numbered)
│   └── google/             # Vendored upstream protobuf test corpus (see below)
├── tools/
│   ├── protoc-gen-dump/    # Fake plugin that captures CodeGeneratorRequest
│   ├── protoc-gen-mock/    # Misbehaving plugin for response-protocol tests
│   └── protoc-bin/         # (reserved) Vendored C++ protoc if needed
├── scripts/
│   ├── test                # Correctness harness — compares C++ protoc vs Go protoc-go
│   ├── bench               # Performance harness — times C++ protoc vs Go protoc-go
│   ├── merge-summaries     # Reassembles sharded `scripts/test` runs into one verdict
│   ├── render-readme       # Renders harness output into the README compliance block
│   ├── release-notes       # Renders the compliance header for a GitHub release
│   ├── next-version        # Semver arithmetic over existing tags
│   ├── gen-large-stress    # Generates scaled stress/bench corpus (tiers)
│   └── find-protoc         # Locates system C++ protoc
├── RALPH.md                # Builder agent prompt (automated loop)
├── NELSON.md               # Adversarial tester prompt (automated loop)
├── ralph.sh                # Loop orchestrator
└── status.txt              # RALPH/NELSON communication
```

## How To Build and Test

```bash
# Build everything and run tests (compares Go protoc-go vs C++ protoc)
scripts/test

# Summary only (no diff output)
scripts/test --summary

# Machine-readable summary (for publishing; requires jq)
scripts/test --summary --json results/summary.json

# One slice of the suite, for splitting it across machines (see Sharding below)
scripts/test --summary --shard 2/4 --json results/shard-2.json

# Performance comparison (C++ protoc vs Go protoc-go) on a scaled corpus
scripts/bench --summary
# In-process library core (ns/op, B/op, allocs/op)
go test ./protoc/ -run='^$' -bench=. -benchmem
```

The test harness:
1. Builds `cmd/protoc-go/`, `tools/protoc-gen-dump/`, and `tools/protoc-gen-mock/`
2. Verifies the discovered C++ protoc is the version `protoc-go --version`
   declares as its target — a mismatched run is marked `status=error` in the
   JSON summary and cannot be published
3. For each `testdata/*/` directory × 10 profiles, runs both C++ protoc and Go
   protoc-go. Profiles: `plugin`, `plugin_param`, `descriptor_set`,
   `descriptor_set_src`, `descriptor_set_full`, `descriptor_set_retain`,
   `multi_plugin`, `multi_opt`, `plugin_descriptor`, `colon_param`
4. Also runs: CLI error tests (`cli@`), stdin/decode tests (`stdin@`,
   `decode@`), partial-generation (`partial@`), determinism (`determinism@`),
   the vendored upstream corpus (`google@`, see below), plugin response
   protocol tests (`mock@`), and PATH-based plugin discovery (`pathplugin@`)
5. Test names: `<case>@<profile>` (e.g., `01_basic_message@plugin`, `cli@no_args`)
6. Reports pass/fail with diffs

Harness invariants (deliberate, keep them):
- A successful compile whose comparison artifacts (request.pb, summary.txt,
  parameter.txt, descriptor sets) are missing on either side is a FAILURE,
  never a skip — a comparison that silently didn't happen must not count as a
  match. Fixture dirs with no top-level `.proto` files are not registered
  (container dirs for CLI tests); if a run function still meets one, that's a
  FAILURE too.
- protoc-gen-dump treats the segment after the LAST comma of its parameter as
  the output directory, so the dir must always be the final `--dump_opt`. The
  full raw parameter is written to `parameter.txt` and compared between
  compilers (with each side's output dir normalized to `<OUT>`).
- Compiler stdout is captured and compared in every suite, and exit codes are
  compared in the CLI/mock suites.

### Vendored Google corpus

`testdata/google/` carries the `.proto` test corpus from
protocolbuffers/protobuf (BSD license included) at the release protoc-go
targets. Unlike the numbered fixtures these were not written alongside this
port, so they don't share its blind spots. Each file is compiled standalone in
`plugin` and `descriptor_set_full` mode from the corpus root. The same corpus
is the real-world row of the performance harness, compiled there as one
invocation rather than file by file. When the target
C++ protoc version moves, refresh the corpus from the matching upstream tag
(`https://raw.githubusercontent.com/protocolbuffers/protobuf/v<VER>/src/google/protobuf/<file>.proto`).

A registered test that writes no verdict (usually a missing tool — `xxd` drives
the `stdin@`/`decode@` suites) is reported as a `no_result` warning and is NOT
counted as a pass. `--json` marks such a run `"error"` so it cannot be
published: a suite that silently shrinks must never read as all-match.

### Sharding the correctness suite

`scripts/test --shard i/N` runs slice `i` of `N`. Every test registers through
one `launch()` helper in a fixed, machine-independent order (the testdata glob
is sorted; every other suite is a literal array), and a shard keeps the entries
whose position is its own. So the shards partition the suite exactly — no
coordination, no shared list, no case counted twice — and adding a test case
reshuffles the slices without any of them needing to be told.

A shard's `--json` output describes its slice only, and is marked
`"partial": true`; `scripts/render-readme` refuses it. `scripts/merge-summaries`
is the only way back to a publishable verdict, and it refuses to merge unless
every index `1..N` is present exactly once and all shards agree on the commit,
the C++ protoc, the protoc-go build and the Go toolchain — so a shard that died
cannot quietly shrink the published total.

```bash
scripts/test --summary --shard 1/4 --json results/shard-1.json   # ×4, in parallel
scripts/merge-summaries --out results/summary.json results/shard-*.json
```

An unsharded run is just `--shard 1/1` and needs no merge.

### Performance harness

`scripts/bench` is the performance counterpart to `scripts/test`. It runs both
compilers on a curated corpus — serial, warmed up, median-of-N — and reports
per-case latency plus the Go/C++ ratio, separating compile throughput from
process-startup cost.

The corpus is the size-scaled generated tiers (which isolate how each compiler
scales, since only one dimension changes between them) plus one `google_corpus`
row: the whole vendored upstream corpus (`testdata/google/`, 53 files) compiled
in a single invocation. The generated tiers are uniform by construction, so
they say nothing about the mix a real project actually contains — imports and
public imports, custom options, extensions, editions, generic services. That
row is the one that does, and it is the honest headline number. It adds ~35s to
the run.

Every row is gated on a clean compile from both compilers before it is timed: a
failed compile is not a measurement of compiling, and the two give up at
different points, so timing one would compare error paths. Rows that don't
compile are dropped from the table with a reason rather than published. The
`google_corpus` row needs the C++ protoc include directory (four corpus files
import `google/protobuf/cpp_features.proto` or `java_features.proto`, which
protoc ships, protoc-go does not embed and the corpus does not vendor), so a
Go-only run without protoc installed drops it instead of benchmarking a subset.

```bash
scripts/bench                 # C++ protoc vs Go protoc-go, human table
scripts/bench --summary       # table only; writes results-bench/bench.{json,md}
```

Each timed row also reports peak memory (max RSS, median of `--mem-runs` runs,
default 3) for both compilers, read from the kernel's rusage accounting via
GNU/BSD `time` or a python3 fallback — the plugin variant includes the plugin
subprocess on both sides. Memory is informational only: verdicts stay
wall-clock, and rows read `n/a` when no reader is available.

`tests.yml` runs this harness (tiny/small/medium tiers) on every pull request
and posts `bench.md` as a sticky comment on the pull request, updated in place
on each push.

If `hyperfine` is installed it drives the timing for better statistics;
otherwise a built-in median-of-N loop is used. C++ `protoc` is optional — when
absent, only Go numbers are reported. For the in-process library core (no
process-startup noise, plus `B/op` and `allocs/op`), use Go's native
benchmarks — same cases, `google_corpus` included:

```bash
go test ./protoc/ -run='^$' -bench=. -benchmem
```

### Published compliance results

`.github/workflows/compliance.yml` runs both harnesses weekly and publishes the
result to the README, so the compliance claim on the home page is evidence
rather than assertion. The chain is deliberately machine-readable end to end —
nothing parses human output:

```
correctness ×4 (sharded)                     ┐
  scripts/test --shard i/4 --json            │
    → results/shard-i.json                   │
    → scripts/merge-summaries                │
      → results/summary.json  ───────────────┼→ scripts/render-readme → README block
                                             │                        → docs/badge.json
performance ×1                               │                        → docs/history.jsonl
  scripts/bench → results-bench/bench.json  ─┘
```

Correctness and performance share no state and no longer queue behind each
other; correctness is split four ways with `--shard`, and the shard count is
`strategy.job-total`, so changing the `shard:` matrix is the only edit needed to
change the width. Perf gets a runner that has *not* just spent minutes
saturating every core with the correctness suite, which is the right way round
for the numbers.

Shape and rough cost of a weekly run (measured on a 4-core box):

| leg | wall | note |
| --- | ---: | --- |
| correctness, per shard | ~35s | ~1,370 of 5,497 comparisons |
| performance | ~20min | `bench_large`/`plugin` alone is ~17min of it |

The performance leg is the critical path and sharding it does not help: one row
(`bench_large` × `plugin`, 13 timed invocations of each compiler at ~40s each)
sets the floor. Splitting a compiler's timing across machines is not an option —
the go-vs-cpp comparison in a row is only meaningful when both halves ran on the
same machine — so the only levers there are `--runs` or dropping the `large`
tier from the weekly `--sizes`, both of which weaken the published evidence.

A shard that finds real differences does not abort the run: `scripts/test` exits
non-zero, the step is `continue-on-error`, and the merged `fail` status is
published in red. Abandoning the run instead would leave the README showing the
last green week indefinitely. A shard that produces *no* summary is the opposite
case and is refused outright by the merge.

`scripts/render-readme` rewrites only the region between the
`<!-- BEGIN COMPLIANCE -->` / `<!-- END COMPLIANCE -->` markers; the rest of the
README is hand-written and must stay that way. It refuses to render a summary
whose status is `error` or that is one unmerged shard of a distributed run,
renders `fail` honestly in red, and drops performance verdicts when the bench
run was not timed by hyperfine.

The rendered results are published as a pull request from the disposable
`compliance/results` branch, not pushed straight to main: main's ruleset
requires the `test` status check, which a direct push has no way to satisfy.
That branch is force-pushed every run, so an already-open pull request is
updated in place rather than piling up. The pull request body carries the
bench table (`results-bench/bench.md`), rebuilt on every run, so the week's
numbers are visible without opening artifacts.

Two things have to be true for that pull request to open and merge on its own.
"Allow GitHub Actions to create and approve pull requests" (Settings → Actions →
General) must be on, or the create call is rejected and the results never leave
the branch. And because no workflow run is created for events the default
`GITHUB_TOKEN` causes, neither the push nor the pull request wakes tests.yml,
which would leave the required `test` check unreported forever; `workflow_dispatch`
is the one event that token may raise, so the publish step asks tests.yml to run
against the results branch by name. That run reports `test` on the results commit
just as a `pull_request` run would, which is why tests.yml carries a
`workflow_dispatch` trigger it is otherwise never invoked through.

Setting a `COMPLIANCE_TOKEN` secret (fine-grained PAT or GitHub App token,
contents and pull-requests write) sidesteps both: its pull request arrives with
CI already running and the dispatch is redundant.

The C++ version to verify against is not pinned in the workflow — it is read
from `protoc-go --version`, so the compiler itself declares its target and CI
cannot drift from it. A second job compares that target against the newest
upstream release and, when they differ, opens an `upstream-drift` issue
reporting how many comparisons still match on the new release.

```bash
scripts/render-readme --check   # fail if the committed block is stale
```

The scaled corpus is generated on demand by `scripts/gen-large-stress <tier>`
(`tiny`/`small`/`medium`/`large`/`xl`) into `testdata/bench/` (gitignored).

## Automated Development Loop

```bash
./ralph.sh          # start the RALPH/NELSON adversarial loop
```

- **RALPH** (builder) fixes failing tests one at a time.
- **NELSON** (adversarial tester) creates new tests to find bugs.
- The loop continues until NELSON can't break it.

## Key Design Decisions

- **Comparison surface**: We compare CodeGeneratorRequest payloads sent to plugins. If both compilers send identical requests, the port is correct.
- **Go package layout mirrors C++**: Each Go package corresponds to a C++ source file/directory in the protobuf repo.
- **Reuse existing proto types**: We use `google.golang.org/protobuf/types/descriptorpb` for FileDescriptorProto etc.
- **No built-in generators**: The Go protoc is plugin-only. No C++/Java/Python code generators.
- **Fake plugin**: `protoc-gen-dump` captures what protoc sends to plugins. It writes JSON + binary + human-readable summary.

## C++ protoc Pipeline (What We're Porting)

```
.proto files → Tokenizer → Parser → Importer → DescriptorPool → CommandLineInterface → Plugin
                (lexer)    (AST)   (imports)   (validate/link)   (orchestrate)        (subprocess)
```

Key C++ files:
- `io/tokenizer.cc` — lexer (~1800 lines)
- `compiler/parser.cc` — parser (~2500 lines)
- `compiler/importer.cc` — import resolution (~500 lines)
- `descriptor.cc` — descriptor pool + validation (~9000 lines)
- `compiler/command_line_interface.cc` — CLI (~3000 lines)
- `compiler/subprocess.cc` — plugin subprocess (~300 lines)
