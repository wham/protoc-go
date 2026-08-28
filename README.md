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
**5609 / 5609 comparisons produce byte-identical output to C++ protoc 36.0**

Last verified 2026-08-28 · commit `29f40e1` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/33144917190)

<details><summary>Per-suite results</summary>

| suite | comparisons | result |
| --- | ---: | --- |
| `cli` | 139 | all match |
| `colon_param` | 530 | all match |
| `decode` | 27 | all match |
| `descriptor_set` | 530 | all match |
| `descriptor_set_full` | 530 | all match |
| `descriptor_set_retain` | 530 | all match |
| `descriptor_set_src` | 530 | all match |
| `determinism` | 14 | all match |
| `google` | 106 | all match |
| `mock` | 12 | all match |
| `multi_opt` | 530 | all match |
| `multi_plugin` | 530 | all match |
| `partial` | 2 | all match |
| `pathplugin` | 1 | all match |
| `plugin` | 530 | all match |
| `plugin_descriptor` | 530 | all match |
| `plugin_param` | 530 | all match |
| `stdin` | 8 | all match |

</details>

<details><summary>Performance vs C++ protoc</summary>

Across 12 compile cases: Go faster on 6, C++ faster on 3, tie on 3. A gap smaller than both the run-to-run noise and the tie margin counts as a tie, not a win.

| case | variant | C++ ms | Go ms | go/cpp | verdict |
| --- | --- | ---: | ---: | ---: | --- |
| startup_empty | descriptor | 1.42±0.06 | 2.23±0.20 | 1.57 | informational |
| startup_empty | plugin | 3.92±0.11 | 4.81±0.22 | 1.23 | informational |
| 01_basic_message | descriptor | 1.63±0.03 | 2.63±0.19 | 1.61 | cpp |
| 01_basic_message | plugin | 5.10±0.21 | 6.06±0.24 | 1.19 | cpp |
| bench_tiny | descriptor | 2.84±0.11 | 3.14±0.15 | 1.11 | cpp |
| bench_tiny | plugin | 12.98±0.21 | 13.56±0.48 | 1.04 | tie |
| bench_small | descriptor | 14.67±0.32 | 8.65±0.33 | 0.59 | go |
| bench_small | plugin | 144.09±3.65 | 131.18±3.56 | 0.91 | go |
| bench_medium | descriptor | 78.70±2.29 | 41.31±2.60 | 0.52 | go |
| bench_medium | plugin | 2239.16±49.81 | 2193.71±40.12 | 0.98 | tie |
| bench_large | descriptor | 451.85±7.95 | 171.04±2.89 | 0.38 | go |
| bench_large | plugin | 16729.26±430.22 | 16178.54±252.86 | 0.97 | tie |
| 329_large_stress | descriptor | 76.52±8.91 | 40.42±1.80 | 0.53 | go |
| 329_large_stress | plugin | 2203.75±35.45 | 2107.93±37.51 | 0.96 | go |

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
