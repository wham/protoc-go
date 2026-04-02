package protoc_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wham/protoc-go/protoc"
	"github.com/wham/protoc-go/tools/protoc-gen-dump/dump"
	pluginpb "google.golang.org/protobuf/types/pluginpb"
)

func TestCompileBasicMessage(t *testing.T) {
	c := protoc.New(
		protoc.WithProtoPaths("../testdata/01_basic_message"),
	)

	result, err := c.Compile("basic.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}

	fd := result.Files[0]
	if fd.GetName() != "basic.proto" {
		t.Errorf("expected file name basic.proto, got %s", fd.GetName())
	}
	if fd.GetPackage() != "basic" {
		t.Errorf("expected package basic, got %s", fd.GetPackage())
	}
	if len(fd.GetMessageType()) != 1 {
		t.Fatalf("expected 1 message, got %d", len(fd.GetMessageType()))
	}
	msg := fd.GetMessageType()[0]
	if msg.GetName() != "Person" {
		t.Errorf("expected message Person, got %s", msg.GetName())
	}
	if len(msg.GetField()) != 3 {
		t.Errorf("expected 3 fields, got %d", len(msg.GetField()))
	}
}

func TestCompileWithOverlay(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"test.proto": `syntax = "proto3";
package test;
message Ping {
  string message = 1;
}
`,
		}),
	)

	result, err := c.Compile("test.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}

	fd := result.Files[0]
	if fd.GetPackage() != "test" {
		t.Errorf("expected package test, got %s", fd.GetPackage())
	}
	if len(fd.GetMessageType()) != 1 {
		t.Fatalf("expected 1 message, got %d", len(fd.GetMessageType()))
	}
	if fd.GetMessageType()[0].GetName() != "Ping" {
		t.Errorf("expected message Ping, got %s", fd.GetMessageType()[0].GetName())
	}
}

func TestCompileMultipleFiles(t *testing.T) {
	c := protoc.New(
		protoc.WithProtoPaths("../testdata/01_basic_message"),
	)

	result, err := c.Compile("a.proto", "b.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(result.Files) < 2 {
		t.Fatalf("expected at least 2 files, got %d", len(result.Files))
	}
}

func TestCompileWithIncludeImports(t *testing.T) {
	// Create two files, one importing the other
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"dep.proto": `syntax = "proto3";
package dep;
message Dep { string val = 1; }
`,
			"main.proto": `syntax = "proto3";
package main;
import "dep.proto";
message Main { dep.Dep dep = 1; }
`,
		}),
		protoc.WithIncludeImports(),
	)

	result, err := c.Compile("main.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files (main + dep), got %d", len(result.Files))
	}
}

func TestCompileWithoutIncludeImports(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"dep.proto": `syntax = "proto3";
package dep;
message Dep { string val = 1; }
`,
			"main.proto": `syntax = "proto3";
package main;
import "dep.proto";
message Main { dep.Dep dep = 1; }
`,
		}),
	)

	result, err := c.Compile("main.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file (main only), got %d", len(result.Files))
	}
	if result.Files[0].GetName() != "main.proto" {
		t.Errorf("expected main.proto, got %s", result.Files[0].GetName())
	}
}

func TestCompileIncludeSourceInfo(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"test.proto": `syntax = "proto3";
package test;
message Foo { string bar = 1; }
`,
		}),
		protoc.WithIncludeSourceInfo(),
	)

	result, err := c.Compile("test.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	fd := result.Files[0]
	if fd.GetSourceCodeInfo() == nil {
		t.Error("expected SourceCodeInfo to be present with WithIncludeSourceInfo")
	}
}

func TestCompileWithoutSourceInfo(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"test.proto": `syntax = "proto3";
package test;
message Foo { string bar = 1; }
`,
		}),
	)

	result, err := c.Compile("test.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	fd := result.Files[0]
	if fd.GetSourceCodeInfo() != nil {
		t.Error("expected SourceCodeInfo to be stripped by default")
	}
}

