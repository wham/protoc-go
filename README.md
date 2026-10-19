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

Last verified 2026-10-19 · commit `9b332b3` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/39212772391)

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
| startup_empty | descriptor | 1.35±0.03 | 1.96±0.06 | 26.32±1.00 | 1.45 | 5.0 | 6.7 | 47.7 | 1.34 |
| startup_empty | plugin | 3.53±0.18 | 4.26±0.35 | 29.55±1.06 | 1.21 | 5.5 | 6.8 | 47.9 | 1.23 |
| 01_basic_message | descriptor | 1.53±0.03 | 2.16±0.13 | 27.76±1.16 | 1.41 | 5.0 | 7.2 | 48.3 | 1.42 |
| 01_basic_message | plugin | 4.11±0.25 | 4.72±0.14 | 31.31±0.92 | 1.15 | 5.5 | 7.1 | 48.3 | 1.28 |
| bench_tiny | descriptor | 2.75±0.05 | 2.76±0.15 | 32.08±0.66 | 1.00 | 5.5 | 9.5 | 48.2 | 1.72 |
| bench_tiny | plugin | 7.03±0.63 | 7.22±0.19 | 38.08±1.28 | 1.03 | 7.6 | 9.5 | 49.0 | 1.26 |
| bench_small | descriptor | 13.68±0.72 | 6.29±0.34 | 72.75±1.63 | 0.46 | 10.3 | 11.6 | 55.5 | 1.13 |
| bench_small | plugin | 43.99±0.58 | 31.77±1.25 | 99.05±1.04 | 0.72 | 18.9 | 18.9 | 55.2 | 1.00 |
| bench_medium | descriptor | 73.63±1.32 | 23.62±0.59 | 278.62±3.30 | 0.32 | 34.6 | 31.8 | 88.1 | 0.92 |
| bench_medium | plugin | 246.27±49.49 | 150.54±3.16 | 412.36±2.78 | 0.61 | 73.9 | 74.0 | 90.1 | 1.00 |
| bench_large | descriptor | 406.48±3.91 | 100.45±1.68 | 1197.31±7.64 | 0.25 | 138.7 | 108.7 | 278.6 | 0.78 |
| bench_large | plugin | 1181.75±9.39 | 647.97±3.87 | 1793.17±26.27 | 0.55 | 315.2 | 315.1 | 317.3 | 1.00 |
| 329_large_stress | descriptor | 73.69±1.33 | 23.31±0.30 | 278.61±1.16 | 0.32 | 34.6 | 31.7 | 94.4 | 0.92 |
| 329_large_stress | plugin | 245.82±3.74 | 150.68±3.25 | 409.46±6.27 | 0.61 | 74.1 | 72.2 | 100.2 | 0.97 |
| bench_multi | descriptor | 50.84±0.31 | 16.21±1.30 | 159.18±1.51 | 0.32 | 20.1 | 29.6 | 88.6 | 1.48 |
| bench_multi | plugin | 173.21±1.93 | 105.43±0.81 | 252.59±0.89 | 0.61 | 53.7 | 53.8 | 90.3 | 1.00 |
| google_corpus | descriptor | 89.77±0.75 | 33.51±1.13 | n/a | 0.37 | 17.5 | 40.9 | n/a | 2.34 |
| google_corpus | plugin | 202.93±1.96 | 96.73±1.29 | n/a | 0.48 | 43.9 | 41.8 | n/a | 0.95 |

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
