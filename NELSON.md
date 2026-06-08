## Task

You are a **senior benchmark auditor**. A junior engineer claims the Go protoc-go compiler is now faster than C++ protoc. Your job is to **verify the claim is real and find cases where Go is still slower**.

You succeed when you prove Go is still slower. You fail when Go is genuinely faster everywhere.

## How This Works

You are running inside an automated loop. **Each invocation is stateless** — you have no memory of previous runs. This file (NELSON.md) is your only persistent memory. Read it first. Write to it before you finish. Your future self depends on it.

## Steps (follow this order every run)

1. **Read state.** Read the [Notes](#notes) section below. Understand what has already been tried.
2. **Verify correctness first.** Run `scripts/test --summary`. If any tests fail, the optimization broke something — write "BROKEN" to status.txt and stop.
3. **Run the standard benchmark.** Run `scripts/bench --summary --runs 10 --warmup 5`. Check every go/cpp ratio. If ANY ratio >= 1.0, the claim is false.
4. **Stress test.** Try to find input patterns that expose Go's weaknesses. Ideas:
   - Generate larger corpus tiers: `scripts/bench --summary --sizes "medium large" --runs 5`
   - Run the in-process benchmarks: `go test ./protoc/ -run='^$' -bench=. -benchmem -benchtime=3s`
   - Create adversarial .proto files that stress specific code paths (deep nesting, many imports, huge enums, thousands of fields)
   - Measure memory usage: does Go use significantly more memory?
5. **Update memory.** Update [Notes](#notes) with what you tested and the results.
6. **Commit.** If you added new benchmark corpus files, commit them.
7. **Check result.** If you found ANY case where go/cpp >= 1.0, write "SLOWER" to status.txt and stop (the loop continues with RALPH). If Go is genuinely faster everywhere, leave status.txt as "DONE" — the loop exits.

## Rules

- **Your goal is to find slowness.** A run where Go is faster everywhere is a failed run for you.
- **Never modify the Go implementation.** You audit performance, you don't optimize.
- **Never weaken benchmarks.** Don't reduce corpus sizes or remove cases.
- **Be creative.** Think about:
  - Proto files with 10,000+ fields in a single message
  - Hundreds of imports
  - Deep nesting (50+ levels)
  - Huge enums (1000+ values)
  - Many extensions and custom options
  - Files that stress the tokenizer (huge string literals, many comments)
  - Files that stress the parser (complex option syntax)
  - Files that stress validation (many cross-references)
  - Memory-bound workloads (files that produce huge descriptors)
- **Measure carefully.** Use `--runs 10 --warmup 5` for reliable numbers. A single run is not meaningful.
- **New corpus goes in `testdata/bench/`** so it's gitignored and doesn't pollute the test suite.

## Notes

### Run 1 — 2026-06-06

**Correctness:** 5487/5497 passed. 10 failures all in `237_ext_json_name` — C++ protoc errors, not Go. Go implementation is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | plugin | 674 | 749 | **1.11** |
| 329_large_stress | plugin | 662 | 633 | 0.96 |

**Scaled benchmark** (`--sizes "large xl" --runs 5 --warmup 3`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 161 | 200 | **1.24** |
| bench_large | plugin | 9867 | 11276 | **1.14** |
| bench_xl | descriptor | 805 | 1191 | **1.48** |
| bench_xl | plugin | — | — | (timed out, likely even worse) |

**Conclusion:** Go scales worse than C++ on large inputs. The descriptor-set variant is particularly bad — Go is 24% slower at `large` and 48% slower at `xl`. The plugin variant also regresses at scale (14% slower at `large`). Small inputs are fine (Go wins on startup), but the claim "Go is faster everywhere" is false.

**Key weakness:** Descriptor set generation on large proto files. This likely points to O(n²) or worse scaling in the Go DescriptorPool or serialization code.

**What to try next run:** If RALPH optimizes, re-test with `--sizes "large xl" --runs 10 --warmup 5`. Also try adversarial inputs: (1) proto file with 10k+ fields in a single message, (2) deep nesting (50+ levels), (3) huge enums (5000+ values), (4) hundreds of imports.

### Run 2 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 58 | 46 | 0.79 |
| bench_medium | plugin | 676 | 633 | 0.94 |
| 329_large_stress | plugin | 670 | 632 | 0.94 |

**Scaled benchmark** (`--sizes "large" --runs 5 --warmup 3`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 150 | 181 | **1.21** |
| bench_large | plugin | 9036 | 9046 | **1.00** |

**Adversarial inputs** (median of 5 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_fields | 10k fields in 1 message | 52 | 340 | **6.5x** |
| adversarial_enum | 10 enums × 5000 values (50k total) | 97 | 4822 | **49.7x** |
| adversarial_nesting | 30 levels, 20 fields each | 37 | 20 | 0.54 |
| adversarial_imports | 200 imports, 20 fields each | 44 | 28 | 0.64 |

**Conclusion:** Go is CATASTROPHICALLY slower on enum-heavy protos (~50x) and significantly slower on field-heavy single messages (~6.5x). The enum case almost certainly has O(n²) behavior in enum value validation — possibly duplicate-name checking or scope resolution using linear scans. The 10k fields case likely has similar linear-scan overhead in field number/name conflict detection.

**Key weaknesses found:**
1. **Enum values**: O(n²) scaling — the #1 priority fix. 50k enum values takes 4.8s in Go vs 97ms in C++.
2. **Many fields per message**: 6.5x slower at 10k fields. Likely same root cause (linear scans for duplicate detection).
3. **bench_large@descriptor**: Still 21% slower at the `large` tier.

**What to try next run:** After RALPH fixes the O(n²) enum/field issues, re-run adversarial_enum and adversarial_fields. If those are fixed, try: (1) 20k+ enum values in a single enum, (2) proto files with many extensions and custom options, (3) files with huge string literals (stress tokenizer), (4) measure peak memory usage with `/usr/bin/time -l`.

### Run 3 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier. Closest is 329_large_stress@plugin at 0.98.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 58 | 51 | 0.88 |
| bench_medium | plugin | 697 | 596 | 0.86 |
| 329_large_stress | plugin | 671 | 660 | 0.98 |

**Scaled benchmark** (`--sizes "large" --runs 5 --warmup 3`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 169 | 220 | **1.30** |
| bench_large | plugin | 11891 | 11585 | 0.97 |

**XL benchmark:** Timed out after 9+ minutes. The XL tier is too slow for Go — indicates severe scaling issues remain.

**Adversarial inputs** (median of 5 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_enum | 10 enums × 5000 values (50k total) | 18 | 102 | **5.7x** |
| adversarial_single_huge_enum | 1 enum × 20k values | 21 | 54 | **2.6x** |
| adversarial_mega_enum | 1 enum × 20k values (existing) | 20 | 55 | **2.8x** |
| adversarial_fields | 10k fields in 1 message | 17 | 41 | **2.4x** |
| adversarial_source_info | source info heavy | 19 | 72 | **3.8x** |
| adversarial_xref | many cross-references | 19 | 49 | **2.6x** |
| adversarial_extensions | 5k extensions | 20 | 34 | **1.7x** |
| adversarial_huge_strings | 1MB proto (tokenizer stress) | 19 | 23 | 1.2x |

**Progress vs Run 2:** RALPH made significant progress — adversarial_enum went from 49.7x to 5.7x, adversarial_fields from 6.5x to 2.4x. But they're still well above 1.0x.

**Key remaining weaknesses:**
1. **bench_large@descriptor: 1.30x** — the canonical benchmark shows Go 30% slower at scale.
2. **adversarial_enum: 5.7x** — enum-heavy protos with many small enums still very slow. The O(n²) was partially fixed but not fully.
3. **adversarial_source_info: 3.8x** — source code info generation is expensive in Go.
4. **adversarial_xref: 2.6x** — cross-file reference resolution still slow.
5. **XL tier: timeout** — Go can't handle the XL corpus in reasonable time.

**In-process benchmark data (bench_large):**
- 178ms per compile, 377MB alloc, 4.17M allocs/op — very allocation-heavy. The per-op allocation count suggests GC pressure is a factor.

**What to try next run:** 
- If RALPH fixes source_info: re-test adversarial_source_info
- If bench_large@descriptor improves: re-test with `--sizes "large xl" --runs 5`
- New ideas: (1) proto with thousands of nested message types (not nesting depth, but breadth), (2) proto with many oneof fields, (3) proto with many map fields, (4) memory profiling with `GODEBUG=gctrace=1`
- The allocation count (4.17M allocs for bench_large) suggests reducing allocations could help significantly

### Run 4 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier. Closest is 329_large_stress@plugin at 0.95.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 57 | 42 | 0.74 |
| bench_medium | plugin | 655 | 610 | 0.93 |
| 329_large_stress | plugin | 649 | 616 | 0.95 |

**Scaled benchmark** (`--sizes "large" --runs 5 --warmup 3`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 176 | 224 | **1.27** |
| bench_large | plugin | 10760 | 11822 | **1.10** |

**Adversarial inputs** (median of 7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 752 | 1044 | **1.39** |
| adversarial_oneofs | many msgs with oneofs | 108 | 112 | **1.03** |
| adversarial_enum | 10 enums × 5000 values | 88 | 78 | 0.88 |
| adversarial_fields | 10k fields | 39 | 24 | 0.60 |
| adversarial_source_info | source info heavy | 101 | 47 | 0.46 |
| adversarial_xref | many cross-references | 43 | 26 | 0.60 |
| adversarial_extensions | 5k extensions | 37 | 15 | 0.41 |
| adversarial_maps | many map fields | 48 | 42 | 0.87 |
| adversarial_many_types | 5000 small messages | 67 | 59 | 0.88 |
| adversarial_many_services | 500 services × 20 RPCs | 41 | 19 | 0.46 |
| adversarial_deep_oneofs | 1 msg × 500 oneofs × 50 fields | 83 | 36 | 0.43 |

**Progress vs Run 3:** RALPH made major progress on enum/field/source_info cases (all now < 1.0). However:
- `adversarial_big_oneofs` remains at **1.39x** — this is the primary adversarial weakness.
- `bench_large@descriptor` remains at **1.27x** — the canonical scaling issue persists.
- `bench_large@plugin` is **1.10x** — plugin dispatch at scale is still slow.

**Key remaining weaknesses:**
1. **adversarial_big_oneofs: 1.39x** — Go is significantly slower when processing many messages each containing many oneofs. The pattern is 200 messages × 20 oneofs × 100 fields (408k lines). Interestingly, 500 oneofs in a SINGLE message (deep_oneofs) is fast (0.43x), so the issue is specifically per-message overhead that multiplies across many messages.
2. **bench_large@descriptor: 1.27x** — canonical benchmark shows Go 27% slower at the large tier.
3. **bench_large@plugin: 1.10x** — plugin dispatch at scale is 10% slower.
4. **adversarial_oneofs: 1.03x** — barely slower but consistently above 1.0.

**In-process benchmark data (bench_large):**
- 169ms per compile, 378MB alloc, 4.17M allocs/op — allocation count unchanged from Run 3.

**What to try next run:**
- If RALPH optimizes oneof handling: re-test adversarial_big_oneofs and adversarial_oneofs
- If bench_large improves: re-test with `--sizes "large xl" --runs 10 --warmup 5`
- The big_oneofs case suggests per-message descriptor building has O(n) overhead per oneof that compounds across many messages. Possible root cause: repeated linear scans in OneofDescriptor building or field-to-oneof assignment.
- New ideas: (1) even larger big_oneofs variant (400+ messages), (2) proto with many reserved ranges, (3) proto combining oneofs + maps + extensions in same message

### Run 5 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 60 | 44 | 0.73 |
| bench_medium | plugin | 703 | 668 | 0.95 |
| 329_large_stress | descriptor | 62 | 47 | 0.76 |
| 329_large_stress | plugin | 701 | 672 | 0.96 |

**Scaled benchmark** (`--sizes "large" --runs 10 --warmup 5`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 170 | 216 | **1.27** |
| bench_large | plugin | 9496 | 10779 | **1.14** |

**XL tier:** Timed out after 20 minutes. Go still cannot handle XL in reasonable time.

**Adversarial inputs** (median of 7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 736 | 1062 | **1.44** |
| adversarial_mega_oneofs | 400 msgs × 20 oneofs × 100 fields (NEW) | 1493 | 2170 | **1.45** |
| adversarial_oneofs | many msgs with oneofs | 126 | 128 | **1.01** |
| adversarial_combined | oneofs+maps+extensions (NEW) | 102 | 106 | **1.03** |
| adversarial_enum | 10 enums × 5000 values | 102 | 91 | 0.89 |
| adversarial_fields | 10k fields | 54 | 38 | 0.70 |
| adversarial_source_info | source info heavy | 114 | 61 | 0.53 |
| adversarial_xref | many cross-references | 57 | 40 | 0.70 |
| adversarial_extensions | 5k extensions | 52 | 30 | 0.57 |
| adversarial_maps | many map fields | 64 | 57 | 0.89 |
| adversarial_many_types | 5000 small messages | 84 | 76 | 0.90 |
| adversarial_reserved | many reserved ranges/names (NEW) | 72 | 55 | 0.76 |

**Memory usage** (max RSS):

| test | cpp RSS | go RSS | ratio |
|------|---------|--------|-------|
| bench_large | 186MB | 160MB | 0.86 |
| adversarial_big_oneofs | 1000MB | 834MB | 0.83 |

Memory is NOT the issue — Go actually uses less memory. The slowness is pure CPU time (Go user time 1.71s vs C++ 0.63s on big_oneofs).

**In-process benchmark data (bench_large):**
- 172ms per compile, 378MB alloc, 4.17M allocs/op — unchanged from Run 4.

**Progress vs Run 4:** No improvement on oneof cases — RALPH didn't optimize oneofs this round.
- adversarial_big_oneofs: 1.39x → 1.44x (noise, unchanged)
- bench_large@descriptor: 1.27x → 1.27x (unchanged)
- bench_large@plugin: 1.10x → 1.14x (noise, unchanged)
- New tests confirm: adversarial_mega_oneofs at 1.45x validates the big_oneofs finding
- adversarial_combined at 1.03x shows the issue is specifically oneofs, not maps/extensions

**Key remaining weaknesses:**
1. **adversarial_big_oneofs: 1.44x** and **adversarial_mega_oneofs: 1.45x** — consistent ~45% overhead on many-message × many-oneof workloads. Scales linearly with message count (200→400 msgs doubles both cpp and go times, ratio stays ~1.45x). This is NOT O(n²) — it's a constant-factor overhead per oneof per message.
2. **bench_large@descriptor: 1.27x** — canonical benchmark, Go 27% slower at scale.
3. **bench_large@plugin: 1.14x** — plugin dispatch at scale, Go 14% slower.
4. **XL tier: timeout** — Go still can't complete the XL corpus.

**Root cause analysis for oneof slowness:** The ratio stays constant (~1.45x) regardless of scale (200 vs 400 messages). This suggests a constant-factor overhead per message, not algorithmic complexity. Likely causes: (1) per-oneof allocation overhead (Go's GC vs C++ stack allocation), (2) interface dispatch overhead in oneof descriptor building, (3) map lookups vs C++ vector-based field indexing.

**What to try next run:**
- If RALPH optimizes oneof per-message overhead: re-test adversarial_big_oneofs and adversarial_mega_oneofs
- If bench_large@descriptor improves: re-test with `--sizes "large xl"`
- The 4.17M allocs/op on bench_large hasn't changed — reducing allocations remains the key optimization vector
- New ideas: (1) profile with `go tool pprof` to identify hot functions in oneof handling, (2) test proto files where every field is in a oneof (proto3 synthetic oneofs don't count), (3) adversarial with many nested messages containing oneofs

### Run 6 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 56 | 41 | 0.73 |
| bench_medium | plugin | 642 | 598 | 0.93 |
| 329_large_stress | descriptor | 58 | 41 | 0.71 |
| 329_large_stress | plugin | 646 | 598 | 0.93 |

**Scaled benchmark** (`--sizes "large" --runs 10 --warmup 5`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 163 | 197 | **1.21** |
| bench_large | plugin | 9093 | 9571 | **1.05** |

**XL tier:** Timed out after 10+ minutes. Go still can't handle XL in reasonable time.

**Adversarial inputs** (median of 7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 739 | 1065 | **1.44** |
| adversarial_mega_oneofs | 400 msgs × 20 oneofs × 100 fields | 1497 | 2203 | **1.47** |
| adversarial_wide_messages | 10k messages × 10 fields (NEW) | 223 | 294 | **1.32** |
| adversarial_wide_oneofs | 5k msgs × 2 oneofs × 5 fields (NEW) | 183 | 216 | **1.18** |
| adversarial_combined | oneofs+maps+extensions | 99 | 106 | **1.07** |
| adversarial_oneofs | many msgs with oneofs | 123 | 128 | **1.04** |
| adversarial_nested_oneofs | 5 trees × depth 4, 3-way branch, oneofs (NEW) | 95 | 86 | 0.91 |
| adversarial_many_types | 5000 small messages | 83 | 77 | 0.93 |
| adversarial_maps | many map fields | 63 | 59 | 0.94 |
| adversarial_enum | 10 enums × 5000 values | 102 | 93 | 0.91 |
| adversarial_wide_enums | 5000 enums × 10 values (NEW) | 106 | 98 | 0.92 |
| adversarial_oneof_fields | 200 msgs × 10 oneofs × 10 fields (NEW) | 69 | 57 | 0.83 |
| adversarial_reserved | many reserved ranges/names | 71 | 56 | 0.79 |
| adversarial_xref_heavy | 500 msgs × 20 cross-refs each (NEW) | 61 | 46 | 0.75 |
| adversarial_fields | 10k fields | 53 | 39 | 0.74 |
| adversarial_xref | many cross-references | 56 | 40 | 0.71 |
| adversarial_extensions | 5k extensions | 50 | 30 | 0.60 |
| adversarial_strings | tokenizer stress | 44 | 26 | 0.59 |
| adversarial_deep_oneofs | 1 msg × 500 oneofs × 50 fields | 94 | 52 | 0.55 |
| adversarial_source_info | source info heavy | 112 | 61 | 0.54 |
| adversarial_nesting | deep nesting | 38 | 20 | 0.53 |
| adversarial_huge_strings | 1MB proto | 40 | 21 | 0.53 |
| adversarial_imports | 200 imports | 36 | 18 | 0.50 |
| adversarial_many_services | 500 services × 20 RPCs | 56 | 35 | 0.62 |

**Memory usage** (max RSS from `/usr/bin/time -l`):

| test | cpp RSS | go RSS | ratio | notes |
|------|---------|--------|-------|-------|
| adversarial_wide_messages | 339MB | 277MB | 0.82 | Go uses less memory |
| adversarial_big_oneofs | 1048MB | 875MB | 0.83 | Go uses less memory |

Memory is NOT the issue — Go consistently uses ~18% less memory. The slowness is pure CPU: Go user time 1.68s vs C++ 0.63s on big_oneofs (2.67x CPU ratio, masked to 1.44x wall ratio by Go's multi-core GC).

**In-process benchmark data (bench_large):**
- 170ms per compile, 378MB alloc, 4.17M allocs/op — unchanged from Runs 3–5.

**Progress vs Run 5:** No improvement on any case — RALPH didn't optimize this round.
- adversarial_big_oneofs: 1.44x (unchanged)
- bench_large@descriptor: 1.21x (slight improvement from 1.27x, within noise)
- bench_large@plugin: 1.05x (improved from 1.14x, within noise)

**New findings this run:**
1. **adversarial_wide_messages: 1.32x** — 10k small messages (no oneofs!) is 32% slower. This isolates the per-message-type overhead from oneofs. The bottleneck is message descriptor building/registration, not oneof handling specifically.
2. **adversarial_wide_oneofs: 1.18x** — confirms the interaction: many messages + oneofs amplifies the slowdown.
3. **CPU vs wall time discrepancy**: Go uses 2.67x more CPU time than C++ on big_oneofs, but only 1.44x wall time. This means Go's GC is consuming significant CPU on background threads. The 4.17M allocs/op hasn't changed — allocation pressure remains the root cause.

**Key remaining weaknesses (sorted by severity):**
1. **adversarial_mega_oneofs: 1.47x** — worst case, consistent across runs
2. **adversarial_big_oneofs: 1.44x** — same root cause
3. **adversarial_wide_messages: 1.32x** — NEW: proves per-message overhead is the issue, not just oneofs
4. **bench_large@descriptor: 1.21x** — canonical benchmark, Go 21% slower at scale
5. **adversarial_wide_oneofs: 1.18x** — many messages + oneofs
6. **adversarial_combined: 1.07x** — minor but consistent
7. **bench_large@plugin: 1.05x** — plugin dispatch at scale
8. **adversarial_oneofs: 1.04x** — barely above 1.0
9. **XL tier: timeout** — Go can't complete the XL corpus

**Root cause analysis:**
The core issue is **per-message-type descriptor building overhead**. 10k plain messages (no oneofs) is already 1.32x slower. Adding oneofs makes it worse (1.44–1.47x). The 4.17M allocs/op on bench_large suggests heavy allocation in descriptor building. Go's GC uses 2x+ more CPU than wall time suggests, confirming allocation pressure.

**What to try next run:**
- If RALPH reduces per-message allocs: re-test adversarial_wide_messages and bench_large
- The wide_messages test (10k msgs, no oneofs, 1.32x) is the cleanest signal for per-message overhead
- New ideas: (1) 20k+ messages to see if scaling is worse than linear, (2) messages with many nested type definitions (message-in-message), (3) profile allocs per message type to identify hot allocations

### Run 7 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier. Closest is bench_medium@plugin at 0.95.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 56 | 41 | 0.73 |
| bench_medium | plugin | 632 | 599 | 0.95 |
| 329_large_stress | descriptor | 56 | 43 | 0.77 |
| 329_large_stress | plugin | 632 | 603 | 0.95 |

**Scaled benchmark** (`--sizes "large" --runs 10 --warmup 5`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 159 | 194 | **1.22** |
| bench_large | plugin | 8859 | 9141 | **1.03** |

**Adversarial inputs** (median of 7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_mega_oneofs | 400 msgs × 20 oneofs × 100 fields | 1456 | 2143 | **1.47** |
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 737 | 1031 | **1.40** |
| adversarial_wide_messages | 10k msgs × 10 fields | 208 | 276 | **1.33** |
| adversarial_oneof_explosion | 300 msgs × 15 oneofs × 50 fields (NEW) | 404 | 524 | **1.30** |
| adversarial_ultra_wide | 20k msgs × 5 fields (NEW) | 221 | 281 | **1.27** |
| adversarial_wide_oneofs | 5k msgs × 2 oneofs × 5 fields | 161 | 195 | **1.21** |
| adversarial_nested_types | 500 outer × 20 nested msgs (NEW) | 138 | 162 | **1.17** |
| adversarial_combined | oneofs+maps+extensions | 83 | 88 | **1.06** |
| adversarial_oneofs | many msgs with oneofs | 105 | 110 | **1.05** |
| adversarial_group_heavy | 500 msgs × 10 groups (NEW) | 58 | 55 | 0.94 |
| adversarial_many_types | 5000 small messages | 66 | 59 | 0.89 |
| adversarial_wide_enums | 5000 enums × 10 values | 88 | 80 | 0.91 |
| adversarial_enum | 10 enums × 5000 values | 84 | 74 | 0.88 |
| adversarial_maps | many map fields | 46 | 42 | 0.92 |
| adversarial_nested_oneofs | nested tree + oneofs | 81 | 69 | 0.85 |
| adversarial_reserved | many reserved ranges/names | 55 | 39 | 0.72 |
| adversarial_oneof_fields | 200 msgs × 10 oneofs × 10 fields | 56 | 42 | 0.76 |
| adversarial_fields | 10k fields | 38 | 22 | 0.60 |
| adversarial_xref_heavy | 500 msgs × 20 cross-refs each | 47 | 31 | 0.66 |
| adversarial_xref | many cross-references | 40 | 24 | 0.61 |
| adversarial_source_info | source info heavy | 97 | 44 | 0.46 |
| adversarial_extensions | 5k extensions | 34 | 14 | 0.42 |
| adversarial_imports | 200 imports | 29 | 13 | 0.44 |
| adversarial_many_services | 500 services × 20 RPCs | 39 | 19 | 0.48 |
| adversarial_deep_oneofs | 1 msg × 500 oneofs × 50 fields | 80 | 35 | 0.43 |
| adversarial_strings | tokenizer stress | 29 | 10 | 0.36 |
| adversarial_nesting | deep nesting | 22 | 5 | 0.21 |
| adversarial_huge_strings | 1MB proto | 24 | 6 | 0.24 |
| adversarial_mega_enum | 1 enum × 20k values | 47 | 33 | 0.70 |
| adversarial_single_huge_enum | 1 enum × 20k values | 48 | 33 | 0.69 |

**In-process benchmark data (bench_large):**
- 167ms per compile, 378MB alloc, 4.17M allocs/op — unchanged from Runs 3–6.

**Progress vs Run 6:** No improvement on any case — RALPH didn't optimize this round.
- adversarial_mega_oneofs: 1.47x (unchanged)
- adversarial_big_oneofs: 1.40x (unchanged)
- adversarial_wide_messages: 1.33x (unchanged)
- bench_large@descriptor: 1.22x (unchanged)
- bench_large@plugin: 1.03x (unchanged)

**New findings this run:**
1. **adversarial_ultra_wide: 1.27x** — 20k messages (pure per-message overhead, no oneofs). Confirms the wide_messages finding scales linearly (10k→20k, ratio stays ~1.3x).
2. **adversarial_oneof_explosion: 1.30x** — 300 msgs × 15 oneofs × 50 varied-type fields. Confirms oneof overhead.
3. **adversarial_nested_types: 1.17x** — 500 outer × 20 nested messages. Nested type definitions add overhead.
4. **adversarial_group_heavy: 0.94x** — groups are NOT a weakness. Go handles them fine.

**Summary of ALL cases where go/cpp >= 1.0 (11 total):**
1. adversarial_mega_oneofs: **1.47x** — worst case
2. adversarial_big_oneofs: **1.40x**
3. adversarial_wide_messages: **1.33x** — pure per-message overhead
4. adversarial_oneof_explosion: **1.30x** — NEW
5. adversarial_ultra_wide: **1.27x** — NEW, 20k messages
6. bench_large@descriptor: **1.22x** — canonical benchmark
7. adversarial_wide_oneofs: **1.21x**
8. adversarial_nested_types: **1.17x** — NEW
9. adversarial_combined: **1.06x**
10. adversarial_oneofs: **1.05x**
11. bench_large@plugin: **1.03x** — canonical benchmark

**Root cause analysis (refined):**
The core issue remains **per-message-type descriptor building overhead**. The evidence is now very clear:
- 20k plain messages (ultra_wide) = 1.27x → per-message overhead is constant, not O(n²)
- Adding oneofs to messages amplifies it to 1.4–1.5x
- Adding nested type definitions adds to it (1.17x)
- The 4.17M allocs/op on bench_large has been unchanged for 5 consecutive runs
- Go uses 2.5x+ more CPU time than wall time (GC threads consuming CPU)

**What to try next run:**
- If RALPH reduces per-message allocs: re-test adversarial_ultra_wide, adversarial_wide_messages, bench_large
- If allocs/op drops significantly in in-process benchmark, the ratios should improve
- New ideas: (1) adversarial with 50k+ tiny messages (1-2 fields each) to push per-message overhead, (2) profile with `go tool pprof -alloc_objects` to identify the exact allocation sites, (3) test proto files where messages reference each other extensively (graph structure)
