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
**5504 / 5504 comparisons produce byte-identical output to C++ protoc 36.0**

Last verified 2026-08-27 · commit `62c9ab8` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/33046484651)

<details><summary>Per-suite results</summary>

| suite | comparisons | result |
| --- | ---: | --- |
| `cli` | 133 | all match |
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

Across 12 compile cases: Go faster on 5, C++ faster on 1, tie on 6. A gap smaller than both the run-to-run noise and the tie margin counts as a tie, not a win.

| case | variant | C++ ms | Go ms | go/cpp | verdict |
| --- | --- | ---: | ---: | ---: | --- |
| startup_empty | descriptor | 7.00±0.32 | 8.00±0.57 | 1.14 | informational |
| startup_empty | plugin | 9.00±0.42 | 10.00±0.48 | 1.11 | informational |
| 01_basic_message | descriptor | 7.00±0.53 | 8.00±0.42 | 1.14 | cpp |
| 01_basic_message | plugin | 53.00±54.21 | 33.00±16.85 | 0.62 | tie |
| bench_tiny | descriptor | 9.00±0.52 | 9.00±0.48 | 1.00 | tie |
| bench_tiny | plugin | 133.00±52.41 | 88.00±81.70 | 0.66 | tie |
| bench_small | descriptor | 18.00±0.00 | 13.00±0.53 | 0.72 | go |
| bench_small | plugin | 156.00±56.17 | 144.00±3.78 | 0.92 | tie |
| bench_medium | descriptor | 81.00±2.58 | 47.00±1.58 | 0.58 | go |
| bench_medium | plugin | 2610.00±58.62 | 2534.00±52.84 | 0.97 | tie |
| bench_large | descriptor | 434.00±8.85 | 179.00±0.85 | 0.41 | go |
| bench_large | plugin | 43814.00±309.66 | 43297.00±259.41 | 0.99 | tie |
| 329_large_stress | descriptor | 81.00±1.84 | 48.00±1.06 | 0.59 | go |
| 329_large_stress | plugin | 2712.00±83.77 | 2562.00±32.47 | 0.94 | go |

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
