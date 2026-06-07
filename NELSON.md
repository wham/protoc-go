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
