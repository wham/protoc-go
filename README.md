# protoc-go

[![compliance](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fwham%2Fprotoc-go%2Fmain%2Fdocs%2Fbadge.json)](#compliance)
[![tests](https://github.com/wham/protoc-go/actions/workflows/tests.yml/badge.svg)](https://github.com/wham/protoc-go/actions/workflows/tests.yml)

A pure Go implementation of the Protocol Buffers compiler (`protoc`). Use it as a CLI drop-in replacement, or embed it as a library directly in your Go program, with no `protoc` binary and no subprocess.

protoc-go is the compiler behind [**Kaja**](https://github.com/wham/kaja). Kaja needed a pure-Go `protoc` so it could embed the compiler directly into its binary, which also made it much easier to pass Apple App Store review.

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

That is the whole idea. In-memory sources, `FileDescriptorSet` output, in-process
Go plugins and the concurrency guarantees are all in the reference:

[![Go Reference](https://pkg.go.dev/badge/github.com/wham/protoc-go.svg)](https://pkg.go.dev/github.com/wham/protoc-go/protoc)

## Compliance

A [weekly run](.github/workflows/compliance.yml) feeds the same `.proto` corpus to
the real C++ `protoc` and to protoc-go, then compares what each one produces byte
for byte: the `CodeGeneratorRequest` sent to plugins, the serialized
`FileDescriptorSet`, stdout, stderr and exit codes. The results below are written
by that run.

<!-- BEGIN COMPLIANCE -->
**5497 / 5497 comparisons produce byte-identical output to C++ protoc 33.4**

Last verified 2026-08-16 · commit `7e132a9` · Go 1.24.7 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/31973401049)

<details><summary>Per-suite results</summary>

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

</details>

<details><summary>Performance vs C++ protoc</summary>

Across 12 compile cases: Go faster on 6, C++ faster on 2, tie on 4. A gap smaller than both the run-to-run noise and the tie margin counts as a tie, not a win.

| case | variant | C++ ms | Go ms | go/cpp | verdict |
| --- | --- | ---: | ---: | ---: | --- |
| startup_empty | descriptor | 6.00±0.47 | 7.00±0.57 | 1.17 | informational |
| startup_empty | plugin | 8.00±0.57 | 9.00±17.68 | 1.12 | informational |
| 01_basic_message | descriptor | 6.00±0.42 | 7.00±0.32 | 1.17 | cpp |
| 01_basic_message | plugin | 9.00±0.42 | 10.00±0.74 | 1.11 | tie |
| bench_tiny | descriptor | 7.00±0.42 | 8.00±0.32 | 1.14 | cpp |
| bench_tiny | plugin | 17.00±111.43 | 17.00±24.84 | 1.00 | tie |
| bench_small | descriptor | 16.00±0.53 | 13.00±0.52 | 0.81 | go |
| bench_small | plugin | 142.00±2.95 | 130.00±4.52 | 0.92 | go |
| bench_medium | descriptor | 72.00±2.21 | 39.00±1.45 | 0.54 | go |
| bench_medium | plugin | 2278.00±34.41 | 2279.00±131.51 | 1.00 | tie |
| bench_large | descriptor | 376.00±11.69 | 156.00±2.30 | 0.41 | go |
| bench_large | plugin | 30758.00±518.56 | 30008.00±490.12 | 0.98 | tie |
| 329_large_stress | descriptor | 69.00±1.77 | 36.00±1.16 | 0.52 | go |
| 329_large_stress | plugin | 2280.00±33.60 | 2145.00±34.99 | 0.94 | go |

`startup_empty` is process launch cost, not compilation, and is excluded
from the tally. Timings come from the run above, on a shared machine, so
read them as a trend, not a lab result.

</details>
<!-- END COMPLIANCE -->

## Versioning

protoc-go has its own version, separate from the C++ protoc release it mirrors.
The two numbers answer different questions:

- **`protoc-go --version`** prints `libprotoc <upstream>`, the C++ release this
  build reproduces. Tooling parses that string and expects protoc's answer, so
  it has to be protoc's answer.
- **The Go module version** (`v0.x.y`) describes this project's own API and
  fixes, and follows semver. `protoc-go --protoc_go_version` prints it alongside
  the upstream one, e.g. `protoc-go v0.1.0 (libprotoc 36.0)`.

So `protoc-go v0.4.0` may report `libprotoc 36.0`: our release, verified against
that C++ release. We deliberately don't renumber the module to match upstream.
protoc majors land roughly yearly, and following them would force a new import
path (`/v33`, `/v34`, …) on everyone for releases containing none of our changes.
The compliance table above is the compatibility claim; the version number never
was one.

Releases are cut by a `release: major|minor|patch|none` label on the pull
request, so a change nobody can observe never mints a version. Unlabelled work
rides along in the next release that happens. Every tag re-runs the comparison
suite on the tree it points at, and moving the mirrored protoc release is always
at least a minor.

## Development

```bash
scripts/test            # compare Go protoc-go output against C++ protoc
scripts/bench           # performance comparison
```

Requires Go 1.23+, plus a C++ `protoc` on your PATH for the comparison suites (e.g. `brew install protobuf`).