func TestCompileWithWellKnownTypes(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"wkt.proto": `syntax = "proto3";
package wkt;
import "google/protobuf/timestamp.proto";
message Event {
  string name = 1;
  google.protobuf.Timestamp created_at = 2;
}
`,
		}),
	)

	result, err := c.Compile("wkt.proto")
	if err != nil {
		t.Fatalf("Compile failed (well-known type import should resolve via fallback): %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	fd := result.Files[0]
	if len(fd.GetDependency()) != 1 || fd.GetDependency()[0] != "google/protobuf/timestamp.proto" {
		t.Errorf("expected dependency on google/protobuf/timestamp.proto, got %v", fd.GetDependency())
	}
}

func TestCompileError(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"bad.proto": `syntax = "proto3"; message Foo {`,
		}),
	)

	_, err := c.Compile("bad.proto")
	if err == nil {
		t.Fatal("expected compile error for malformed proto")
	}
}

func TestCompileFileNotFound(t *testing.T) {
	c := protoc.New(
		protoc.WithProtoPaths(t.TempDir()),
	)

	_, err := c.Compile("nonexistent.proto")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCompileNoFiles(t *testing.T) {
	c := protoc.New()
	_, err := c.Compile()
	if err == nil {
		t.Fatal("expected error when no files specified")
	}
}

func TestAsFileDescriptorSet(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"test.proto": `syntax = "proto3";
package test;
message Foo { string bar = 1; }
`,
		}),
	)

	result, err := c.Compile("test.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	fds := result.AsFileDescriptorSet()
	if len(fds.GetFile()) != 1 {
		t.Fatalf("expected 1 file in descriptor set, got %d", len(fds.GetFile()))
	}
}

func TestFileDescriptorHelper(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"dep.proto": `syntax = "proto3";
package dep;
message Dep { string val = 1; }
`,
			"main.proto": `syntax = "proto3";
package main;
import "dep.proto";
message Main { dep.Dep dep = 1; }
`,
		}),
		protoc.WithIncludeImports(),
	)

	result, err := c.Compile("main.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	fd := protoc.FileDescriptor(result, "main.proto")
	if fd == nil {
		t.Fatal("expected to find main.proto")
	}
	if fd.GetPackage() != "main" {
		t.Errorf("expected package main, got %s", fd.GetPackage())
	}

	if protoc.FileDescriptor(result, "nonexistent.proto") != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestCompileFileConvenience(t *testing.T) {
	// Write a temp proto file
	dir := t.TempDir()
	protoContent := `syntax = "proto3";
package conv;
message Msg { int32 id = 1; }
`
	if err := os.WriteFile(filepath.Join(dir, "conv.proto"), []byte(protoContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := protoc.CompileFile("conv.proto", protoc.WithProtoPaths(dir))
	if err != nil {
		t.Fatalf("CompileFile failed: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
}

func TestCompileEnum(t *testing.T) {
	c := protoc.New(
		protoc.WithProtoPaths("../testdata/02_enum"),
	)

	result, err := c.Compile("enums.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	fd := result.Files[0]
	if len(fd.GetEnumType()) == 0 {
		t.Error("expected at least one top-level enum")
	}
}

func TestCompileService(t *testing.T) {
	c := protoc.New(
		protoc.WithProtoPaths("../testdata/03_service"),
	)

	result, err := c.Compile("service.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	fd := result.Files[0]
	if len(fd.GetService()) == 0 {
		t.Error("expected at least one service")
	}
}

func TestCompileValidationError(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"dup.proto": `syntax = "proto3";
package dup;
message Foo {
  string name = 1;
  int32 name = 2;
}
`,
		}),
	)

	_, err := c.Compile("dup.proto")
	if err == nil {
		t.Fatal("expected validation error for duplicate field name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected error about 'name', got: %v", err)
	}
}

func TestRunPlugin(t *testing.T) {
	// Build the protoc-gen-dump test plugin
	tmpDir := t.TempDir()
	dumpBin := filepath.Join(tmpDir, "protoc-gen-dump")
	cmd := exec.Command("go", "build", "-o", dumpBin, "../tools/protoc-gen-dump")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building protoc-gen-dump: %v\n%s", err, out)
	}

	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"test.proto": `syntax = "proto3";
package test;
message Ping { string message = 1; }
`,
		}),
	)

	result, err := c.Compile("test.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// protoc-gen-dump writes files to disk (via its parameter), not via
	// CodeGeneratorResponse. Pass the output dir as the plugin parameter.
	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := result.RunPlugin(dumpBin, outDir)
	if err != nil {
		t.Fatalf("RunPlugin failed: %v", err)
	}

	// protoc-gen-dump returns an empty response (files written to disk instead)
	_ = files

	// Verify the plugin received and processed the request correctly
	requestJSON, err := os.ReadFile(filepath.Join(outDir, "request.json"))
	if err != nil {
		t.Fatalf("expected request.json to be written by plugin: %v", err)
	}
	if !strings.Contains(string(requestJSON), "test.proto") {
		t.Error("expected request.json to reference test.proto")
	}
	if !strings.Contains(string(requestJSON), "Ping") {
		t.Error("expected request.json to contain Ping message")
	}
}

func TestRunLibraryPlugin(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"test.proto": `syntax = "proto3";
package test;
message Ping { string message = 1; }
`,
		}),
	)

	result, err := c.Compile("test.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	outDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Use protoc-gen-dump as an in-process library plugin
	files, err := result.RunLibraryPlugin(dump.NewPlugin(), outDir)
	if err != nil {
		t.Fatalf("RunLibraryPlugin failed: %v", err)
	}

	// protoc-gen-dump returns an empty file list (writes to disk instead)
	_ = files

	// Verify the plugin received and processed the request correctly
	requestJSON, err := os.ReadFile(filepath.Join(outDir, "request.json"))
	if err != nil {
		t.Fatalf("expected request.json to be written by plugin: %v", err)
	}
	if !strings.Contains(string(requestJSON), "test.proto") {
		t.Error("expected request.json to reference test.proto")
	}
	if !strings.Contains(string(requestJSON), "Ping") {
		t.Error("expected request.json to contain Ping message")
	}

	// Verify summary was also written
	summary, err := os.ReadFile(filepath.Join(outDir, "summary.txt"))
	if err != nil {
		t.Fatalf("expected summary.txt to be written by plugin: %v", err)
	}
	if !strings.Contains(string(summary), "Ping") {
		t.Error("expected summary.txt to contain Ping message")
	}
}

