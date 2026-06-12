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

### COMPLETED ✅ (Run 13 — 2026-06-11, scale-focused)

NELSON had Go at 1.1–2.2x slower on the **large tier** + adversarial high-message-count
inputs (bench_large 1.13/1.20, 500k msgs 2.24x). Root cause per profiling: 4.17M allocs/op
on bench_large, GC ~40-45% of CPU, plus O(messages) redundant map building. Fixes (each
profiled before/after, correctness re-verified at 5487/5497):

3. **Shallow descriptor-set build** — `buildDescriptorSetFiles` did a deep `proto.Clone` of
   every file just to null `SourceCodeInfo`; result is only marshaled/read. Replaced with a
   shallow field-copy (`shallowFileWithoutSourceInfo`). 4.17M→2.89M allocs/op, 165→135ms.
4. **Location arenas** — `addLocationSpan`/`multiSpan` allocated 3 objects per source location
   (path copy, span slice, struct). Added chunked `locArena`/`i32Arena` in the parser. All
   `multiSpan` callers routed through `p.multiSpan`. 2.89M→2.08M allocs/op, 135→120ms.
5. **Location-index key buffer** — `buildLocationIndex` allocated one string per location for
   map keys. Backed all keys with one pre-sized `[]byte` + `unsafe.String`. 2.08M→1.81M.
6. **Relaxed GC for one-shot CLI** — `cmd/protoc-go/main.go` now sets `GOGC=800` + a 4GB soft
   `GOMEMLIMIT` (unless overridden). GC was ~40% of CPU; C++ does none. Flipped bench_large@plugin
   1.03→0.98. NOTE: do NOT inject GOGC into the *plugin* subprocess — `GOGC=off` made
   protoc-gen-dump 4x SLOWER (alloc-heavy JSON marshal benefits from compact heap).
7. **Scratch path buffers in dup-name collection** — reused scratch slices for field paths.
8. **Skip custom-option resolvers when no such options exist** — the 9 `resolveCustomXxxOptions`
   each built O(message-count) lookup maps before doing anything. Added `hasAnyCustomOpts` early-out.
   500k msgs 1.75→1.04, deep_maps 1.52→0.71, many_maps 1.55→0.72. **Huge win.**
9. **Lazy source-location lookup in `validateDuplicateNames`** — `check` computed line/col for
   EVERY symbol but only used them on a duplicate (rare). Changed `check` to take the path and
   call `findLocationByPath` only on the duplicate branch → clean files never build the location
   index at all. 500k msgs 1.04→0.83, 1m 1.04→0.73, wide_messages 0.65→0.45. **Huge win.**
10. **`io.ReadAll` for plugin stdout** — replaced hand-rolled 4KB-buffer reader.

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

### Final Benchmark (verified 2026-06-11, Run 13)

Default `scripts/bench --summary` (the DONE criterion), stable across 4+ runs — ALL < 1.0:

```
case                 variant        cpp(ms)     go(ms)   go/cpp
----                 -------        -------     ------   ------
startup_empty        descriptor          28         10     0.36
startup_empty        plugin              31         12     0.39
01_basic_message     descriptor          30          8     0.27
01_basic_message     plugin              32         13     0.41
bench_tiny           descriptor          28          9     0.32
bench_tiny           plugin              36         16     0.44
bench_small          descriptor          34         11     0.32
bench_small          plugin              85         58     0.68
bench_medium         descriptor          57         23     0.40
bench_medium         plugin             636        584     0.92
329_large_stress     descriptor          56         25     0.45
329_large_stress     plugin             662        609     0.92
```

Large tier (`--sizes large`): descriptor **0.48** (was 1.13). plugin ~**0.97** measured
INTERLEAVED (go 9230ms vs cpp 9500ms). NOTE: the plugin large/medium rows are dominated by
the *shared* protoc-gen-dump subprocess (~9s identical JSON marshal for both compilers), so
non-interleaved `scripts/bench` runs are noisy and can briefly show 1.0–1.03 from load drift.
Our compile side is ~2x faster; the plugin work itself is not ours to change.

In-process bench_large: 4.17M→1.67M allocs/op, 165→113ms.

Adversarial (median-of-3, descriptor_set_out, vs C++ 33.4) — all now < 1.0:
500k_messages 0.83, 1m_messages 0.73, deep_maps 0.71, many_maps 0.72, wide_messages 0.45,
combined 0.50 (were 2.24/2.20/1.52/1.55/1.31/0.50 at NELSON Run 12).

**Default `scripts/bench --summary` is reliably all < 1.0. DONE.**

Note: 10 test "failures" in `237_ext_json_name` are caused by C++ protoc 33.4 rejecting
`json_name` on extension fields — not a Go regression. All 5487 other tests pass.

### If reopened (plugin large/medium near-tie)
The only remaining thin margins are plugin@large/@medium, dominated by the shared subprocess.
To widen: measure INTERLEAVED (alternate cpp/go) — Go wins. Do NOT chase it by injecting GOGC
into the plugin env (GOGC=off → 4x slower). Our marshal of the ~14MB request is the only
protoc-go-side cost left; beating C++ proto marshal further is hard. Focus any future work on
the descriptor path (already 0.4–0.5) or further alloc cuts (parseField 23%, ToJSONName).

### Key Files
- `io/tokenizer/tokenizer.go` — lexer
- `compiler/parser/parser.go` — parser (tokens → FileDescriptorProto)
- `compiler/cli/cli.go` — CLI orchestration, validation, option resolution
- `descriptor/descriptor.go` — DescriptorPool (validate, link, resolve)
- `protoc/protoc.go` — in-process library API
- `protoc/bench_test.go` — in-process benchmarks
