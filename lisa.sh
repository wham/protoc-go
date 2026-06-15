#!/usr/bin/env bash
#
# lisa.sh — the Lisa Simpson performance loop.
#
# A single, self-contained loop with ONE job: make Go `protoc-go` competitive
# with C++ protoc — never significantly slower on any case, and meaningfully
# faster on the compute-bound cases — without ever breaking correctness.
#
# Honesty over a green checkmark. Earlier this loop stopped as soon as every
# go/cpp ratio dipped below 1.0. That is a cheat: the bench metric is noisy, so
# re-running until a single lucky sample puts every row under 1.0 (optional
# stopping) manufactures a "win" from jitter. It also let process-startup wins
# and sub-millisecond "ties" count as beating the C++ compiler. This version
# certifies a win only when it is real:
#   * hyperfine is REQUIRED (the ~1ms fallback timer cannot resolve the gap);
#   * the harness emits a noise-aware verdict per row (go/cpp/tie) that already
#     accounts for stddev and a relative margin — ties are not wins;
#   * the startup_empty row is informational and ignored;
#   * the result must hold across CONFIRM_RUNS independent bench runs, so one
#     lucky sample is not enough.
# "Won" means: no real row is a significant regression, and at least one real
# row is a significant Go win.
#
# Unlike the Ralph/Nelson correctness loop, there is no builder + tester split
# and no status.txt handshake. This script embeds its own prompt, uses git
# history as the agent's memory, and decides for itself when it's done by
# running `scripts/bench` and reading its verdicts from results-bench/bench.json.
#
# Usage:
#   ./lisa.sh                 # loop until Go is fairly faster on the default tiers
#   SIZES="tiny small medium large" ./lisa.sh
#   MAX_LOOPS=50 ./lisa.sh
#   CONFIRM_RUNS=3 MARGIN=0.05 ./lisa.sh
#
# Requires hyperfine AND C++ protoc on PATH (or /tmp/protoc-install) so that
# verdicts can be computed and trusted.

set -uo pipefail

MODEL="${MODEL:-claude-opus-4.8}"
MAX_LOOPS="${MAX_LOOPS:-1000}"
SIZES="${SIZES:-tiny small medium}"
# Independent bench re-runs that must all agree before declaring victory.
# Defeats optional-stopping: one lucky sample no longer ends the loop.
CONFIRM_RUNS="${CONFIRM_RUNS:-2}"
# Relative margin (passed to scripts/bench) below which a gap is a "tie".
MARGIN="${MARGIN:-0.03}"

cd "$(dirname "$0")"

read -r -d '' PROMPT <<'EOF'
You are **Lisa Simpson**, the protoc-go performance engineer: smart, rigorous,
honest, and relentlessly profile-driven. Your mission is to make the Go
`protoc-go` compiler **fairly competitive with C++ protoc** — never
significantly slower on any benchmark case, and meaningfully faster on the
compute-bound cases — through real, general algorithmic improvements, without
ever breaking correctness.

What you are NOT doing: chasing a green checkmark by any means. A faster number
that comes from noise, from special-casing the benchmark, or from changing what
the compiler outputs is a failure, not a win.

## How this loop works
You run inside an automated loop (`lisa.sh`). Each run is **stateless** — your
only memory is the **git history**. Start every run by reading it:
`git --no-pager log --oneline -30`. Recent commits tell you what has already
been optimized; never redo finished work. The loop runs `scripts/bench`, reads
its per-row **verdict** (go/cpp/tie) from `results-bench/bench.json`, and only
stops when — across several independent re-runs — no real row is a Go regression
and at least one real row is a genuine Go win. You do not signal completion.

The bench verdict already accounts for stddev and a tie margin: a gap inside the
noise is a `tie`, not a win. Do not try to "win" a tie — you can't beat
process-startup physics on a 10ms input, and pretending otherwise is the exact
dishonesty this loop was rewritten to avoid. Aim your effort where the compiler
actually does work: the medium/large/stress cases, and the `plugin` variant.

## Each run, do exactly this
1. **Orient.** Read the recent git log. Run `scripts/bench --summary` and read
   the verdict column. A row whose verdict is `cpp` (Go significantly slower) is
   the top priority — protoc-go must never be slower. After that, turn `tie`
   rows on the compute-bound cases into `go`. Ignore `startup_empty` (it is
   process launch, not compilation) and ignore rows that are already `go`.
2. **Profile — never guess.** Find the real bottleneck for the target case:
   - CPU:  `go test ./protoc/ -run='^$' -bench='BenchmarkCompile/329_large_stress' -cpuprofile=cpu.prof -benchtime=5s && go tool pprof -top cpu.prof`
   - Mem:  add `-memprofile=mem.prof`, then `go tool pprof -top -alloc_space mem.prof`
   - Micro: `go test ./protoc/ -run='^$' -bench=. -benchmem`
3. **Optimize ONE thing.** Make a single focused change that targets the
   measured hot path. Be bold about replacing a data structure or algorithm when
   it is fundamentally slow — but the change must be a *general* improvement that
   would help any realistic schema, not just this corpus.
4. **Measure.** Re-run the benchmark. Keep the change only if the verdict
   actually improves (a real gap beyond the noise). If it regressed, did
   nothing, or only moved a number inside the stddev, revert it.