func TestRunLibraryPluginFunc(t *testing.T) {
	c := protoc.New(
		protoc.WithOverlay(map[string]string{
			"svc.proto": `syntax = "proto3";
package svc;
message Req { string id = 1; }
message Resp { string data = 1; }
service Greeter {
  rpc Hello(Req) returns (Resp);
}
`,
		}),
	)

	result, err := c.Compile("svc.proto")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Use a PluginFunc to generate a simple manifest listing all files,
	// messages, and services.
	manifest := protoc.PluginFunc(func(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
		var lines []string
		for _, f := range req.GetFileToGenerate() {
			lines = append(lines, "file: "+f)
		}
		for _, pf := range req.GetProtoFile() {
			for _, m := range pf.GetMessageType() {
				lines = append(lines, "message: "+m.GetName())
			}
			for _, s := range pf.GetService() {
				lines = append(lines, "service: "+s.GetName())
			}
		}
		content := strings.Join(lines, "\n") + "\n"
		name := "manifest.txt"
		return &pluginpb.CodeGeneratorResponse{
			File: []*pluginpb.CodeGeneratorResponse_File{
				{Name: &name, Content: &content},
			},
		}, nil
	})

	files, err := result.RunLibraryPlugin(manifest, "")
	if err != nil {
		t.Fatalf("RunLibraryPlugin failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 generated file, got %d", len(files))
	}
	if files[0].Name != "manifest.txt" {
		t.Errorf("expected manifest.txt, got %s", files[0].Name)
	}
	content := files[0].Content
	if !strings.Contains(content, "file: svc.proto") {
		t.Errorf("expected manifest to contain 'file: svc.proto', got:\n%s", content)
	}
	if !strings.Contains(content, "message: Req") {
		t.Errorf("expected manifest to contain 'message: Req', got:\n%s", content)
	}
	if !strings.Contains(content, "message: Resp") {
		t.Errorf("expected manifest to contain 'message: Resp', got:\n%s", content)
	}
	if !strings.Contains(content, "service: Greeter") {
		t.Errorf("expected manifest to contain 'service: Greeter', got:\n%s", content)
	}
}

func TestConcurrentCompile(t *testing.T) {
	c := protoc.New(
		protoc.WithProtoPaths("../testdata/01_basic_message"),
	)

	const goroutines = 10
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			result, err := c.Compile("basic.proto")
			if err != nil {
				errs <- err
				return
			}
			if len(result.Files) != 1 {
				errs <- fmt.Errorf("expected 1 file, got %d", len(result.Files))
				return
			}
			if result.Files[0].GetMessageType()[0].GetName() != "Person" {
				errs <- fmt.Errorf("expected Person message")
				return
			}
			errs <- nil
		}()
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}
