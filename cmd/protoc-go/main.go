package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/wham/protoc-go/compiler/cli"
)

func main() {
	// protoc-go is a short-lived, one-shot process: it compiles and exits, so
	// the OS reclaims all memory on exit. The default GC pacer collects
	// repeatedly during a single large compile, which is pure overhead here
	// (C++ protoc does no GC at all). Unless the user has tuned GOGC, relax the
	// pacer and let the soft memory limit below decide when collection is
	// actually worth it. The limit is sized so peak RSS stays at or below C++
	// protoc's on equivalent inputs: it is inert for typical compiles (heap
	// far smaller), and on inputs large enough to reach it the GC ran at
	// near-zero measured cost until the live set itself approached the limit —
	// where C++ protoc uses this much memory too, and protoc-go stays well
	// ahead of it in wall time. Raise GOMEMLIMIT for very large one-shot
	// compiles where peak RSS does not matter.
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(800)
	}
	if _, set := os.LookupEnv("GOMEMLIMIT"); !set {
		debug.SetMemoryLimit(512 << 20)
	}

	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
