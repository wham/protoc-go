# protoc-go

[![compliance](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fwham%2Fprotoc-go%2Fmain%2Fdocs%2Fbadge.json)](#compliance)
[![tests](https://github.com/wham/protoc-go/actions/workflows/tests.yml/badge.svg)](https://github.com/wham/protoc-go/actions/workflows/tests.yml)

> **Warning** — experimental and under active development. Not ready for production use; APIs may change without notice.

A pure Go implementation of the Protocol Buffers compiler (`protoc`). Use it as a CLI drop-in replacement, or embed it as a library directly in your Go program — no `protoc` binary, no subprocess.

protoc-go is the compiler behind [**Kaja**](https://github.com/wham/kaja). Kaja needed a pure-Go `protoc` so it could embed the compiler directly into its binary — which also made it much easier to pass Apple App Store review.

## Compliance

Every week, a [scheduled run](.github/workflows/compliance.yml) feeds the same
`.proto` corpus to the real C++ `protoc` and to protoc-go, and compares what
each one produces — the `CodeGeneratorRequest` sent to plugins, the serialized
`FileDescriptorSet`, stdout, stderr, and exit codes, byte for byte. The results
below are written by that run; nothing here is typed by hand.

<!-- BEGIN COMPLIANCE -->
**5497 / 5497 comparisons produce byte-identical output to C++ protoc 33.4**

Last verified 2026-08-16 · commit `eaf7803` · Go 1.24.7 on linux-x86_64

| suite | comparisons | result |
| --- | ---: | --- |
| `cli` | 126 | all match |
| `colon_param` | 532 | all match |
| `decode` | 27 | all match |
| `descriptor_set` | 532 | all match |
| `descriptor_set_full` | 532 | all match |
| `descriptor_set_retain` | 532 | all match |
| `descriptor_set_src` | 532 | all match |
| `determinism` | 14 | all match |
| `multi_opt` | 532 | all match |
| `multi_plugin` | 532 | all match |
| `partial` | 2 | all match |
| `plugin` | 532 | all match |
| `plugin_descriptor` | 532 | all match |
| `plugin_param` | 532 | all match |
| `stdin` | 8 | all match |

<details><summary>Performance vs C++ protoc</summary>

Across 10 compile cases: Go faster on 2, C++ faster on 1, tie on 7. A gap smaller than both the run-to-run noise and the tie margin counts as a tie, not a win.

| case | variant | C++ ms | Go ms | go/cpp | verdict |
| --- | --- | ---: | ---: | ---: | --- |
| startup_empty | descriptor | 8.00±0.63 | 9.00±0.70 | 1.12 | informational |
| startup_empty | plugin | 11.00±0.53 | 12.00±0.48 | 1.09 | informational |
| 01_basic_message | descriptor | 8.00±0.52 | 9.00±0.53 | 1.12 | tie |
| 01_basic_message | plugin | 12.00±0.47 | 14.00±0.52 | 1.17 | cpp |
| bench_tiny | descriptor | 10.00±0.95 | 11.00±3.20 | 1.10 | tie |
| bench_tiny | plugin | 24.00±1.07 | 24.00±0.88 | 1.00 | tie |
| bench_small | descriptor | 20.00±1.10 | 19.00±0.97 | 0.95 | tie |
| bench_small | plugin | 250.00±17.71 | 228.00±16.50 | 0.91 | tie |
| bench_medium | descriptor | 100.00±2.88 | 64.00±1.65 | 0.64 | go |
| bench_medium | plugin | 3144.00±105.20 | 3112.00±93.64 | 0.99 | tie |
| 329_large_stress | descriptor | 103.00±4.08 | 63.00±2.23 | 0.61 | go |
| 329_large_stress | plugin | 3166.00±118.68 | 2983.00±127.44 | 0.94 | tie |

`startup_empty` is process launch cost, not compilation, and is excluded
from the tally. Timings come from the run above, on a shared machine —
read them as a trend, not a lab result.

</details>
<!-- END COMPLIANCE -->

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

## Versioning

protoc-go has its own version, separate from the C++ protoc release it mirrors.
The two numbers answer different questions:

- **`protoc-go --version`** prints `libprotoc <upstream>` — the C++ release this
  build reproduces. Tooling parses that string and expects protoc's answer, so
  it has to be protoc's answer.
- **The Go module version** (`v0.x.y`) describes this project's own API and
  fixes, and follows semver.

So `protoc-go v0.4.0` may report `libprotoc 33.4`: our release, verified against
that C++ release. We deliberately don't renumber the module to match upstream —
protoc majors land roughly yearly, and following them would force a new import
path (`/v33`, `/v34`, …) on everyone for releases containing none of our changes.
The compliance table above is the compatibility claim; the version number never
was one.

## Build & test

```bash
scripts/test            # compare Go protoc-go output against C++ protoc
scripts/bench           # performance comparison
```

Requires Go 1.23+, plus a C++ `protoc` on your PATH for the comparison suites (e.g. `brew install protobuf`).

See [AGENTS.md](AGENTS.md) for the architecture, how the test/bench harnesses work, and the automated development loop.
