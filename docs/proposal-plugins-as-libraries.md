# Proposal: Go Plugins as Libraries

## Summary

Today, `protoc-go` can be embedded as a Go library — no subprocess needed for
the compiler itself. However, code generation plugins (e.g., `protoc-gen-go`)
still require spawning a child process. This proposal describes how Go-based
plugins could also be used as in-process libraries, eliminating the last
subprocess in the compilation pipeline.

## Motivation

When using `protoc-go` as a library, the current flow is:

```
Go app → Compiler.Compile() [in-process] → result.RunPlugin("protoc-gen-go") [subprocess]
```

This has several drawbacks:

1. **Deployment friction** — Plugin binaries must be installed and discoverable
   on `PATH`. This complicates containerized builds, CI pipelines, and
   hermetic build systems.
2. **Performance overhead** — Each plugin invocation spawns a process,
   serializes the full `CodeGeneratorRequest` to protobuf, deserializes it in
   the plugin, then serializes the response back. For large schemas or
   frequent invocations this is wasteful.
3. **Incomplete embedding story** — The library API promises "embed protoc in
   your Go app," but you still need external binaries for the most important
   part: code generation.

## Design

### Core interface

Define a `Plugin` interface that mirrors what every Go plugin already does
internally — accept a request, return a response:

```go
// Plugin is a protoc code generation plugin that runs in-process.
//
// This is the in-process equivalent of a protoc-gen-* binary.
// The plugin receives the same CodeGeneratorRequest it would receive
// over stdin and returns the same CodeGeneratorResponse it would
// write to stdout.
type Plugin interface {
    Generate(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error)
}

// PluginFunc adapts a plain function into a Plugin.
type PluginFunc func(*pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error)

func (f PluginFunc) Generate(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
    return f(req)
}
```

This uses the same `pluginpb.CodeGeneratorRequest` / `CodeGeneratorResponse`
types that every existing Go plugin already works with. No new wire format, no
new abstractions.

### New API on CompileResult

Add a method that accepts an in-process plugin instead of a binary path:

```go
// RunLibraryPlugin executes an in-process Plugin against the compiled
// descriptors. It builds the same CodeGeneratorRequest that RunPlugin
// would send to a subprocess, but calls the plugin directly.
func (r *CompileResult) RunLibraryPlugin(p Plugin, parameter string) ([]GeneratedFile, error) {
    co := r.co
    protoFiles := co.buildProtoFiles()
    sourceFileDescriptors := co.buildSourceFileDescriptors()

    req := plugin.BuildCodeGeneratorRequest(co.relFiles, parameter, protoFiles, sourceFileDescriptors)

    resp, err := p.Generate(req)
    if err != nil {
        return nil, err
    }

    if resp.GetError() != "" {
        return nil, fmt.Errorf("plugin error: %s", resp.GetError())
    }

    var files []GeneratedFile
    for _, f := range resp.GetFile() {
        files = append(files, GeneratedFile{
            Name:           f.GetName(),
            Content:        f.GetContent(),
            InsertionPoint: f.GetInsertionPoint(),
        })
    }
    return files, nil
}
```

The implementation is nearly identical to the existing `RunPlugin` — the only
difference is calling `p.Generate(req)` instead of
`plugin.RunPlugin(pluginPath, req)`.

### How plugin authors expose a library entry point

Most Go plugins are built with `google.golang.org/protobuf/compiler/protogen`.
A typical plugin's `main()` looks like:

```go
func main() {
    protogen.Options{}.Run(func(gen *protogen.Plugin) error {
        for _, f := range gen.Files {
            if f.Generate {
                generateFile(gen, f)
            }
        }
        return nil
    })
}
```

Under the hood, `protogen.Options{}.Run()` reads `CodeGeneratorRequest` from
stdin and writes `CodeGeneratorResponse` to stdout. The actual logic is in the
function passed to `Run`.

To expose a library entry point, a plugin author would add a package-level
function (or a public `Plugin` value) next to their `main.go`:

```go
package main

import protocgo "github.com/wham/protoc-go/protoc"

// AsPlugin returns this plugin as an in-process library plugin.
// This allows protoc-go to call the plugin directly without a subprocess.
var AsPlugin protocgo.Plugin = protocgo.PluginFunc(func(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
    // Use protogen to process the request, but return the response
    // instead of writing to stdout.
    return protogen.Options{}.Process(req, func(gen *protogen.Plugin) error {
        for _, f := range gen.Files {
            if f.Generate {
                generateFile(gen, f)
            }
        }
        return nil
    })
})
```

> **Note:** `protogen.Options{}.Process()` does not exist today in the
> upstream `protogen` package. Currently only `Run()` (stdin/stdout) is
> available. The proposal below discusses options for bridging this gap.

### Bridging the protogen gap

The `protogen` package only exposes `Run()`, which hardcodes stdin/stdout I/O.
There are three strategies to work around this:

