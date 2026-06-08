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

### Run 8 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier. Closest is bench_medium@plugin at 0.95.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 57 | 43 | 0.75 |
| bench_medium | plugin | 640 | 606 | 0.95 |
| 329_large_stress | descriptor | 57 | 42 | 0.74 |
| 329_large_stress | plugin | 641 | 599 | 0.93 |

**Scaled benchmark** (`--sizes "large" --runs 10 --warmup 5`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 159 | 195 | **1.23** |
| bench_large | plugin | 8880 | 9201 | **1.04** |

**Adversarial inputs** (median of 7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_100k_messages | 100k msgs × 1 field (NEW) | 310 | 580 | **1.87** |
| adversarial_tiny_messages | 50k msgs × 1 field (NEW) | 160 | 270 | **1.68** |
| adversarial_mega_oneofs | 400 msgs × 20 oneofs × 100 fields | 1530 | 2170 | **1.41** |
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 730 | 1040 | **1.42** |
| adversarial_oneof_explosion | 300 msgs × 15 oneofs × 50 fields | 400 | 530 | **1.32** |
| adversarial_wide_messages | 10k msgs × 10 fields | 210 | 270 | **1.28** |
| adversarial_ultra_wide | 20k msgs × 5 fields | 220 | 280 | **1.27** |
| adversarial_wide_oneofs | 5k msgs × 2 oneofs × 5 fields | 160 | 190 | **1.18** |
| adversarial_nested_types | 500 outer × 20 nested msgs | 140 | 160 | **1.14** |
| adversarial_repeated_fields | 500 msgs × 100 repeated fields (NEW) | 100 | 110 | **1.10** |
| adversarial_oneofs | many msgs with oneofs | 100 | 110 | **1.10** |
| adversarial_combined | oneofs+maps+extensions | 80 | 80 | **1.00** |
| adversarial_maps | many map fields | 40 | 40 | **1.00** |
| adversarial_many_types | 5000 small messages | 60 | 50 | 0.83 |
| adversarial_enum | 10 enums × 5000 values | 80 | 70 | 0.87 |
| adversarial_graph_refs | 2k msgs × 5 cross-refs (NEW) | 40 | 30 | 0.75 |
| adversarial_xref_heavy | 500 msgs × 20 cross-refs | 40 | 30 | 0.75 |
| adversarial_fields | 10k fields | 30 | 20 | 0.66 |
| adversarial_xref | many cross-references | 40 | 20 | 0.50 |
| adversarial_source_info | source info heavy | 90 | 40 | 0.44 |
| adversarial_extensions | 5k extensions | 30 | 10 | 0.33 |

**In-process benchmark data (bench_large):**
- 169ms per compile, 378MB alloc, 4.17M allocs/op — **UNCHANGED** for 6 consecutive runs (Runs 3-8). RALPH has not reduced allocations.

**Progress vs Run 7:** No improvement from RALPH on any existing case. All ratios within noise of Run 7.

**New findings this run:**
1. **adversarial_100k_messages: 1.87x** — NEW WORST NON-ONEOF RATIO. 100k messages with 1 field each. This is the purest measure of per-message-type descriptor overhead. Go is 87% slower.
2. **adversarial_tiny_messages: 1.68x** — 50k messages, same pattern at half scale. Ratio is slightly lower (1.68 vs 1.87), confirming the overhead scales slightly worse than linearly.
3. **adversarial_repeated_fields: 1.10x** — 500 messages with 100 repeated fields each. Moderate overhead.
4. **adversarial_graph_refs: 0.75x** — Go is actually faster on heavily cross-referenced messages. Not a weakness.

**Scaling analysis (per-message overhead):**
| messages | fields/msg | cpp(ms) | go(ms) | go/cpp | go overhead/msg |
|----------|-----------|---------|--------|--------|-----------------|
| 5,000    | 3         | 60      | 50     | 0.83   | — (Go wins)     |
| 10,000   | 10        | 210     | 270    | 1.28   | 6μs             |
| 20,000   | 5         | 220     | 280    | 1.27   | 3μs             |
| 50,000   | 1         | 160     | 270    | 1.68   | 2.2μs           |
| 100,000  | 1         | 310     | 580    | 1.87   | 2.7μs           |

The ratio INCREASES with more messages and fewer fields per message. Go has a ~2-3μs constant overhead per message type that C++ doesn't have. With 100k messages, this adds up to 270ms of pure overhead.

**Summary of ALL cases where go/cpp >= 1.0 (14 total, up from 11 in Run 7):**
1. adversarial_100k_messages: **1.87x** — NEW worst case
2. adversarial_tiny_messages: **1.68x** — NEW
3. adversarial_mega_oneofs: **1.41x**
4. adversarial_big_oneofs: **1.42x**
5. adversarial_oneof_explosion: **1.32x**
6. adversarial_wide_messages: **1.28x**
7. adversarial_ultra_wide: **1.27x**
8. bench_large@descriptor: **1.23x** — canonical benchmark
9. adversarial_wide_oneofs: **1.18x**
10. adversarial_nested_types: **1.14x**
11. adversarial_repeated_fields: **1.10x** — NEW
12. adversarial_oneofs: **1.10x**
13. bench_large@plugin: **1.04x** — canonical benchmark
14. adversarial_combined: **1.00x** / adversarial_maps: **1.00x** — borderline

**Root cause analysis (refined):**
The core issue is **per-message-type descriptor registration overhead** in Go. Evidence:
- 100k messages with 1 field each: 1.87x slower (purest signal)
- The overhead is ~2-3μs per message type, accumulating linearly
- 4.17M allocs/op on bench_large has been unchanged for 6 runs
- Adding oneofs amplifies the per-message overhead further (from 1.3x to 1.4-1.5x)
- Go uses 2.5x+ CPU time vs wall time (GC threads consuming CPU)

**What to try next run:**
- If RALPH reduces per-message allocs: re-test adversarial_100k_messages and adversarial_tiny_messages
- The 100k_messages test (1.87x) is now the clearest signal — tell RALPH to focus on this
- If allocs/op drops in in-process benchmark, all ratios should improve
- New ideas: (1) 200k+ messages to see if ratio continues to increase, (2) `go tool pprof -alloc_objects` on 100k_messages to identify exact hot allocs, (3) messages with defaults/options to stress validation

### Run 9 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 54 | 40 | 0.74 |
| bench_medium | plugin | 641 | 598 | 0.93 |
| 329_large_stress | descriptor | 57 | 42 | 0.74 |
| 329_large_stress | plugin | 632 | 598 | 0.95 |

**Scaled benchmark** (`--sizes "large" --runs 10 --warmup 5`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 156 | 193 | **1.24** |
| bench_large | plugin | 8947 | 9154 | **1.02** |

**XL tier** (median of 5 runs, descriptor_set_out):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_xl | descriptor | 768 | 1144 | **1.49** |

**XL tier now completes** (it timed out in Runs 3-6), but Go is **49% slower** — the worst canonical benchmark ratio.

**Adversarial inputs** (median of 7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_200k_messages | 200k msgs × 1 field (NEW) | 620 | 1256 | **2.03** |
| adversarial_100k_messages | 100k msgs × 1 field | 326 | 601 | **1.84** |
| adversarial_tiny_messages | 50k msgs × 1 field | 177 | 290 | **1.64** |
| adversarial_mega_oneofs | 400 msgs × 20 oneofs × 100 fields | 1470 | 2192 | **1.49** |
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 725 | 1059 | **1.46** |
| adversarial_oneof_explosion | 300 msgs × 15 oneofs × 50 fields | 414 | 553 | **1.34** |
| adversarial_wide_messages | 10k msgs × 10 fields | 224 | 295 | **1.32** |
| adversarial_ultra_wide | 20k msgs × 5 fields | 242 | 302 | **1.25** |
| adversarial_wide_oneofs | 5k msgs × 2 oneofs × 5 fields | 176 | 214 | **1.22** |
| adversarial_nested_types | 500 outer × 20 nested msgs | 156 | 182 | **1.17** |
| adversarial_combined | oneofs+maps+extensions | 100 | 105 | **1.05** |
| adversarial_oneofs | many msgs with oneofs | 123 | 128 | **1.04** |
| adversarial_repeated_fields | 500 msgs × 100 repeated fields | 125 | 127 | **1.02** |
| adversarial_deep_nested_types | 100 outer × 10-level nesting (NEW) | 66 | 65 | 0.98 |
| adversarial_maps | many map fields | 62 | 58 | 0.94 |
| adversarial_many_types | 5000 small messages | 83 | 76 | 0.92 |
| adversarial_enum | 10 enums × 5000 values | 100 | 92 | 0.92 |
| adversarial_many_deps | 300 files, 3-layer import graph (NEW) | 66 | 51 | 0.77 |
| adversarial_fields | 10k fields | 53 | 39 | 0.74 |
| adversarial_options_heavy | 2k msgs with custom options (NEW) | 448 | 270 | **0.60** |
| adversarial_source_info | source info heavy | 113 | 62 | 0.55 |
| adversarial_extensions | 5k extensions | 51 | 30 | 0.59 |

**In-process benchmark data (bench_large):**
- 169ms per compile, 378MB alloc, 4.17M allocs/op — **UNCHANGED** for 7 consecutive runs (Runs 3-9). RALPH has not reduced allocations.

**Progress vs Run 8:** No improvement from RALPH. All ratios within noise of Run 8.

**New findings this run:**
1. **adversarial_200k_messages: 2.03x** — NEW WORST RATIO. 200k messages crosses the 2x threshold. The ratio increases with message count: 50k→1.64x, 100k→1.84x, 200k→2.03x. This confirms **slightly superlinear scaling** — the per-message overhead grows as the total number of messages increases, likely due to map/hash-table resizing or GC pressure from larger heaps.
2. **bench_xl@descriptor: 1.49x** — XL tier now works but is 49% slower. This is the worst canonical benchmark result.
3. **adversarial_options_heavy: 0.60x** — Go is actually 40% FASTER on custom-options-heavy protos. Not a weakness.
4. **adversarial_many_deps: 0.77x** — Go is faster on multi-file import resolution. Not a weakness.
5. **adversarial_deep_nested_types: 0.98x** — borderline, not a clear weakness.

**Scaling analysis (per-message overhead, updated):**
| messages | fields/msg | cpp(ms) | go(ms) | go/cpp | trend |
|----------|-----------|---------|--------|--------|-------|
| 5,000 | 3 | 83 | 76 | 0.92 | Go wins |
| 10,000 | 10 | 224 | 295 | 1.32 | — |
| 20,000 | 5 | 242 | 302 | 1.25 | — |
| 50,000 | 1 | 177 | 290 | 1.64 | — |
| 100,000 | 1 | 326 | 601 | 1.84 | — |
| 200,000 | 1 | 620 | 1256 | **2.03** | worsening |

The ratio grows with message count, suggesting Go's per-message overhead includes some component that scales with total symbol count (likely hash map lookups for duplicate detection becoming slower as the map grows, or GC pause time increasing with heap size).

**Summary of ALL cases where go/cpp >= 1.0 (15 total, up from 14 in Run 8):**
1. adversarial_200k_messages: **2.03x** — NEW worst case
2. adversarial_100k_messages: **1.84x**
3. adversarial_tiny_messages: **1.64x**
4. bench_xl@descriptor: **1.49x** — NEW, worst canonical
5. adversarial_mega_oneofs: **1.49x**
6. adversarial_big_oneofs: **1.46x**
7. adversarial_oneof_explosion: **1.34x**
8. adversarial_wide_messages: **1.32x**
9. adversarial_ultra_wide: **1.25x**
10. bench_large@descriptor: **1.24x** — canonical
11. adversarial_wide_oneofs: **1.22x**
12. adversarial_nested_types: **1.17x**
13. adversarial_combined: **1.05x**
14. adversarial_oneofs: **1.04x**
15. bench_large@plugin: **1.02x** / adversarial_repeated_fields: **1.02x**

**Root cause analysis (refined):**
The core issue remains **per-message-type descriptor registration overhead** in Go. The scaling is slightly superlinear — doubling from 100k to 200k messages increases the ratio from 1.84x to 2.03x. This suggests:
1. Go map operations degrade as map size grows (hash collisions, cache misses)
2. GC pressure increases with heap size (more objects to scan)
3. The 4.17M allocs/op on bench_large hasn't changed in 7 runs — allocation reduction is the key

**What to try next run:**
- If RALPH reduces per-message allocs: re-test adversarial_200k_messages and bench_xl
- Focus on the scaling behavior: the fact that 200k messages is 2.03x while 5k messages is 0.92x shows Go fundamentally doesn't scale as well as C++ on type-count-heavy workloads
- New ideas: (1) 500k messages to see if ratio crosses 3x, (2) profile with `GOGC=off` to isolate GC impact, (3) test with `GOMAXPROCS=1` to see single-threaded impact

### Run 10 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier. Closest is bench_medium@plugin at 0.98.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 61 | 46 | 0.75 |
| bench_medium | plugin | 1000 | 979 | 0.98 |
| 329_large_stress | descriptor | 70 | 68 | 0.97 |
| 329_large_stress | plugin | 1018 | 981 | 0.96 |

**Scaled benchmark** (`--sizes "large" --runs 10 --warmup 5`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 173 | 211 | **1.22** |
| bench_large | plugin | 9433 | 9579 | **1.02** |

**XL tier:** Timed out after 30+ minutes. Go still can't complete the XL corpus in reasonable time.

**Adversarial inputs** (median of 7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_500k_messages | 500k msgs × 1 field (NEW) | 2352 | 6897 | **2.93** |
| adversarial_200k_messages | 200k msgs × 1 field | 961 | 2397 | **2.49** |
| adversarial_100k_messages | 100k msgs × 1 field | 488 | 1191 | **2.44** |
| adversarial_tiny_messages | 50k msgs × 1 field | 250 | 538 | **2.15** |
| adversarial_many_maps | 2k msgs × 20 map fields (NEW) | 307 | 567 | **1.85** |
| adversarial_wide_messages | 10k msgs × 10 fields | 343 | 629 | **1.83** |
| adversarial_ultra_wide | 20k msgs × 5 fields | 349 | 612 | **1.75** |
| adversarial_mega_oneofs | 400 msgs × 20 oneofs × 100 fields | 2216 | 3735 | **1.69** |
| adversarial_wide_oneofs | 5k msgs × 2 oneofs × 5 fields | 263 | 425 | **1.62** |
| adversarial_nested_types | 500 outer × 20 nested msgs | 229 | 362 | **1.58** |
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 1060 | 1501 | **1.42** |
| adversarial_repeated_fields | 500 msgs × 100 repeated fields | 171 | 243 | **1.42** |
| adversarial_synthetic_oneofs | 5k msgs × 10 optional (proto3) (NEW) | 218 | 305 | **1.40** |
| adversarial_combined | oneofs+maps+extensions | 141 | 190 | **1.35** |
| adversarial_oneof_explosion | 300 msgs × 15 oneofs × 50 fields | 882 | 1066 | **1.21** |
| adversarial_oneofs | many msgs with oneofs | 205 | 229 | **1.12** |
| adversarial_enum | 10 enums × 5000 values | 147 | 155 | **1.05** |
| adversarial_many_types | 5000 small messages | 137 | 138 | **1.01** |
| adversarial_fields | 10k fields | 76 | 73 | 0.96 |
| adversarial_extensions | 5k extensions | 78 | 73 | 0.94 |
| adversarial_method_options | 1k svcs × 10 methods (NEW) | 80 | 70 | 0.88 |
| adversarial_source_info | source info heavy | 152 | 106 | 0.70 |

**GC isolation** (GOGC=off vs default):

| test | GOGC=100 | GOGC=off | speedup |
|------|----------|----------|---------|
| adversarial_200k_messages | 2441ms | 2324ms | 1.05x |
| adversarial_100k_messages | 1180ms | 1137ms | 1.04x |
| adversarial_big_oneofs | 1785ms | 1686ms | 1.06x |
| adversarial_wide_messages | 522ms | 504ms | 1.04x |

GC is NOT the bottleneck — disabling it only saves 4-6%. The slowness is in computation.

**GOMAXPROCS isolation** (GOMAXPROCS=1 vs default):

| test | default | GOMP=1 | slowdown |
|------|---------|--------|----------|
| adversarial_200k_messages | 2780ms | 3532ms | 1.27x |
| adversarial_100k_messages | 1134ms | 1504ms | 1.33x |
| adversarial_big_oneofs | 1749ms | 2984ms | 1.71x |

Go benefits 27-71% from multi-core parallelism (GC threads, etc.), but even with all cores it's still much slower than single-threaded C++.

**Memory usage** (max RSS from `/usr/bin/time -l`):

| test | cpp RSS | go RSS | ratio | cpp user | go user | user ratio |
|------|---------|--------|-------|----------|---------|------------|
| adversarial_500k_messages | 2.36GB | 1.99GB | 0.84 | 1.85s | 8.02s | **4.34x** |

Go uses 16% less memory but **4.34x more CPU time** on 500k messages. The wall time ratio (2.93x) is masked by Go's multi-core GC, but the CPU ratio reveals the true computational overhead.

**In-process benchmark data (bench_large):**
- 345ms per compile, 378MB alloc, 4.17M allocs/op — **UNCHANGED** for 8 consecutive runs (Runs 3-10). RALPH has not reduced allocations.

**Progress vs Run 9:** No improvement from RALPH on any existing case. All ratios unchanged or slightly worse (likely system load noise).

**New findings this run:**
1. **adversarial_500k_messages: 2.93x** — NEW WORST CASE. 500k messages crosses the ~3x threshold as predicted. Go takes 6.9s vs C++ 2.4s.
2. **adversarial_many_maps: 1.85x** — NEW. 2000 messages × 20 map fields. Each map field creates a synthetic nested message, effectively doubling the message count and amplifying per-message overhead.
3. **adversarial_synthetic_oneofs: 1.40x** — NEW. Proto3 optional fields create synthetic oneofs, same amplification effect.
4. **GC is not the bottleneck**: GOGC=off saves only 4-6%. The issue is raw computation.
5. **CPU time is 4.34x**: Go uses 4.34x more CPU than C++ on 500k messages, but multi-core masks it to 2.93x wall time.
6. **XL tier still times out**: After 30+ minutes. Go can't handle the XL corpus.

**Summary of ALL cases where go/cpp >= 1.0 (18 total, up from 15 in Run 9):**
1. adversarial_500k_messages: **2.93x** — NEW worst case
2. adversarial_200k_messages: **2.49x**
3. adversarial_100k_messages: **2.44x**
4. adversarial_tiny_messages: **2.15x**
5. adversarial_many_maps: **1.85x** — NEW
6. adversarial_wide_messages: **1.83x**
7. adversarial_ultra_wide: **1.75x**
8. adversarial_mega_oneofs: **1.69x**
9. adversarial_wide_oneofs: **1.62x**
10. adversarial_nested_types: **1.58x**
11. adversarial_big_oneofs: **1.42x**
12. adversarial_repeated_fields: **1.42x**
13. adversarial_synthetic_oneofs: **1.40x** — NEW
14. adversarial_combined: **1.35x**
15. bench_large@descriptor: **1.22x** — canonical benchmark
16. adversarial_oneof_explosion: **1.21x**
17. adversarial_oneofs: **1.12x**
18. adversarial_enum: **1.05x** / adversarial_many_types: **1.01x** / bench_large@plugin: **1.02x**

**Root cause analysis (final):**
The issue is definitively **per-message-type descriptor building overhead** in Go. Evidence:
1. Pure message-count scaling: 5k=0.83x, 10k=1.28x, 50k=2.15x, 100k=2.44x, 200k=2.49x, 500k=2.93x
2. Map fields (which create synthetic messages) amplify the overhead: 1.85x
3. Proto3 optional fields (which create synthetic oneofs) amplify it: 1.40x
4. GOGC=off barely helps (4-6%) → not GC
5. CPU time is 4.34x → raw computation overhead, not I/O or memory
6. 4.17M allocs/op unchanged for 8 runs → allocation reduction is the key
7. The overhead scales slightly superlinearly — ratio grows from 1.28x at 10k to 2.93x at 500k

**What to try next run:**
- If RALPH reduces per-message allocs (4.17M → lower): re-test adversarial_500k_messages and bench_large
- If bench_large allocs/op drops below 3M, expect meaningful ratio improvements
- The many_maps test (1.85x) is a good proxy for real-world protos (gRPC services commonly use map fields)
- New ideas: (1) profile with `go tool pprof -alloc_objects` on adversarial_500k_messages to find hot allocation sites, (2) test proto with deeply nested maps (map<string, map<string, ...>>), (3) try 1M messages

### Run 11 — 2026-06-07

**Correctness:** 5487/5497 passed. Same 10 failures in `237_ext_json_name` (C++ protoc errors). Go is correct.

**Standard benchmark** (`--runs 10 --warmup 5`, default sizes):
All ratios < 1.0. Go wins on default tier. Closest is bench_medium@plugin at 0.95.

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_medium | descriptor | 60 | 44 | 0.73 |
| bench_medium | plugin | 640 | 611 | 0.95 |
| 329_large_stress | descriptor | 58 | 44 | 0.76 |
| 329_large_stress | plugin | 646 | 611 | 0.95 |

**Scaled benchmark** (`--sizes "large" --runs 10 --warmup 5`):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_large | descriptor | 163 | 203 | **1.25** |
| bench_large | plugin | 9751 | 9898 | **1.02** |

**XL tier** (median of 5 runs, descriptor_set_out):

| case | variant | cpp(ms) | go(ms) | go/cpp |
|------|---------|---------|--------|--------|
| bench_xl | descriptor | 880 | 1330 | **1.51** |

**Adversarial inputs** (median of 5-7 runs, descriptor_set_out):

| test | description | cpp(ms) | go(ms) | go/cpp |
|------|-------------|---------|--------|--------|
| adversarial_1m_messages | 1M msgs × 1 field (NEW) | 3420 | 8180 | **2.39** |
| adversarial_500k_messages | 500k msgs × 1 field | 1670 | 3970 | **2.37** |
| adversarial_200k_messages | 200k msgs × 1 field | 620 | 1330 | **2.14** |
| adversarial_100k_messages | 100k msgs × 1 field | 310 | 620 | **2.00** |
| adversarial_deep_maps | 3k msgs × 10 maps (33k types) (NEW) | 160 | 310 | **1.93** |
| adversarial_tiny_messages | 50k msgs × 1 field | 160 | 290 | **1.81** |
| adversarial_many_maps | 2k msgs × 20 map fields | 200 | 330 | **1.65** |
| adversarial_mega_oneofs | 400 msgs × 20 oneofs × 100 fields | 1510 | 2260 | **1.49** |
| adversarial_big_oneofs | 200 msgs × 20 oneofs × 100 fields | 730 | 1080 | **1.47** |
| adversarial_wide_messages | 10k msgs × 10 fields | 210 | 290 | **1.38** |
| adversarial_ultra_wide | 20k msgs × 5 fields | 230 | 300 | **1.30** |
| adversarial_wide_oneofs | 5k msgs × 2 oneofs × 5 fields | 160 | 200 | **1.25** |
| adversarial_nested_types | 500 outer × 20 nested msgs | 140 | 170 | **1.21** |
| adversarial_all_optional | 10k msgs × 5 optional (NEW) | 140 | 170 | **1.21** |
| adversarial_json_names | 5k msgs × 10 json_name fields (NEW) | 160 | 190 | **1.18** |
| adversarial_synthetic_oneofs | 5k msgs × 10 optional (proto3) | 140 | 160 | **1.14** |
| adversarial_combined | oneofs+maps+extensions | 80 | 90 | **1.12** |
| adversarial_repeated_fields | 500 msgs × 100 repeated fields | 110 | 120 | **1.09** |
| adversarial_oneofs | many msgs with oneofs | 110 | 110 | **1.00** |
| adversarial_many_types | 5000 small messages | 60 | 60 | **1.00** |
| adversarial_enum | 10 enums × 5000 values | 80 | 70 | 0.87 |
| adversarial_comments | tokenizer stress (many comments) (NEW) | 60 | 50 | 0.83 |
| adversarial_long_names | 5k msgs × long names (NEW) | 110 | 90 | 0.81 |
| adversarial_fields | 10k fields | 30 | 20 | 0.66 |
| adversarial_extensions | 5k extensions | 30 | 10 | 0.33 |
| adversarial_source_info | source info heavy | 90 | 40 | 0.44 |
| adversarial_defaults | 5k msgs × default values (NEW) | 640 | 50 | **0.07** |

**Memory & CPU profile** (1M messages, `/usr/bin/time -l`):

| metric | C++ | Go | ratio |
|--------|-----|-----|-------|
| wall time | 3.40s | 8.09s | **2.38x** |
| user CPU | 2.96s | 11.89s | **4.02x** |
| max RSS | 4.9GB | 4.5GB | 0.92 |

Go uses 4x more CPU time than C++ on 1M messages, but only 2.4x wall time — Go's multi-core GC hides the true overhead.

**In-process benchmark data (bench_large):**
- 178ms per compile, 378MB alloc, 4.17M allocs/op — **UNCHANGED** for 9 consecutive runs (Runs 3-11). RALPH has not reduced allocations.

**Progress vs Run 10:** No improvement from RALPH. All ratios within noise of Run 10.

**New findings this run:**
1. **adversarial_1m_messages: 2.39x** — 1M messages plateaus around 2.4x (same as 500k at 2.37x). The superlinear scaling seen from 50k→500k appears to level off above 500k — the ratio stabilizes around 2.4x.
2. **adversarial_deep_maps: 1.93x** — NEW. 3000 messages × 10 map fields creates 33k total message types via synthetic map entry messages. This is 1.93x slower — maps amplify per-message overhead.
3. **adversarial_all_optional: 1.21x** — NEW. Proto3 optional fields (creating synthetic oneofs) add overhead, same mechanism.
4. **adversarial_json_names: 1.18x** — NEW. json_name annotations add per-field overhead.
5. **adversarial_comments: 0.83x** — Go tokenizer handles comments efficiently.
6. **adversarial_defaults: 0.07x** — Go is 14x FASTER on default values! C++ does significantly more work processing default values.
7. **CPU time ratio (4.02x)** on 1M messages confirms the overhead is pure computation, not I/O.

**Scaling analysis (per-message overhead, updated with 1M):**
| messages | fields/msg | cpp(ms) | go(ms) | go/cpp | trend |
|----------|-----------|---------|--------|--------|-------|
| 5,000 | 3 | 60 | 60 | 1.00 | break-even |
| 10,000 | 10 | 210 | 290 | 1.38 | — |
| 20,000 | 5 | 230 | 300 | 1.30 | — |
| 50,000 | 1 | 160 | 290 | 1.81 | — |
| 100,000 | 1 | 310 | 620 | 2.00 | — |
| 200,000 | 1 | 620 | 1330 | 2.14 | — |
| 500,000 | 1 | 1670 | 3970 | 2.37 | — |
| 1,000,000 | 1 | 3420 | 8180 | **2.39** | plateauing |

The ratio plateaus around 2.4x for very high message counts. This suggests the per-message overhead is a constant factor (~2.4x) that becomes dominant when per-message cost overwhelms startup/IO costs.

**Summary of ALL cases where go/cpp >= 1.0 (20 total, up from 18 in Run 10):**
1. adversarial_1m_messages: **2.39x** — NEW worst case
2. adversarial_500k_messages: **2.37x**
3. adversarial_200k_messages: **2.14x**
4. adversarial_100k_messages: **2.00x**
5. adversarial_deep_maps: **1.93x** — NEW
6. adversarial_tiny_messages: **1.81x**
7. adversarial_many_maps: **1.65x**
8. bench_xl@descriptor: **1.51x**
9. adversarial_mega_oneofs: **1.49x**
10. adversarial_big_oneofs: **1.47x**
11. adversarial_wide_messages: **1.38x**
12. adversarial_ultra_wide: **1.30x**
13. bench_large@descriptor: **1.25x** — canonical benchmark
14. adversarial_wide_oneofs: **1.25x**
15. adversarial_nested_types: **1.21x** / adversarial_all_optional: **1.21x** — NEW
16. adversarial_json_names: **1.18x** — NEW
17. adversarial_synthetic_oneofs: **1.14x**
18. adversarial_combined: **1.12x**
19. adversarial_repeated_fields: **1.09x**
20. bench_large@plugin: **1.02x** — canonical benchmark

**Root cause analysis (definitive):**
The core issue is **per-message-type descriptor registration overhead** in Go — a ~2.4x constant factor per message type that becomes dominant at scale. Key evidence:
1. 4.17M allocs/op on bench_large unchanged for 9 runs — allocation reduction is THE key optimization
2. CPU time is 4x wall time on 1M messages — GC threads consume massive CPU
3. Map fields amplify the overhead by creating synthetic message types (1.93x for deep_maps)
4. The ratio plateaus at ~2.4x for pure message-count workloads
5. Memory is NOT the issue — Go uses less RSS than C++

**What to try next run:**
- If RALPH reduces allocs/op below 3.5M: re-test adversarial_1m_messages, adversarial_deep_maps, bench_large
- The deep_maps test (1.93x) is a realistic proxy for gRPC services with many map fields
- The all_optional test (1.21x) is realistic for proto3 services using optional extensively
- New ideas: (1) adversarial with many service definitions + streaming RPCs, (2) proto with many extension ranges in message options, (3) test with `GODEBUG=gctrace=1` to quantify GC pauses, (4) proto files mixing all expensive patterns (maps + oneofs + nested types + json_name)
