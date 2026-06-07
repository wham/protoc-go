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

(First run — no history yet.)