#### Option A: Upstream a `Process` method (preferred)

Propose adding `Process(req, fn)` to `protogen.Options` upstream in
`google.golang.org/protobuf`. This method would accept a
`CodeGeneratorRequest` and return a `CodeGeneratorResponse` — the same logic
as `Run()` minus the I/O. This is a small, backwards-compatible addition.

#### Option B: Provide a helper in protoc-go

If upstreaming is slow, `protoc-go` can ship a helper that reimplements the
`protogen` request→response flow:

```go
// package protoc

// RunProtogenPlugin wraps a protogen-style function as a Plugin.
func RunProtogenPlugin(fn func(*protogen.Plugin) error) Plugin {
    return PluginFunc(func(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
        // Construct a protogen.Plugin from the request,
        // run fn, and collect the response.
        ...
    })
}
```

This is feasible because `protogen.Plugin` construction from a
`CodeGeneratorRequest` is straightforward — it's what `Run()` does internally.

#### Option C: Stdin/stdout loopback (simplest, no upstream changes)

For plugins that only expose `Run()`, simulate the subprocess protocol
in-process using `io.Pipe`:

```go
func PluginFromRun(run func()) Plugin {
    return PluginFunc(func(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
        // Replace os.Stdin/os.Stdout with pipes
        // Call run() in a goroutine
        // Read the response from the pipe
    })
}
```

This is fragile (global state mutation) and should only be a last resort, but
it enables wrapping *any* existing plugin without code changes.

### End-to-end usage example

```go
package main

import (
    "log"
    "os"

    "github.com/wham/protoc-go/protoc"
    protocgengo "google.golang.org/protobuf/cmd/protoc-gen-go/internal_gengo"
)

func main() {
    c := protoc.New(
        protoc.WithProtoPaths("./protos"),
    )

    result, err := c.Compile("api/v1/service.proto")
    if err != nil {
        log.Fatal(err)
    }

    // In-process plugin — no subprocess, no binary on PATH
    goPlugin := protoc.RunProtogenPlugin(protocgengo.GenerateFile)

    files, err := result.RunLibraryPlugin(goPlugin, "paths=source_relative")
    if err != nil {
        log.Fatal(err)
    }

    for _, f := range files {
        os.WriteFile(f.Name, []byte(f.Content), 0644)
    }
}
```

**Zero subprocesses. Single Go binary. Full protobuf compilation + code generation.**

### CLI integration (optional, future)

The CLI could also support registered library plugins via a plugin registry:

```go
// Register in-process plugins before calling Run
cli.RegisterPlugin("go", goPlugin)
cli.RegisterPlugin("go-grpc", grpcPlugin)

// These are now used instead of searching PATH for protoc-gen-go
cli.Run(os.Args)
```

This would let developers build custom `protoc-go` binaries with plugins
baked in — useful for distributing a single "batteries-included" binary.

## Implementation plan

1. **Add `Plugin` interface and `PluginFunc` to `protoc/protoc.go`** — Pure
   type definitions, no behavior change.

2. **Add `RunLibraryPlugin` to `CompileResult`** — Near-copy of `RunPlugin`,
   calling `Plugin.Generate` instead of spawning a subprocess. Add to
   `compiler/cli/compile.go`.

3. **Add `RunProtogenPlugin` helper** — Bridge for plugins built with
   `protogen`. Implement Option B from above as a starting point.

4. **Add tests** — Test with a trivial in-process plugin (e.g., one that
   echoes file names). Then test with a real plugin like `protoc-gen-go` if
   its internals are accessible.

5. **Document in `protoc/protoc.go` package docs** — Update the library usage
   examples.

6. **(Future) CLI plugin registry** — Wire up registered plugins in
   `compiler/cli/cli.go` as an alternative to subprocess invocation.

## Compatibility

- **No breaking changes.** `RunPlugin` (subprocess) continues to work exactly
  as before. `RunLibraryPlugin` is a new method.
- **Same protocol.** Library plugins receive the identical
  `CodeGeneratorRequest` that subprocess plugins receive. This means any
  plugin can be tested in both modes and the output should be byte-identical.
- **Thread safety preserved.** `Plugin.Generate` is called synchronously on
  the caller's goroutine. The `CompileResult` is read-only, so concurrent
  `RunLibraryPlugin` calls are safe (assuming the plugin itself is safe).

## Open questions

1. **Should `Plugin` be in `protoc/` or a separate `protoc/plugin/` package?**
   Keeping it in `protoc/` is simpler and avoids an extra import for users.

2. **Naming: `RunLibraryPlugin` vs `RunInProcessPlugin` vs `Generate`?**
   `RunLibraryPlugin` parallels `RunPlugin` and makes the distinction clear.

3. **Should we pursue upstreaming `protogen.Options.Process()`?**
   This would give the cleanest integration, but we shouldn't block on it.
   Option B (helper in protoc-go) is a good starting point.