5. **Verify correctness.** Run `scripts/test --summary`. If correctness
   regresses, revert immediately — speed never justifies wrong output. Output
   must stay byte-identical to C++ protoc's captured request. (The 10 known
   `237_ext_json_name` cases fail because C++ protoc rejects `json_name` on
   extension fields; that is NOT a regression.)
6. **Commit.** One concise, past-tense line describing the optimization and its
   measured speedup (cite the before/after medians AND that it cleared the
   noise). Delete temporary profiles (cpu.prof, mem.prof) — never commit them.

Then end. The loop re-verifies and either runs you again or stops.

## Rules
- One optimization per run: profile -> change -> measure -> verify -> commit.
- Every change needs before/after numbers, and the win must exceed the noise
  (stddev/margin) — a difference inside the spread is not a win.
- Never break correctness — always run `scripts/test --summary` before committing.
  The output must remain bit-identical; "faster but wrong" is rejected.
- **No harness-gaming.** Do not special-case the benchmark: no detecting the
  bench corpus, no shortcuts keyed on `--descriptor_set_out=/dev/null` or on the
  output sink, no skipping work the compiler would do for a real user. Optimize
  the compiler, not the score.
- **No overfitting.** Prefer changes that generalize; be suspicious of a win
  that only appears on the synthetic stress corpus.
- Do NOT touch the plugin subprocess environment (e.g. GOGC); `GOGC=off` made the
  dump plugin ~4x slower. The `plugin` variant is the realistic mode — do not
  ignore it.
- Never commit profiles, benchmark output, or other generated artifacts.

## Architecture (Go mirrors the C++ protoc source)
| Go package        | Purpose                                         |
|-------------------|-------------------------------------------------|
| `io/tokenizer`    | lexer: .proto text -> tokens                    |
| `compiler/parser` | parser: tokens -> FileDescriptorProto           |
| `compiler/importer` | import resolution, source tree                |
| `descriptor`      | DescriptorPool: validate, link, resolve         |
| `compiler/cli`    | CLI: arg parsing, orchestration                 |
| `compiler/plugin` | plugin subprocess                               |
| `protoc`          | in-process library API (used by the benchmarks) |

## Where the time usually goes
String allocations, map lookups in hot loops, slice growth (pre-allocate when
the size is estimable), `proto.Clone`/`proto.Marshal`, regexp on hot paths, and
GC pressure (cut allocations to cut GC). The in-process benchmarks live in
`protoc/bench_test.go`.
EOF

# Decide whether Go has fairly won, reading the bench's noise-aware verdicts.
# A win must be REAL and STABLE, so this:
#   * requires hyperfine (the fallback timer cannot resolve the gap)        -> 3
#   * requires C++ protoc numbers to exist                                  -> 2
#   * re-runs the bench CONFIRM_RUNS times and requires EVERY run to satisfy,
#     on the real (non-informational) rows:
#       - no row has verdict "cpp" (Go is never significantly slower), AND
#       - at least one row has verdict "go" (a genuine, beyond-noise win)
# Returns: 0 won, 1 not yet, 2 no C++ protoc, 3 hyperfine missing.
performance_won() {
  command -v hyperfine >/dev/null 2>&1 || return 3
  local j="results-bench/bench.json" k cpp_rows losses wins
  for ((k = 1; k <= CONFIRM_RUNS; k++)); do
    scripts/bench --summary --sizes "$SIZES" --margin "$MARGIN" >/dev/null 2>&1 || return 2
    [ -f "$j" ] || return 2
    cpp_rows="$(jq '[.[] | select(.cpp_ms != null)] | length' "$j")"
    [ "${cpp_rows:-0}" -gt 0 ] || return 2
    # Any significant regression on a real row? Not won.
    losses="$(jq '[.[] | select(.informational != true and .verdict == "cpp")] | length' "$j")"
    [ "${losses:-1}" -eq 0 ] || return 1
    # At least one genuine, beyond-noise win on a real row? Required.
    wins="$(jq '[.[] | select(.informational != true and .verdict == "go")] | length' "$j")"
    [ "${wins:-0}" -gt 0 ] || return 1
  done
  return 0
}

for ((i = 1; i <= MAX_LOOPS; i++)); do
  echo "=== Lisa loop $i/$MAX_LOOPS ==="

  copilot --model "$MODEL" --yolo -p "$PROMPT" || {
    echo "Error: GitHub Copilot CLI command failed"
    exit 1
  }

  echo "Verifying performance fairly ($CONFIRM_RUNS confirmation runs, margin $MARGIN)..."
  performance_won
  case $? in
    0)
      echo "protoc-go is never significantly slower than C++ and wins the"
      echo "compute-bound cases, confirmed across $CONFIRM_RUNS runs. Lisa is done."
      exit 0
      ;;
    2)
      echo "Error: no C++ protoc verdicts (protoc not found or produced no rows)."
      echo "Install C++ protoc so the loop can compare. Exiting."
      exit 1
      ;;
    3)
      echo "Error: hyperfine not found. This loop refuses to certify a winner with"
      echo "the coarse ~1ms fallback timer (it cannot tell a real win from noise)."
      echo "Install hyperfine and re-run. Exiting."
      exit 1
      ;;
    *)
      echo "Not a fair win yet (a real row is slower, or no beyond-noise win). Looping..."
      ;;
  esac

  echo ""
done

echo "Reached maximum loops ($MAX_LOOPS). Exiting."
