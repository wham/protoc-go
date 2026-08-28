// protoc-gen-mock is a misbehaving/feature-exercising protoc plugin used by the
// test harness to compare how C++ protoc and Go protoc-go handle the response
// side of the plugin protocol: generated files, insertion points, declared
// error responses, crashes, malformed output, invalid file names, and feature
// gating (proto3 optional / editions support).
//
// The behavior is selected by the plugin parameter (--mock_opt). The first
// comma-separated token is the mode:
//
//	respfiles      response with several files (nested dirs, empty file)
//	insertion      base file plus an insertion into it in the same response
//	error          response with the error field set
//	exit3          write to stderr and exit with status 3
//	garbage        write bytes that are not a CodeGeneratorResponse
//	dupfile        response with two files of the same name
//	badname_dotdot response file named ../escaped.txt
//	badname_abs    response file with an absolute name
//	nofeat         declare no supported features (gates proto3 optional/editions)
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "protoc-gen-mock: %v\n", err)
		os.Exit(1)
	}
}

func str(s string) *string { return &s }

func run() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("unmarshaling CodeGeneratorRequest: %w", err)
	}

	mode := req.GetParameter()
	if i := strings.Index(mode, ","); i >= 0 {
		mode = mode[:i]
	}

	allFeatures := uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL |
		pluginpb.CodeGeneratorResponse_FEATURE_SUPPORTS_EDITIONS)
	minEdition := int32(descriptorpb.Edition_EDITION_PROTO2)
	maxEdition := int32(descriptorpb.Edition_EDITION_2024)

	resp := &pluginpb.CodeGeneratorResponse{
		SupportedFeatures: &allFeatures,
		MinimumEdition:    &minEdition,
		MaximumEdition:    &maxEdition,
	}

	switch mode {
	case "respfiles":
		resp.File = []*pluginpb.CodeGeneratorResponse_File{
			{Name: str("simple.txt"), Content: str("hello from mock\n")},
			{Name: str("sub/nested/deep.txt"), Content: str("nested content\nsecond line\n")},
			{Name: str("empty.txt"), Content: str("")},
		}
	case "insertion":
		resp.File = []*pluginpb.CodeGeneratorResponse_File{
			{Name: str("ins.txt"), Content: str("first line\n  // @@protoc_insertion_point(marker)\nlast line\n")},
			{Name: str("ins.txt"), InsertionPoint: str("marker"), Content: str("inserted A\ninserted B\n")},
		}
	case "error":
		resp = &pluginpb.CodeGeneratorResponse{Error: str("mock generator failed on purpose")}
	case "exit3":
		fmt.Fprintln(os.Stderr, "mock plugin exploding")
		os.Exit(3)
	case "garbage":
		os.Stdout.WriteString("this is definitely not a serialized CodeGeneratorResponse")
		return nil
	case "dupfile":
		resp.File = []*pluginpb.CodeGeneratorResponse_File{
			{Name: str("twice.txt"), Content: str("first\n")},
			{Name: str("twice.txt"), Content: str("second\n")},
		}
	case "badname_dotdot":
		resp.File = []*pluginpb.CodeGeneratorResponse_File{
			{Name: str("../escaped.txt"), Content: str("should not be written outside the output directory\n")},
		}
	case "badname_abs":
		resp.File = []*pluginpb.CodeGeneratorResponse_File{
			{Name: str("/absolute/escaped.txt"), Content: str("absolute names are invalid\n")},
		}
	case "nofeat":
		zero := uint64(0)
		resp = &pluginpb.CodeGeneratorResponse{
			SupportedFeatures: &zero,
			File: []*pluginpb.CodeGeneratorResponse_File{
				{Name: str("nofeat.txt"), Content: str("generated without declaring features\n")},
			},
		}
	default:
		return fmt.Errorf("unknown mock mode %q", mode)
	}

	respBytes, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}
	if _, err := os.Stdout.Write(respBytes); err != nil {
		return fmt.Errorf("writing response: %w", err)
	}
	return nil
}
