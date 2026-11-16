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

In-memory sources, `FileDescriptorSet` output, in-process Go plugins and
concurrency guarantees are all in the reference:

[![Go Reference](https://pkg.go.dev/badge/github.com/wham/protoc-go.svg)](https://pkg.go.dev/github.com/wham/protoc-go/protoc)

## Compliance

A [weekly run](.github/workflows/compliance.yml) compiles the same corpus with C++
`protoc` and with protoc-go and compares the output byte for byte.

<!-- BEGIN COMPLIANCE -->
**5609 / 5609 comparisons produce byte-identical output to C++ protoc 36.2**

Last verified 2026-11-16 · commit `9c90598` · Go 1.23.12 on ubuntu26 · [run log](https://github.com/wham/protoc-go/actions/runs/43818858907)

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

<details><summary>Performance: C++ protoc vs Go protoc-go vs buf</summary>

Across 16 compile cases: Go faster on 12, C++ faster on 2, tie on 2.

| case | variant | cpp ms(±sd) | go ms(±sd) | buf ms(±sd) | go/cpp | cpp peak MB | go peak MB | buf peak MB | go/cpp |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| startup_empty | descriptor | 1.00±0.02 | 1.70±0.04 | 20.92±0.14 | 1.70 | 5.1 | 5.5 | 43.2 | 1.09 |
| startup_empty | plugin | 3.07±0.06 | 3.97±2.10 | 23.46±41.45 | 1.29 | 5.1 | 5.6 | 44.2 | 1.11 |
| 01_basic_message | descriptor | 1.16±0.02 | 1.93±0.06 | 22.54±2.61 | 1.66 | 4.9 | 6.2 | 44.9 | 1.25 |
| 01_basic_message | plugin | 9.17±10.69 | 10.69±9.58 | 40.52±27.23 | 1.17 | 4.9 | 6.3 | 44.7 | 1.28 |
| bench_tiny | descriptor | 1.92±0.16 | 2.48±0.09 | 25.29±0.70 | 1.29 | 5.4 | 6.7 | 44.2 | 1.23 |
| bench_tiny | plugin | 6.04±1.68 | 29.74±35.94 | 29.78±1.91 | 4.92 | 5.8 | 7.1 | 45.0 | 1.21 |
| bench_small | descriptor | 8.34±0.04 | 4.68±0.11 | 51.53±10.59 | 0.56 | 10.2 | 8.0 | 51.4 | 0.79 |
| bench_small | plugin | 32.52±1.27 | 25.97±0.70 | 70.35±2.04 | 0.80 | 16.7 | 16.3 | 52.5 | 0.98 |
| bench_medium | descriptor | 54.29±13.56 | 17.72±0.29 | 199.70±1.99 | 0.33 | 34.5 | 14.9 | 91.8 | 0.43 |
| bench_medium | plugin | 192.16±6.81 | 129.28±6.23 | 295.62±7.13 | 0.67 | 71.2 | 71.3 | 96.5 | 1.00 |
| bench_large | descriptor | 344.23±21.01 | 74.91±2.08 | 823.47±17.55 | 0.22 | 138.7 | 42.8 | 264.1 | 0.31 |
| bench_large | plugin | 972.36±114.05 | 556.61±16.27 | 1293.31±31.41 | 0.57 | 307.4 | 309.2 | 307.3 | 1.01 |
| 329_large_stress | descriptor | 45.21±1.21 | 16.75±0.21 | 180.39±8.42 | 0.37 | 34.5 | 14.9 | 91.8 | 0.43 |
| 329_large_stress | plugin | 187.67±2.44 | 124.53±15.55 | 291.25±17.08 | 0.66 | 71.0 | 71.1 | 96.2 | 1.00 |
| bench_multi | descriptor | 30.30±0.80 | 10.47±0.12 | 106.08±2.17 | 0.35 | 20.0 | 14.4 | 82.2 | 0.72 |
| bench_multi | plugin | 130.02±3.75 | 85.16±1.09 | 180.36±2.57 | 0.65 | 51.7 | 51.8 | 85.6 | 1.00 |
| google_corpus | descriptor | 53.58±1.24 | 18.16±0.47 | n/a | 0.34 | 17.4 | 20.4 | n/a | 1.17 |
| google_corpus | plugin | 141.35±13.51 | 77.74±1.90 | n/a | 0.55 | 41.0 | 40.8 | n/a | 0.99 |

buf was not timed on some rows:

- `google_corpus` / `descriptor`: google/protobuf/edition_unittest.proto:17:11:unrecognized `edition` declaration value
- `google_corpus` / `plugin`: google/protobuf/edition_unittest.proto:17:11:unrecognized `edition` declaration value

</details>
<!-- END COMPLIANCE -->

## Versioning

protoc-go has its own version, separate from the C++ protoc release it mirrors.
The two numbers answer different questions:

- **`protoc-go --version`** prints `libprotoc <upstream>`, the C++ release this
  build reproduces. Tooling parses that string and expects protoc's answer, so
  that is what it gets.
- **The Go module version** (`v0.x.y`) describes this project's own API and
  fixes, and follows semver. `protoc-go --protoc_go_version` prints it alongside
  the upstream one, e.g. `protoc-go v0.1.0 (libprotoc 36.0)`.

So `protoc-go v0.4.0` may report `libprotoc 36.0`. We don't renumber the module to
match upstream: protoc majors land about once a year, and chasing them would push
a new import path (`/v33`, `/v34`, …) on everyone for a release with none of our
changes in it. The compliance table above is what says which protoc we match.

## Development

```bash
scripts/test            # compare Go protoc-go output against C++ protoc
scripts/bench           # performance comparison: C++ protoc vs protoc-go vs buf
```

Requires Go 1.23+, plus a C++ `protoc` on your PATH for the comparison suites (e.g. `brew install protobuf`). `scripts/bench` also times [buf](https://github.com/bufbuild/buf) when it is installed; buf is a performance reference point only, never a correctness target.

Releases are cut by a `release: major|minor|patch|none` label on the pull
request, so a change nobody can observe never mints a version.
