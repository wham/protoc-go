// protoc-gen-dump is a fake protoc plugin that captures the CodeGeneratorRequest
// it receives and writes it as deterministic JSON to a file. This is used by the
// test harness to compare what C++ protoc vs Go protoc-go sends to plugins.
//
// Usage: protoc --plugin=protoc-gen-dump=./protoc-gen-dump --dump_out=./output input.proto
//
// The plugin writes the CodeGeneratorRequest as JSON to <output_dir>/request.json.
//
// The core logic is in the dump sub-package, which can also be used as an
// in-process library plugin via [dump.NewPlugin].
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/wham/protoc-go/tools/protoc-gen-dump/dump"
	"google.golang.org/protobuf/proto"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "protoc-gen-dump: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Read CodeGeneratorRequest from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("unmarshaling CodeGeneratorRequest: %w", err)
	}

	// Delegate to the library implementation
	resp, err := dump.Generate(&req)
	if err != nil {
		return err
	}

	// Write response back to protoc
	respBytes, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}
	if _, err := os.Stdout.Write(respBytes); err != nil {
		return fmt.Errorf("writing response: %w", err)
	}

	return nil
}
