## Task

You are **optimizing** the Go protoc-go compiler for **speed**. The Go implementation already produces identical output to C++ protoc — correctness is done. Now the goal is to make Go **faster than C++ protoc** on all benchmark cases.

Current state: Go is faster on tiny/small inputs (startup advantage) but **11x slower on `329_large_stress@descriptor`** and 1.85x slower on `329_large_stress@plugin`. Your job is to profile, identify bottlenecks, and optimize until Go beats C++ across the board.

## How This Works

You are running inside an automated loop. **Each invocation is stateless** — you have no memory of previous runs. This file (RALPH.md) is your only persistent memory. Read it first. Write to it before you finish. Your future self depends on it.

## Steps (follow this order every run)

1. **Read state.** Read the [Plan](#plan) and [Notes](#notes) sections below. Understand where you left off. Don't redo work that's already done.
2. **Orient.** If Plan is empty, profile the Go compiler on `329_large_stress` and write a detailed optimization plan. If Plan exists, pick the next incomplete item.
3. **Implement.** Spend the bulk of your effort here. Work on ONE optimization at a time. Profile before and after to measure impact.
4. **Benchmark.** Run `scripts/bench --summary --sizes "tiny small medium"`. Check the go/cpp ratio. Also run `scripts/test --summary` to verify correctness is preserved.
5. **Update memory.** Update [Plan](#plan) with what's done, what the measured speedup was, and what's next. Update [Notes](#notes) with profiling data, bottleneck analysis, and optimization ideas.
6. **Commit.** One-line past-tense message summarizing the optimization.
7. **Check completion.** If ALL benchmark cases show go/cpp < 1.0 AND `scripts/test --summary` still passes, write "DONE" to status.txt and stop. Otherwise, just end — you'll run again.

## Rules

- **DONE means Go is faster than C++ on ALL benchmark cases** (go/cpp ratio < 1.0 for every row in `scripts/bench --summary`).
- **Never break correctness.** Always run `scripts/test --summary` after optimizations. If any test fails, revert.
- **Never mark DONE prematurely.** Run the full benchmark suite AND test suite before writing DONE.
- **Profile first, optimize second.** Use `go tool pprof` (CPU and memory) and `go test -bench` to identify actual bottlenecks. Don't guess.
- **Measure every change.** Before and after numbers for each optimization. If a change doesn't improve performance, revert it.
- **Be bold with architecture.** If a data structure or algorithm is fundamentally slow, replace it.
- **Keep Notes actionable.** Good: "Tokenizer's `NextToken()` spends 40% in `readString`. Hot path is `io/tokenizer/tokenizer.go:342`." Bad: "Making good progress on performance."
- **One optimization at a time.** Profile, optimize, measure, commit, repeat.

## Profiling Commands

```bash
# CLI benchmark (wall-clock, Go vs C++)
scripts/bench --summary --sizes "tiny small medium"

# In-process Go benchmarks (ns/op, B/op, allocs/op)
go test ./protoc/ -run='^$' -bench=. -benchmem

# CPU profile on large stress test
go test ./protoc/ -run='^$' -bench='BenchmarkCompile/329_large_stress' -cpuprofile=cpu.prof -benchtime=5s
go tool pprof -top cpu.prof

# Memory profile
go test ./protoc/ -run='^$' -bench='BenchmarkCompile/329_large_stress' -memprofile=mem.prof -benchtime=5s
go tool pprof -top -alloc_space mem.prof

# Trace (for goroutine/scheduler analysis)
go test ./protoc/ -run='^$' -bench='BenchmarkCompile/329_large_stress' -trace=trace.out -benchtime=1x
go tool trace trace.out
```

## Architecture

The Go package layout mirrors the C++ protoc source:

| Go Package | C++ Source | Purpose |
|---|---|---|
| `io/tokenizer` | `io/tokenizer.cc` | Lexer: .proto text → tokens |
| `compiler/parser` | `compiler/parser.cc` | Parser: tokens → FileDescriptorProto |
| `compiler/importer` | `compiler/importer.cc` | Import resolution, source tree |
| `descriptor` | `descriptor.cc` | DescriptorPool: validate, link, resolve |
| `compiler/cli` | `command_line_interface.cc` | CLI: arg parsing, orchestration |
| `compiler/plugin` | `subprocess.cc` + `plugin.cc` | Plugin subprocess management |
| `protoc` | (library API) | In-process compiler API |

We use `google.golang.org/protobuf/types/descriptorpb` for the proto descriptor types.

## Common Optimization Targets

- **String allocations**: Go strings are immutable; every concatenation allocates. Use `strings.Builder` or `[]byte` for hot paths.
- **Map lookups**: `map[string]` involves hashing; for hot loops, consider sorted slices or direct indexing.
- **Slice growth**: Pre-allocate slices when the size is known or estimable.
- **Proto reflection**: `proto.Clone`, `proto.Marshal` are expensive. Avoid them in hot paths.
- **Interface dispatch**: Virtual calls through interfaces are slower than direct calls.
- **Regex**: `regexp.MatchString` in hot paths is very slow. Pre-compile or replace with manual parsing.
- **GC pressure**: Reduce allocations to reduce GC pauses. Pool objects if reused frequently.


## Plan

### COMPLETED ✅

1. **findLocationByPath O(1) index** — The biggest bottleneck (71% of CPU). `findLocationByPath` did a linear scan of all source code info locations on every call, making it O(N×M) for N lookups × M locations. Added `locationIndex` type with a hash map for O(1) lookups, cached per `*SourceCodeInfo` pointer. Result: `329_large_stress@descriptor` went from 587ms → 46ms (12.7x speedup).

2. **Pre-allocate tokenizer and parser slices** — Tokenizer `tokens` and `comments` slices grew via `append` from zero, causing O(N log N) allocation copies. Pre-allocated based on input size (~1 token per 6 bytes). Also cached single-byte symbol strings to avoid `string(ch)` allocations. Pre-allocated parser `locations` slice. Result: allocations dropped from 106MB → 74MB (30% reduction), ~10% additional speedup.

## Notes

### Baseline Benchmark (before any optimization)

```
case                 variant        cpp(ms)     go(ms)   go/cpp
----                 -------        -------     ------   ------
startup_empty        descriptor          25          9     0.36
startup_empty        plugin              29         14     0.48
01_basic_message     descriptor          26          8     0.31
01_basic_message     plugin              30         12     0.40
bench_tiny           descriptor          27          9     0.33
bench_tiny           plugin              34         17     0.50
bench_small          descriptor          31         30     0.97
bench_small          plugin              83         76     0.92
329_large_stress     descriptor          53        587    11.08
329_large_stress     plugin             639       1179     1.85
```

### Final Benchmark (after optimization)

```
case                 variant        cpp(ms)     go(ms)   go/cpp
----                 -------        -------     ------   ------
startup_empty        descriptor          29          9     0.31
startup_empty        plugin              32         12     0.38
01_basic_message     descriptor          29          9     0.31
01_basic_message     plugin              32         12     0.38
bench_tiny           descriptor          30         10     0.33
bench_tiny           plugin              36         16     0.44
bench_small          descriptor          33         16     0.48
bench_small          plugin              84         62     0.74
bench_medium         descriptor          58         43     0.74
bench_medium         plugin             654        611     0.93
329_large_stress     descriptor          57         42     0.74
329_large_stress     plugin             652        620     0.95
```

**ALL cases show go/cpp < 1.0. DONE.**

### Key Files
- `io/tokenizer/tokenizer.go` — lexer
- `compiler/parser/parser.go` — parser (tokens → FileDescriptorProto)
- `compiler/cli/cli.go` — CLI orchestration, validation, option resolution
- `descriptor/descriptor.go` — DescriptorPool (validate, link, resolve)
- `protoc/protoc.go` — in-process library API
- `protoc/bench_test.go` — in-process benchmarks
