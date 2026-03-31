// Package dump implements the protoc-gen-dump plugin logic as a reusable library.
//
// This package can be used both as a subprocess plugin (via the protoc-gen-dump
// binary) and as an in-process library plugin via [NewPlugin].
package dump

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wham/protoc-go/protoc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

// NewPlugin returns a protoc-gen-dump plugin that can be used in-process
// with [protoc.CompileResult.RunLibraryPlugin].
//
// The plugin uses the parameter string as the output directory, writing
// request.json, request.pb, and summary.txt — the same files that the
// protoc-gen-dump binary writes.
func NewPlugin() protoc.Plugin {
	return protoc.PluginFunc(Generate)
}

// Generate processes a CodeGeneratorRequest the same way the protoc-gen-dump
// binary does: it writes request.json, request.pb, and summary.txt to the
// directory specified in the request parameter.
func Generate(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	outputDir := req.GetParameter()
	if outputDir == "" {
		outputDir = "."
	} else if idx := strings.LastIndex(outputDir, ","); idx >= 0 {
		outputDir = outputDir[idx+1:]
	}

	// Marshal to deterministic JSON
	marshaler := protojson.MarshalOptions{
		Multiline: true,
		Indent:    "  ",
	}
	jsonBytes, err := marshaler.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling to JSON: %w", err)
	}

	outputPath := filepath.Join(outputDir, "request.json")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, jsonBytes, 0o644); err != nil {
		return nil, fmt.Errorf("writing output: %w", err)
	}

	// For comparison outputs, clear the parameter field
	reqForCompare := proto.Clone(req).(*pluginpb.CodeGeneratorRequest)
	reqForCompare.Parameter = nil

	// Write request as binary for exact comparison
	binaryPath := filepath.Join(outputDir, "request.pb")
	deterministicBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(reqForCompare)
	if err != nil {
		return nil, fmt.Errorf("marshaling deterministic binary: %w", err)
	}
	if err := os.WriteFile(binaryPath, deterministicBytes, 0o644); err != nil {
		return nil, fmt.Errorf("writing binary output: %w", err)
	}

	// Write sorted summary
	summary := BuildSummary(reqForCompare)
	summaryPath := filepath.Join(outputDir, "summary.txt")
	if err := os.WriteFile(summaryPath, []byte(summary), 0o644); err != nil {
		return nil, fmt.Errorf("writing summary: %w", err)
	}

	supportedFeatures := uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL | pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
	minEdition := int32(descriptorpb.Edition_EDITION_PROTO2)
	maxEdition := int32(descriptorpb.Edition_EDITION_2024)
	return &pluginpb.CodeGeneratorResponse{
		SupportedFeatures: &supportedFeatures,
		MinimumEdition:    &minEdition,
		MaximumEdition:    &maxEdition,
	}, nil
}

// BuildSummary creates a human-readable text summary of a CodeGeneratorRequest.
func BuildSummary(req *pluginpb.CodeGeneratorRequest) string {
	var s string
	s += fmt.Sprintf("files_to_generate: %v\n", req.GetFileToGenerate())
	s += fmt.Sprintf("parameter: %q\n", req.GetParameter())
	s += fmt.Sprintf("compiler_version: %v\n", req.GetCompilerVersion())
	s += fmt.Sprintf("proto_file_count: %d\n", len(req.GetProtoFile()))
	s += fmt.Sprintf("source_file_descriptor_count: %d\n", len(req.GetSourceFileDescriptors()))
	s += "\n"
	for i, f := range req.GetProtoFile() {
		s += fmt.Sprintf("proto_file[%d]: %s (package=%q, syntax=%q)\n", i, f.GetName(), f.GetPackage(), f.GetSyntax())

		for _, m := range f.GetMessageType() {
			s += fmt.Sprintf("  message: %s\n", m.GetName())
			for _, field := range m.GetField() {
				s += fmt.Sprintf("    field: %s (number=%d, type=%v, label=%v)\n",
					field.GetName(), field.GetNumber(), field.GetType(), field.GetLabel())
			}
		}

		for _, e := range f.GetEnumType() {
			s += fmt.Sprintf("  enum: %s\n", e.GetName())
			for _, v := range e.GetValue() {
				s += fmt.Sprintf("    value: %s = %d\n", v.GetName(), v.GetNumber())
			}
		}

		for _, svc := range f.GetService() {
			s += fmt.Sprintf("  service: %s\n", svc.GetName())
			for _, m := range svc.GetMethod() {
				s += fmt.Sprintf("    rpc: %s(%s) returns (%s)\n",
					m.GetName(), m.GetInputType(), m.GetOutputType())
			}
		}

		if sci := f.GetSourceCodeInfo(); sci != nil {
			s += fmt.Sprintf("  source_code_info_locations: %d\n", len(sci.GetLocation()))
		}
	}

	return s
}
