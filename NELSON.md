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
