// Package protoc provides a Go library API for the Protocol Buffers compiler.
//
// This package allows embedding protoc directly in Go applications without
// invoking it as a separate CLI process.
//
// # Basic usage
//
//	c := protoc.New(
//	    protoc.WithProtoPaths("./protos", "./vendor/proto"),
//	)
//
//	result, err := c.Compile("api/v1/service.proto", "api/v1/messages.proto")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Access compiled descriptors
//	for _, fd := range result.Files {
//	    fmt.Println(fd.GetName())
//	}
//
//	// Serialize as FileDescriptorSet
//	fds := result.AsFileDescriptorSet()
//	data, _ := proto.Marshal(fds)
//
// # Working with in-memory sources
//
// For cases where .proto content comes from sources other than the filesystem
// (e.g., a database, network, or generated at runtime), use an Overlay:
//
//	c := protoc.New(
//	    protoc.WithOverlay(map[string]string{
//	        "api/v1/service.proto": `syntax = "proto3"; message Ping {}`,
//	    }),
//	)
//	result, err := c.Compile("api/v1/service.proto")
package protoc

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wham/protoc-go/compiler/cli"
	"github.com/wham/protoc-go/compiler/importer"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// Compiler is a configured protocol buffer compiler.
// Create one with [New] and reuse it across multiple [Compiler.Compile] calls.
type Compiler struct {
	protoPaths     []string
	mappings       []importer.Mapping
	includeImports bool
	sourceInfo     bool
	retainOptions  bool
	descriptorSets []string
	overlay        map[string]string
}

// Option configures a [Compiler].
type Option func(*Compiler)

// New creates a new [Compiler] with the given options.
func New(opts ...Option) *Compiler {
	c := &Compiler{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithProtoPaths adds directories to search for .proto files and their imports.
// Equivalent to protoc's -I / --proto_path flag.
// If no proto paths are configured, "." is used by default.
func WithProtoPaths(paths ...string) Option {
	return func(c *Compiler) {
		c.protoPaths = append(c.protoPaths, paths...)
	}
}

// WithMapping maps a virtual import path to a disk path.
// For example, WithMapping("google/protobuf", "/usr/include/google/protobuf")
// allows imports like `import "google/protobuf/descriptor.proto"` to resolve
// to files under /usr/include/google/protobuf/.
func WithMapping(virtualPath, diskPath string) Option {
	return func(c *Compiler) {
		c.mappings = append(c.mappings, importer.Mapping{
			VirtualPath: virtualPath,
			DiskPath:    diskPath,
		})
	}
}

// WithIncludeImports causes [Compiler.Compile] to include all transitive
// dependencies in the result, not just the directly requested files.
// Equivalent to protoc's --include_imports flag.
func WithIncludeImports() Option {
	return func(c *Compiler) {
		c.includeImports = true
	}
}

// WithIncludeSourceInfo preserves SourceCodeInfo in the output descriptors.
// This includes original source locations and comments but increases
// descriptor size significantly.
// Equivalent to protoc's --include_source_info flag.
func WithIncludeSourceInfo() Option {
	return func(c *Compiler) {
		c.sourceInfo = true
	}
}

// WithRetainOptions keeps all options in the output, including source-retention
// options that are normally stripped.
// Equivalent to protoc's --retain_options flag.
func WithRetainOptions() Option {
	return func(c *Compiler) {
		c.retainOptions = true
	}
}

// WithDescriptorSetIn provides pre-compiled FileDescriptorSet files to use
// for resolving imports. This is useful when some dependencies are already
// compiled.
// Equivalent to protoc's --descriptor_set_in flag.
func WithDescriptorSetIn(paths ...string) Option {
	return func(c *Compiler) {
		c.descriptorSets = append(c.descriptorSets, paths...)
	}
}

// WithOverlay provides in-memory .proto file contents that take precedence
// over files on disk. The keys are virtual paths (e.g., "mypackage/foo.proto")
// and values are the .proto file contents.
//
// This enables compiling .proto definitions that are generated at runtime,
// fetched from a database, or otherwise not on the local filesystem.
func WithOverlay(files map[string]string) Option {
	return func(c *Compiler) {
		if c.overlay == nil {
			c.overlay = make(map[string]string)
		}
		for k, v := range files {
			c.overlay[k] = v
		}
	}
}

// Result contains the output of a successful [Compiler.Compile] call.
type Result = cli.CompileResult

// Compile parses and validates the given .proto files and returns their
// compiled [descriptorpb.FileDescriptorProto] representations.
//
// The file paths should be relative to one of the configured proto paths.
// All transitive imports are resolved and validated, but only the requested
// files are included in the result unless [WithIncludeImports] was set.
func (c *Compiler) Compile(protoFiles ...string) (*Result, error) {
	if len(protoFiles) == 0 {
		return nil, fmt.Errorf("protoc: no proto files specified")
	}

	// If overlay is set, write files to a temp directory and add it as a proto path
	var cleanupDir string
	protoPaths := c.protoPaths
	if len(c.overlay) > 0 {
		tmpDir, err := os.MkdirTemp("", "protoc-overlay-*")
		if err != nil {
			return nil, fmt.Errorf("protoc: creating overlay temp dir: %w", err)
		}
		cleanupDir = tmpDir
		defer os.RemoveAll(cleanupDir)

		for name, content := range c.overlay {
			p := filepath.Join(tmpDir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return nil, fmt.Errorf("protoc: creating overlay dir: %w", err)
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				return nil, fmt.Errorf("protoc: writing overlay file: %w", err)
			}
		}
		// Overlay directory takes precedence (prepended)
		protoPaths = append([]string{tmpDir}, protoPaths...)
	}

	return cli.Compile(&cli.CompileRequest{
		ProtoFiles:        protoFiles,
		ProtoPaths:        protoPaths,
		ProtoPathMappings: c.mappings,
		IncludeImports:    c.includeImports,
		IncludeSourceInfo: c.sourceInfo,
		RetainOptions:     c.retainOptions,
		DescriptorSetIn:   c.descriptorSets,
	})
}

// CompileFile is a convenience function that compiles a single .proto file
// with the given options.
func CompileFile(protoFile string, opts ...Option) (*Result, error) {
	return New(opts...).Compile(protoFile)
}

// CompileFiles is a convenience function that compiles multiple .proto files
// with the given options.
func CompileFiles(protoFiles []string, opts ...Option) (*Result, error) {
	return New(opts...).Compile(protoFiles...)
}

// FileDescriptor returns the first FileDescriptorProto from the result
// matching the given filename, or nil if not found.
func FileDescriptor(result *Result, name string) *descriptorpb.FileDescriptorProto {
	for _, fd := range result.Files {
		if fd.GetName() == name {
			return fd
		}
	}
	return nil
}
