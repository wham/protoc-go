// protoc-gen-dump is a fake protoc plugin that captures the CodeGeneratorRequest
// it receives and writes it as deterministic JSON to a file. This is used by the
// test harness to compare what C++ protoc vs Go protoc sends to plugins.
//
// Usage: protoc --plugin=protoc-gen-dump=./protoc-gen-dump --dump_out=./output input.proto
//
// The plugin writes the CodeGeneratorRequest as JSON to <output_dir>/request.json.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
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

	// Write to the output directory specified by the parameter
	outputDir := req.GetParameter()
	if outputDir == "" {
		outputDir = "."
	}

	// Marshal to deterministic JSON for diffing (includes original parameter for debugging)
	marshaler := protojson.MarshalOptions{
		Multiline: true,
		Indent:    "  ",
	}
	jsonBytes, err := marshaler.Marshal(&req)
	if err != nil {
		return fmt.Errorf("marshaling to JSON: %w", err)
	}

	outputPath := filepath.Join(outputDir, "request.json")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, jsonBytes, 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	// Write a minimal successful response back to protoc
	supportedFeatures := uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL | pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
	minEdition := int32(descriptorpb.Edition_EDITION_PROTO2)
	maxEdition := int32(descriptorpb.Edition_EDITION_2023)
	resp := &pluginpb.CodeGeneratorResponse{
		SupportedFeatures: &supportedFeatures,
		MinimumEdition:    &minEdition,
		MaximumEdition:    &maxEdition,
	}
	respBytes, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}
	if _, err := os.Stdout.Write(respBytes); err != nil {
		return fmt.Errorf("writing response: %w", err)
	}

	// For comparison outputs (summary + binary), clear the parameter field
	// since it contains the output directory path which inherently differs
	// between C++ protoc and Go protoc-go test runs.
	reqForCompare := proto.Clone(&req).(*pluginpb.CodeGeneratorRequest)
	reqForCompare.Parameter = nil

	// Write request as binary for exact comparison (parameter cleared)
	binaryPath := filepath.Join(outputDir, "request.pb")
	deterministicBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(reqForCompare)
	if err != nil {
		return fmt.Errorf("marshaling deterministic binary: %w", err)
	}
	if err := os.WriteFile(binaryPath, deterministicBytes, 0o644); err != nil {
		return fmt.Errorf("writing binary output: %w", err)
	}

	// Write a sorted summary for quick human-readable diffing (parameter cleared)
	summary := buildSummary(reqForCompare)
	summaryPath := filepath.Join(outputDir, "summary.txt")
	if err := os.WriteFile(summaryPath, []byte(summary), 0o644); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}

	return nil
}

func buildSummary(req *pluginpb.CodeGeneratorRequest) string {
	var s string
	s += fmt.Sprintf("files_to_generate: %v\n", req.GetFileToGenerate())
	s += fmt.Sprintf("parameter: %q\n", req.GetParameter())
	s += fmt.Sprintf("compiler_version: %v\n", req.GetCompilerVersion())
	s += fmt.Sprintf("proto_file_count: %d\n", len(req.GetProtoFile()))
	s += fmt.Sprintf("source_file_descriptor_count: %d\n", len(req.GetSourceFileDescriptors()))
	s += "\n"
	for i, f := range req.GetProtoFile() {
		s += fmt.Sprintf("proto_file[%d]: %s (package=%q, syntax=%q)\n", i, f.GetName(), f.GetPackage(), f.GetSyntax())

		// Messages
		for _, m := range f.GetMessageType() {
			s += fmt.Sprintf("  message: %s\n", m.GetName())
			for _, field := range m.GetField() {
				s += fmt.Sprintf("    field: %s (number=%d, type=%v, label=%v)\n",
					field.GetName(), field.GetNumber(), field.GetType(), field.GetLabel())
			}
		}

		// Enums
		for _, e := range f.GetEnumType() {
			s += fmt.Sprintf("  enum: %s\n", e.GetName())
			for _, v := range e.GetValue() {
				s += fmt.Sprintf("    value: %s = %d\n", v.GetName(), v.GetNumber())
			}
		}

		// Services
		for _, svc := range f.GetService() {
			s += fmt.Sprintf("  service: %s\n", svc.GetName())
			for _, m := range svc.GetMethod() {
				s += fmt.Sprintf("    rpc: %s(%s) returns (%s)\n",
					m.GetName(), m.GetInputType(), m.GetOutputType())
			}
		}

		// Source code info
		if sci := f.GetSourceCodeInfo(); sci != nil {
			s += fmt.Sprintf("  source_code_info_locations: %d\n", len(sci.GetLocation()))
		}
	}

	return s
}

// prettyJSON re-formats JSON bytes with sorted keys for deterministic output.
func prettyJSON(data []byte) ([]byte, error) {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	return json.MarshalIndent(obj, "", "  ")
}
