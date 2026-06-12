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
	// pacer so we trade a little peak memory for less GC work. A soft memory
	// limit keeps very large inputs from running away.
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(800)
	}
	if _, set := os.LookupEnv("GOMEMLIMIT"); !set {
		debug.SetMemoryLimit(4 << 30)
	}

	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
