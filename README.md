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
**5609 / 5609 comparisons produce byte-identical output to C++ protoc 36.0**

Last verified 2026-09-28 · commit `9b332b3` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/36389140925)

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
| startup_empty | descriptor | 1.35±0.07 | 1.91±0.06 | 27.20±0.81 | 1.41 | 5.0 | 6.8 | 46.4 | 1.35 |
| startup_empty | plugin | 3.64±0.06 | 4.25±0.19 | 30.22±0.75 | 1.17 | 5.5 | 6.8 | 46.5 | 1.23 |
| 01_basic_message | descriptor | 1.53±0.02 | 2.13±0.08 | 28.48±0.71 | 1.39 | 5.0 | 7.2 | 48.2 | 1.42 |
| 01_basic_message | plugin | 4.02±0.05 | 4.85±0.18 | 32.26±0.64 | 1.21 | 5.5 | 7.1 | 48.8 | 1.28 |
| bench_tiny | descriptor | 2.72±0.08 | 2.80±0.09 | 32.63±0.59 | 1.03 | 5.5 | 9.4 | 48.7 | 1.71 |
| bench_tiny | plugin | 7.62±0.31 | 7.51±0.23 | 38.51±0.71 | 0.99 | 7.6 | 9.5 | 48.9 | 1.26 |
| bench_small | descriptor | 14.28±0.25 | 7.25±0.43 | 75.14±1.19 | 0.51 | 10.3 | 11.6 | 55.4 | 1.13 |
| bench_small | plugin | 45.08±0.67 | 31.59±0.90 | 100.47±1.88 | 0.70 | 18.9 | 18.9 | 57.2 | 1.00 |
| bench_medium | descriptor | 78.91±1.75 | 24.31±0.59 | 288.49±2.54 | 0.31 | 34.6 | 31.7 | 94.0 | 0.92 |
| bench_medium | plugin | 257.35±5.98 | 152.68±2.10 | 426.58±7.70 | 0.59 | 73.9 | 74.1 | 88.4 | 1.00 |
| bench_large | descriptor | 463.67±7.02 | 108.37±2.85 | 1233.01±9.16 | 0.23 | 138.8 | 108.7 | 275.1 | 0.78 |
| bench_large | plugin | 1271.51±14.04 | 669.73±9.38 | 1873.53±43.28 | 0.53 | 313.3 | 315.3 | 313.1 | 1.01 |
| 329_large_stress | descriptor | 79.79±1.03 | 24.24±0.39 | 291.84±4.12 | 0.30 | 34.6 | 31.8 | 96.3 | 0.92 |
| 329_large_stress | plugin | 253.43±9.16 | 151.38±1.73 | 419.03±4.20 | 0.60 | 72.3 | 73.9 | 92.1 | 1.02 |
| bench_multi | descriptor | 55.23±1.14 | 16.68±0.35 | 164.64±2.18 | 0.30 | 20.1 | 29.6 | 88.3 | 1.48 |
| bench_multi | plugin | 185.45±2.23 | 106.16±1.59 | 264.90±8.22 | 0.57 | 53.8 | 53.8 | 90.4 | 1.00 |
| google_corpus | descriptor | 90.78±1.00 | 33.44±0.46 | n/a | 0.37 | 17.5 | 41.3 | n/a | 2.36 |
| google_corpus | plugin | 212.00±3.06 | 98.15±2.60 | n/a | 0.46 | 43.7 | 43.9 | n/a | 1.00 |

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
