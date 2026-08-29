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

// SkipJSONEnv suppresses the request.json dump when set to a non-empty value.
// Everything the test harness actually compares is still written.
const SkipJSONEnv = "PROTOC_GEN_DUMP_SKIP_JSON"

// NewPlugin returns a protoc-gen-dump plugin that can be used in-process
// with [protoc.CompileResult.RunLibraryPlugin].
//
// The plugin uses the parameter string as the output directory, writing
// request.json, request.pb, and summary.txt — the same files that the
// protoc-gen-dump binary writes, and honouring SkipJSONEnv the same way.
func NewPlugin() protoc.Plugin {
	return protoc.PluginFunc(Generate)
}

// Generate processes a CodeGeneratorRequest the same way the protoc-gen-dump
// binary does: it writes request.json, request.pb, and summary.txt to the
// directory specified in the request parameter. request.json is skipped when
// SkipJSONEnv is set.
func Generate(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	outputDir := req.GetParameter()
	if outputDir == "" {
		outputDir = "."
	} else if idx := strings.LastIndex(outputDir, ","); idx >= 0 {
		outputDir = outputDir[idx+1:]
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	// request.json is a debugging aid, not a comparison artifact: the harness
	// diffs request.pb, summary.txt and parameter.txt and never reads this. On
	// the big bench tiers rendering it costs an order of magnitude more than
	// the compile it surrounds, so the perf harness sets SkipJSONEnv rather
	// than time the plugin instead of the compiler.
	if os.Getenv(SkipJSONEnv) == "" {
		marshaler := protojson.MarshalOptions{
			Multiline: true,
			Indent:    "  ",
		}
		jsonBytes, err := marshaler.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshaling to JSON: %w", err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, "request.json"), jsonBytes, 0o644); err != nil {
			return nil, fmt.Errorf("writing output: %w", err)
		}
	}

	// Record the raw parameter string protoc sent. The harness compares this
	// between compilers after normalizing the output-directory path out of it;
	// request.pb cannot carry it because the directories necessarily differ
	// between the C++ and Go runs.
	parameterPath := filepath.Join(outputDir, "parameter.txt")
	if err := os.WriteFile(parameterPath, []byte(req.GetParameter()), 0o644); err != nil {
		return nil, fmt.Errorf("writing parameter: %w", err)
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
	var b strings.Builder
	fmt.Fprintf(&b, "files_to_generate: %v\n", req.GetFileToGenerate())
	fmt.Fprintf(&b, "parameter: %q\n", req.GetParameter())
	fmt.Fprintf(&b, "compiler_version: %v\n", req.GetCompilerVersion())
	fmt.Fprintf(&b, "proto_file_count: %d\n", len(req.GetProtoFile()))
	fmt.Fprintf(&b, "source_file_descriptor_count: %d\n", len(req.GetSourceFileDescriptors()))
	b.WriteString("\n")
	for i, f := range req.GetProtoFile() {
		fmt.Fprintf(&b, "proto_file[%d]: %s (package=%q, syntax=%q)\n", i, f.GetName(), f.GetPackage(), f.GetSyntax())

		for _, m := range f.GetMessageType() {
			fmt.Fprintf(&b, "  message: %s\n", m.GetName())
			for _, field := range m.GetField() {
				fmt.Fprintf(&b, "    field: %s (number=%d, type=%v, label=%v)\n",
					field.GetName(), field.GetNumber(), field.GetType(), field.GetLabel())
			}
		}

		for _, e := range f.GetEnumType() {
			fmt.Fprintf(&b, "  enum: %s\n", e.GetName())
			for _, v := range e.GetValue() {
				fmt.Fprintf(&b, "    value: %s = %d\n", v.GetName(), v.GetNumber())
			}
		}

		for _, svc := range f.GetService() {
			fmt.Fprintf(&b, "  service: %s\n", svc.GetName())
			for _, m := range svc.GetMethod() {
				fmt.Fprintf(&b, "    rpc: %s(%s) returns (%s)\n",
					m.GetName(), m.GetInputType(), m.GetOutputType())
			}
		}

		if sci := f.GetSourceCodeInfo(); sci != nil {
			fmt.Fprintf(&b, "  source_code_info_locations: %d\n", len(sci.GetLocation()))
		}
	}

	return b.String()
}
