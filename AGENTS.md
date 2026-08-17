## Guidelines

- See below for how to run and test.
- Only add code comments for really tricky parts; otherwise keep it clean.
- Don't commit changes to `status.txt` — it's managed by ralph.sh.
- Use past tense in commit messages (e.g., "Fix bug" → "Fixed bug").
- Keep pull-request descriptions minimal — a single sentence, no bullet lists, no markdown headers.
- Put exactly one `release:` label on every PR that changes shipped code (`.go`, `go.mod`, `go.sum`, `.goreleaser.yml`). `release: none` is the usual answer; a sizing label cuts a release on merge. See [RELEASING.md](RELEASING.md).
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
├── tools/
│   ├── protoc-gen-dump/    # Fake plugin that captures CodeGeneratorRequest
│   └── protoc-bin/         # (reserved) Vendored C++ protoc if needed
├── scripts/
│   ├── test                # Correctness harness — compares C++ protoc vs Go protoc-go
│   ├── bench               # Performance harness — times C++ protoc vs Go protoc-go
│   ├── render-readme       # Renders harness output into the README compliance block
│   ├── release-notes       # Renders the compliance header for a GitHub release
│   ├── next-version        # Semver arithmetic over existing tags
│   ├── gen-large-stress    # Generates scaled stress/bench corpus (tiers)
│   └── find-protoc         # Locates system C++ protoc
├── RELEASING.md            # How releases are cut (label-gated) — read before tagging
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

# Performance comparison (C++ protoc vs Go protoc-go) on a scaled corpus
scripts/bench --summary
# In-process library core (ns/op, B/op, allocs/op)
go test ./protoc/ -run='^$' -bench=. -benchmem
```

The test harness:
1. Builds `cmd/protoc-go/` and `tools/protoc-gen-dump/`
2. For each `testdata/*/` directory × 5 profiles, runs both C++ protoc and Go protoc-go
3. Profiles: `plugin`, `plugin_param`, `descriptor_set`, `descriptor_set_src`, `descriptor_set_full`
4. Also runs CLI error tests (no args, missing files, bad flags)
5. Test names: `<case>@<profile>` (e.g., `01_basic_message@plugin`, `cli@no_args`)
6. Reports pass/fail with diffs

A registered test that writes no verdict (usually a missing tool — `xxd` drives
the `stdin@`/`decode@` suites) is reported as a `no_result` warning and is NOT
counted as a pass. `--json` marks such a run `"error"` so it cannot be
published: a suite that silently shrinks must never read as all-match.

### Performance harness

`scripts/bench` is the performance counterpart to `scripts/test`. It runs both
compilers on a curated, size-scaled corpus — serial, warmed up, median-of-N —
and reports per-case latency plus the Go/C++ ratio, separating compile
throughput from process-startup cost.

```bash
scripts/bench                 # C++ protoc vs Go protoc-go, human table
scripts/bench --summary       # table only; writes results-bench/bench.{json,md}
```

If `hyperfine` is installed it drives the timing for better statistics;
otherwise a built-in median-of-N loop is used. C++ `protoc` is optional — when
absent, only Go numbers are reported. For the in-process library core (no
process-startup noise, plus `B/op` and `allocs/op`), use Go's native
benchmarks:

```bash
go test ./protoc/ -run='^$' -bench=. -benchmem
```

### Published compliance results

`.github/workflows/compliance.yml` runs both harnesses weekly and publishes the
result to the README, so the compliance claim on the home page is evidence
rather than assertion. The chain is deliberately machine-readable end to end —
nothing parses human output:

```
scripts/test --json  → results/summary.json  ┐
scripts/bench        → results-bench/bench.json ┼→ scripts/render-readme → README block
                                                │                        → docs/badge.json
                                                └                        → docs/history.jsonl
```

`scripts/render-readme` rewrites only the region between the
`<!-- BEGIN COMPLIANCE -->` / `<!-- END COMPLIANCE -->` markers; the rest of the
README is hand-written and must stay that way. It refuses to render a summary
whose status is `error`, renders `fail` honestly in red, and drops performance
verdicts when the bench run was not timed by hyperfine.

The rendered results are published as a pull request from the disposable
`compliance/results` branch, not pushed straight to main: main's ruleset
requires the `test` status check, which a direct push has no way to satisfy.
That branch is force-pushed every run, so an already-open pull request is
updated in place rather than piling up. The default `GITHUB_TOKEN` cannot
trigger workflows, so the pull request it opens sits without CI and needs a
manual merge; setting a `COMPLIANCE_TOKEN` secret (fine-grained PAT or GitHub
App token, contents and pull-requests write) makes tests.yml run on it and the
whole loop self-service.

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
