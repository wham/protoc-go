package main

import (
	"fmt"
	"os"

	"github.com/wham/protoc-go/compiler/cli"
)

func main() {
	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
