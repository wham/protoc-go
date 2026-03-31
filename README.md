# protoc-go

> **Warning**
> This is an experimental project under active development. It is **not ready for production use**. APIs may change without notice.

A pure Go implementation of the Protocol Buffers compiler (`protoc`). Can be used as a **CLI tool** (drop-in replacement) or as a **Go library** embedded directly in your application.

## CLI Usage

```bash
go install github.com/wham/protoc-go/cmd/protoc-go@latest

protoc-go --go_out=. --go_opt=paths=source_relative -I./protos api/v1/service.proto
```

Supports all standard protoc flags (`--proto_path`, `--descriptor_set_out`, `--decode`, `--encode`, plugins, etc.). See the [official protoc documentation](https://protobuf.dev/reference/protoc/) for the full flag reference.

## Go Library Usage

Import the `protoc` package to compile `.proto` files programmatically — no subprocess, no `protoc` binary needed.

### Compile to descriptors

```go
import "github.com/wham/protoc-go/protoc"

c := protoc.New(
    protoc.WithProtoPaths("./protos", "./vendor/proto"),
)

result, err := c.Compile("api/v1/service.proto")
if err != nil {
    log.Fatal(err)
}

for _, fd := range result.Files {
    fmt.Println(fd.GetName(), len(fd.GetMessageType()), "messages")
}

// Serialize as FileDescriptorSet
data, _ := proto.Marshal(result.AsFileDescriptorSet())
```

### Run code generation plugins

```go
result, _ := c.Compile("service.proto")

files, err := result.RunPlugin("protoc-gen-go", "paths=source_relative")
if err != nil {
    log.Fatal(err)
}

for _, f := range files {
    fmt.Printf("%s (%d bytes)\n", f.Name, len(f.Content))
    os.WriteFile(f.Name, []byte(f.Content), 0644)
}
```

### Compile in-memory sources

```go
c := protoc.New(
    protoc.WithOverlay(map[string]string{
        "schema.proto": `
            syntax = "proto3";
            package dynamic;
            message Record { string id = 1; string data = 2; }
        `,
    }),
)
result, err := c.Compile("schema.proto")
```

### Run plugins as libraries (protoc-go extension)

> **Note:** This is a proprietary protoc-go extension. Standard C++ protoc only supports plugins as external binaries communicating over stdin/stdout. This feature allows Go-based plugins to run in-process, eliminating subprocess overhead.

Go plugins can implement the `protoc.Plugin` interface to run directly in the compiler process — no binary on `PATH`, no serialization, no subprocess:

```go
import (
    "github.com/wham/protoc-go/protoc"
    pluginpb "google.golang.org/protobuf/types/pluginpb"
)

// Define a plugin as a function
myPlugin := protoc.PluginFunc(func(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
    // req contains the same CodeGeneratorRequest a subprocess plugin receives
    var files []*pluginpb.CodeGeneratorResponse_File
    for _, f := range req.GetFileToGenerate() {
        name := f + ".txt"
        content := "// Generated from " + f
        files = append(files, &pluginpb.CodeGeneratorResponse_File{
            Name: &name, Content: &content,
        })
    }
    return &pluginpb.CodeGeneratorResponse{File: files}, nil
})

result, _ := c.Compile("service.proto")
files, err := result.RunLibraryPlugin(myPlugin, "")
```

Existing plugins that already use `protoc-gen-*` binaries continue to work with `RunPlugin`. Library plugins receive the identical `CodeGeneratorRequest`, so output is equivalent between the two modes.

### Thread safety

`Compiler` is safe for concurrent use. Each `Compile()` call uses independent internal state — no mutexes, no shared mutable data. Create one `Compiler` and reuse it across goroutines.

## Build & Test

```bash
# Run the test suite (compares Go protoc-go vs C++ protoc output)
scripts/test

# Summary only
scripts/test --summary
```

Requires: Go 1.23+, C++ `protoc` installed (e.g. `brew install protobuf`) for the comparison test suite.

## How It Works

A fake plugin (`protoc-gen-dump`) captures the `CodeGeneratorRequest` that protoc sends to plugins. The test harness runs both C++ protoc and Go protoc-go on the same `.proto` files, then diffs the captured requests. If they match, the port is correct.

## Automated Development Loop

```bash
# Start the RALPH/NELSON adversarial loop
./ralph.sh
```

- **RALPH** (builder) fixes failing tests one at a time
- **NELSON** (adversarial tester) creates new tests to find bugs
- The loop continues until NELSON can't break it
