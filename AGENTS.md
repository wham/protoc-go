## Guidelines

- See below for how to run and test.
- Only add code comments for really tricky parts; otherwise keep it clean.
- Use past tense in commit messages (e.g., "Fix bug" → "Fixed bug").
- Keep pull-request descriptions extremely short — one sentence, and stop. No bullet lists, no markdown headers, no summary of the diff; the diff is right there.
- Put exactly one `release:` label on every PR that changes shipped code (`.go`, `go.mod`, `go.sum`, `.goreleaser.yml`). `release: none` is the usual answer; a sizing label cuts a release on merge. See [Development](README.md#development).
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
└── scripts/
    ├── test                # Correctness harness — compares C++ protoc vs Go protoc-go
    ├── bench               # Performance harness — times C++ protoc, Go protoc-go, buf
    ├── merge-summaries     # Reassembles sharded `scripts/test` runs into one verdict
    ├── render-readme       # Renders harness output into the README compliance block
    ├── release-notes       # Renders the compliance header for a GitHub release
    ├── next-version        # Semver arithmetic over existing tags
    ├── gen-large-stress    # Generates scaled stress/bench corpus (tiers + multi)
    └── find-protoc         # Locates system C++ protoc
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

# Performance comparison (C++ protoc vs Go protoc-go vs buf) on a scaled corpus
scripts/bench --summary
scripts/bench --summary --no-buf   # two-way, when buf is installed but not wanted
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

`scripts/bench` is the performance counterpart to `scripts/test`. It runs each
available compiler on a curated corpus — serial, warmed up, median-of-N — and
reports per-case latency plus the ratio against C++ protoc, separating compile
throughput from process-startup cost.

Three implementations are timed: C++ protoc, Go protoc-go, and
[buf](https://github.com/bufbuild/buf). C++ protoc and buf are both optional —
their columns read `n/a` when they are not installed, and `--no-buf` drops buf
from a run that has it. buf is here because it is what a lot of projects
actually run, not because it is a port target: this project's correctness
contract is with C++ protoc alone, and `scripts/test` says nothing whatsoever
about buf. See [Comparing against buf](#comparing-against-buf) for what is and
is not comparable.

The corpus is the size-scaled generated tiers (which isolate how each compiler
scales, since only one dimension changes between them), one `bench_multi` row
(the same generator emitting 42 files in one flat directory with a real import
graph, since every scaled tier is a single file and so measures nothing about
import resolution or a compiler's ability to parse files concurrently — all
three produce byte-identical descriptor sets on it), plus one `google_corpus`
row: the whole vendored upstream corpus (`testdata/google/`, 53 files) compiled
in a single invocation. The generated tiers are uniform by construction, so
they say nothing about the mix a real project actually contains — imports and
public imports, custom options, extensions, editions, generic services. That
row is the one that does, and it is the honest headline number. It adds ~10s to
the run.

Every implementation is gated on a clean compile of a row before it is timed on
it: a failed compile is not a measurement of compiling, and the three give up at
different points, so timing one would compare error paths. The gate is **per
implementation**, not per row — a row buf cannot compile still publishes its
C++/Go numbers, with buf's cells reading `n/a` and the compiler's own first
error printed under the table. (Go protoc-go failing is the one case that drops
the whole row: this harness exists to place protoc-go against the others, so
there is nothing left worth timing.) The `google_corpus` row needs the C++
protoc include directory (four corpus files import
`google/protobuf/cpp_features.proto` or `java_features.proto`, which protoc
ships, protoc-go does not embed and the corpus does not vendor), so a Go-only
run without protoc installed reports it Go-only instead of benchmarking a
subset.

The `plugin` rows run protoc-gen-dump with `PROTOC_GEN_DUMP_SKIP_JSON=1`, which
drops the plugin's `request.json` debugging dump (150MB on the `large` tier).
Every compiler inherits the variable, so the row stays a fair comparison — it is
simply one that measures the compilers rather than the fixture between them.
That distinction is not cosmetic: while the fixture dominated a row, its cost
sat on both sides of the ratio and dragged it toward 1.00, so `bench_large`
published `0.38` (`go`) on `descriptor` and `0.97` (`tie`) on `plugin` for the
same corpus. Anything added to protoc-gen-dump that is not a comparison
artifact belongs behind that switch for the same reason.

```bash
scripts/bench                 # C++ protoc vs Go protoc-go vs buf, human table
scripts/bench --summary       # table only; writes results-bench/bench.{json,md}
scripts/bench --no-buf        # skip buf even when it is installed
```

Each timed row also reports peak memory (max RSS, median of `--mem-runs` runs,
default 3) for every compiler, read from the kernel's rusage accounting via
GNU/BSD `time` or a python3 fallback — the plugin variant includes the plugin
subprocess on every side. The tables show the raw go/cpp ratio for time and
memory rather than verdict columns; the noise-aware wall-clock verdict is still
computed into `bench.json` per row as `verdict`, where the README tally
consumes it. Memory rows read `n/a` when no reader is available.

**buf is reported, not scored.** It carries its own time and memory columns but
no ratio against C++ protoc, and no verdict — there is no `buf_ratio` or
`verdict_buf_*` in `bench.json`. It is a third implementation offered for
reference, and readers can divide for themselves; the ranking machinery is
reserved for the go/cpp pair, which is the one this project is a port of.

The same rusage read also records CPU time (user+sys) per row as `*_cpu_ms` in
`bench.json`. It is deliberately kept out of the tables and deliberately
collected: the three do not use cores the same way — C++ protoc is
single-threaded, protoc-go parses files in parallel, buf compiles through a
worker pool — so wall-clock alone does not separate *faster* from *spends fewer
cycles*. Wall-clock stays the headline, because it is what a developer waits
for, but a row whose `cpu_ms` far exceeds its `ms` got there with cores rather
than efficiency, and only `cpu_ms` says so.

There is exactly one performance table, and it looks the same everywhere it is
read — the run's own log, `results-bench/bench.md` (which the weekly compliance
pull request quotes), and the README block:

| case | variant | cpp ms(±sd) | go ms(±sd) | buf ms(±sd) | go/cpp | cpp peak MB | go peak MB | buf peak MB | go/cpp |

`scripts/bench` formats it once (`fmt_ms`/`fmt_mb`/`fmt_ratio`) for both the log
and the markdown; `scripts/render-readme` rebuilds the same table from
`bench.json` with fixed-decimal jq helpers, because jq's `tostring` would print
`0.90` as `0.9` and quietly desync the two. Change the columns or the digits in
one and change them in the other.

`tests.yml` runs this harness (tiny/small/medium tiers) on every pull request
and uploads `results-bench/` as an artifact. Both it and `compliance.yml`
install buf through `.github/install-buf.sh`, which pins the version: protoc's
comes from `protoc-go --version` because protoc-go declares what it targets,
but nothing here targets a buf release, so an unpinned buf would move the
published numbers because a third party shipped, with nothing in the diff to
say so. Bumping that pin is a normal change; `bench.json` and `bench.md` record
the version each run measured. It used to also post `bench.md` as
a sticky pull-request comment; that was noise on every push and was removed —
the numbers that get published are the weekly ones.

If `hyperfine` is installed it drives the timing for better statistics;
otherwise a built-in median-of-N loop is used. C++ `protoc` and `buf` are both
optional — when either is absent, its columns and the ratios against it read
`n/a`. For the in-process library core (no
process-startup noise, plus `B/op` and `allocs/op`), use Go's native
benchmarks — same cases, `google_corpus` included:

```bash
go test ./protoc/ -run='^$' -bench=. -benchmem
```

### Comparing against buf

[buf](https://github.com/bufbuild/buf) is a different compiler front end
(`bufbuild/protocompile`) wrapped in a workspace-oriented CLI. It is in the
performance table because it is what a lot of projects reach for, but it is
**not** a correctness target: `scripts/test` compares protoc-go against C++
protoc and says nothing about buf. Nothing about buf's numbers here should be
read as a statement about its output.

What maps onto what:

| harness variant | protoc / protoc-go | buf |
| --- | --- | --- |
| `descriptor` | `--descriptor_set_out=/dev/null -I. <files>` | `buf build . -o /dev/null --as-file-descriptor-set --exclude-source-info` |
| `plugin` | `--plugin=… --dump_out=… --dump_opt=…` | `buf generate . --template <generated buf.gen.yaml>` |

Four differences the harness has to correct for, or it would be timing
different work on each side:

- **buf includes source info by default; protoc does not.** `buf build` writes
  a buf Image (a `FileDescriptorSet` superset) with `SourceCodeInfo` attached —
  on `01_basic_message` that is 1179 bytes against protoc's 418. So the
  `descriptor` variant passes `--as-file-descriptor-set` (drop the Image
  extensions) and `--exclude-source-info` (drop the source info), which is what
  makes all three produce the same artifact.
- **buf takes a directory; protoc takes a file list.** buf compiles every
  `.proto` underneath its input, so the harness checks (`buf_input_matches`)
  that the directory walk yields exactly the file list protoc is handed, and
  marks buf `n/a` for the row if it does not. Every case in the corpus keeps
  its `.proto` files in a single directory, which also means buf's default
  per-directory plugin strategy issues exactly one plugin call, the same as
  protoc.
- **buf has no `--plugin`/`--x_out` flags.** A local plugin is named in a
  generation template, so the harness writes a `buf.gen.yaml` per case into
  `results-bench/`. The result is genuinely like-for-like: on
  `01_basic_message` buf's `CodeGeneratorRequest` is 2412 bytes against
  protoc's 2422, and the whole difference is the `compiler_version` field,
  which buf does not set.
- **buf embeds its own well-known types and ignores `-I` for them.** protoc
  resolves `google/protobuf/*.proto` from its include directory; buf uses the
  copies compiled into the binary. On a corpus needing a newer
  `descriptor.proto` than the buf release carries, buf is compiling against
  different inputs — which surfaces as a compile failure, not as a fast number.

**buf cannot compile `google_corpus`, and that is a finding rather than a
harness bug.** The vendored corpus tracks the protoc release protoc-go targets,
so it uses editions newer than protocompile accepts (`edition = "2026"`,
`edition = "2024"`) and options newer than buf's embedded `descriptor.proto`
knows (`FieldOptions.FeatureSupport`, `enforce_naming_style`). At buf 1.72.0
that is 20 of the 53 files. The row therefore publishes its C++/Go numbers with
buf reading `n/a` and buf's own first error printed under the table — which is
the whole reason the compile gate is per implementation rather than per row.

Corpus shape moves buf's numbers a lot, which is why `bench_multi` exists.
Every scaled tier is a single generated file, which exercises raw parse/link
throughput and nothing else — no import resolution, and nothing for a compiler
that parses files concurrently to use. That judges a front end on its worst
dimension. Measured on a 4-core box:

| corpus | cpp | go | buf | buf÷cpp |
| --- | ---: | ---: | ---: | ---: |
| `bench_medium` — 1 generated file | 135ms | 50ms | 440ms | 3.3 |
| `bench_multi` — 42 generated files, import graph | 96ms | 37ms | 266ms | 2.8 |
| 32 real upstream files, import graph | 92ms | 44ms | 192ms | 2.1 |

Two things follow. Adding files narrows the gap, so a single-file-only table
was reading buf at close to its worst case. And the generated multi tier still
does not narrow it as far as the real corpus does — generated protos are
uniform, and buf does relatively better the more the input looks like something
a person wrote. The row worth trusting most on that axis is `google_corpus`,
and it is the one buf cannot compile, so no table here fully closes the gap.

### Re-checking that buf is measured fairly

The gap is large enough to invite the suspicion that the harness is calling buf
wrong. Four checks say it is not, and they are worth repeating whenever the buf
pin moves:

- **The flags do not penalize it.** `--exclude-source-info` makes buf's compile
  slightly *faster* than its default, not slower, so asking it for protoc's
  artifact is not extra work.
- **Two independent timers agree.** `buf build --debug` reports its own
  `bufimage.BuildImage` duration. On `bench_medium` that is ~400ms against the
  ~389ms left when the measured startup floor is subtracted from wall clock —
  so the harness's number is buf's own number.
- **Startup is isolated, not smeared.** buf's process-launch floor is ~51ms
  against protoc's ~4ms (a 54MB binary against 10MB). That cost dominates the
  small rows, which is exactly why `startup_empty` is flagged informational and
  excluded from the tallies.
- **It is not thrashing on a small box.** buf scales with cores (310ms at
  `GOMAXPROCS=1`, 175ms at 4 on the 32-file corpus), so its default is already
  its best configuration here.

What the CPU column shows is that buf buys its wall-clock with cores: on that
corpus it spends 431ms of CPU to protoc's 91ms and protoc-go's 62ms. It is not
that buf is being measured unfairly; it is that buf is doing considerably more
work and hiding some of it behind parallelism.

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
| performance | ~5min | three compilers over every row; `bench_large`/`plugin` is the longest |

Adding buf roughly doubled the performance leg: it is a third full pass over
every row except `google_corpus`, and buf runs about 1.5–5× slower than C++
protoc depending on the row, so its pass is the most expensive of the three.
Neither leg is worth sharding further, and the performance one is still not the
critical path. It used to run ~20 minutes, ~17 of them in a single row, but that
row was never measuring a compiler: on `bench_large`/`plugin`, protoc-go
compiled and shipped the whole request in 243ms and protoc-gen-dump then spent
38.8s on it, 34.3s of that in a quadratic `s +=` loop building summary.txt.
Sharding would have bought ~3 of those 20 minutes; fixing the fixture bought 18
and un-diluted the ratios at the same time. Should the leg ever need splitting,
note that a row's go and cpp halves must stay on one machine for its ratio to
mean anything, and the tiers are meant to be read against each other as a
size→latency curve, which spanning machines would spoil.

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
