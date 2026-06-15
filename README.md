# protoc-go

> **Warning** — experimental and under active development. Not ready for production use; APIs may change without notice.

A pure Go implementation of the Protocol Buffers compiler (`protoc`). Use it as a CLI drop-in replacement, or embed it as a library directly in your Go program — no `protoc` binary, no subprocess.

protoc-go is the compiler behind [**Kaja**](https://github.com/wham/kaja). Kaja needed a pure-Go `protoc` so it could embed the compiler directly into its binary — which also made it much easier to pass Apple App Store review.

## CLI

```bash
go install github.com/wham/protoc-go/cmd/protoc-go@latest

protoc-go --go_out=. --go_opt=paths=source_relative -I./protos api/v1/service.proto
```

The standard protoc flags work (`--proto_path`, `--descriptor_set_out`, `--decode`, `--encode`, plugins, …). See the [protoc reference](https://protobuf.dev/reference/protoc/) for the full list.

## Library

Compile `.proto` files programmatically and run code-gen plugins:

```go
import "github.com/wham/protoc-go/protoc"

c := protoc.New(protoc.WithProtoPaths("./protos"))

result, err := c.Compile("api/v1/service.proto")
if err != nil {
    log.Fatal(err)
}

files, _ := result.RunPlugin("protoc-gen-go", "paths=source_relative")
for _, f := range files {
    os.WriteFile(f.Name, []byte(f.Content), 0644)
}
```

You can also:

- compile in-memory sources with `protoc.WithOverlay(map[string]string{...})`,
- serialize a `FileDescriptorSet` via `result.AsFileDescriptorSet()`,
- run Go plugins in-process (no subprocess) with `result.RunLibraryPlugin` — they receive the same `CodeGeneratorRequest` a subprocess plugin would.

`Compiler` is safe for concurrent use: create one and reuse it across goroutines.

## Build & test

```bash
scripts/test            # compare Go protoc-go output against C++ protoc
scripts/bench           # performance comparison
```

Requires Go 1.23+, plus a C++ `protoc` on your PATH for the comparison suites (e.g. `brew install protobuf`).

See [AGENTS.md](AGENTS.md) for the architecture, how the test/bench harnesses work, and the automated development loop.
