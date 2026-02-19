// Package plugin implements plugin subprocess management for protoc.
// This mirrors C++ google::protobuf::compiler::Subprocess from compiler/subprocess.cc
// and the plugin protocol from compiler/plugin.cc.
package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

// RunPlugin executes a protoc plugin with the given CodeGeneratorRequest.
func RunPlugin(pluginPath string, req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	cmd := exec.Command(pluginPath)
	cmd.Stdin = nil
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting plugin %s: %w", pluginPath, err)
	}

	if _, err := stdinPipe.Write(reqBytes); err != nil {
		return nil, fmt.Errorf("writing to plugin stdin: %w", err)
	}
	stdinPipe.Close()

	respBytes, err := readAll(stdoutPipe)
	if err != nil {
		return nil, fmt.Errorf("reading plugin stdout: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("plugin %s failed: %w", pluginPath, err)
	}

	var resp pluginpb.CodeGeneratorResponse
	if err := proto.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return &resp, nil
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var result []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return result, err
		}
	}
	return result, nil
}

// BuildCodeGeneratorRequest builds a CodeGeneratorRequest from parsed file descriptors.
func BuildCodeGeneratorRequest(
	filesToGenerate []string,
	parameter string,
	protoFiles []*descriptorpb.FileDescriptorProto,
	sourceFileDescriptors []*descriptorpb.FileDescriptorProto,
) *pluginpb.CodeGeneratorRequest {
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate:        filesToGenerate,
		ProtoFile:             protoFiles,
		SourceFileDescriptors: sourceFileDescriptors,
		CompilerVersion: &pluginpb.Version{
			Major:  proto.Int32(5),
			Minor:  proto.Int32(29),
			Patch:  proto.Int32(3),
			Suffix: proto.String(""),
		},
	}
	if parameter != "" {
		req.Parameter = proto.String(parameter)
	}
	return req
}

// WritePluginOutput writes the files from a CodeGeneratorResponse to disk.
func WritePluginOutput(resp *pluginpb.CodeGeneratorResponse, outputDir string) error {
	for _, f := range resp.GetFile() {
		outPath := filepath.Join(outputDir, f.GetName())
		dir := filepath.Dir(outPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
		if err := os.WriteFile(outPath, []byte(f.GetContent()), 0o644); err != nil {
			return fmt.Errorf("writing file %s: %w", outPath, err)
		}
	}
	return nil
}
