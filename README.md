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

Last verified 2026-08-30 · commit `187d7e8` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/33296147848)

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
| startup_empty | descriptor | 1.39±0.04 | 1.91±0.06 | 27.52±1.07 | 1.37 | 5.0 | 6.8 | 46.1 | 1.36 |
| startup_empty | plugin | 3.26±0.06 | 4.07±0.22 | 29.39±0.70 | 1.25 | 5.5 | 7.0 | 46.3 | 1.26 |
| 01_basic_message | descriptor | 1.52±0.01 | 2.27±0.06 | 27.94±0.75 | 1.49 | 5.0 | 9.1 | 48.6 | 1.81 |
| 01_basic_message | plugin | 3.68±0.04 | 4.63±0.13 | 31.92±0.76 | 1.26 | 5.5 | 9.1 | 48.5 | 1.64 |
| bench_tiny | descriptor | 2.74±0.01 | 2.78±0.06 | 31.74±0.44 | 1.01 | 5.5 | 9.4 | 46.4 | 1.69 |
| bench_tiny | plugin | 6.84±0.21 | 6.88±0.12 | 36.76±0.95 | 1.01 | 7.6 | 9.5 | 48.6 | 1.26 |
| bench_small | descriptor | 13.65±0.15 | 6.15±0.18 | 72.14±0.86 | 0.45 | 10.3 | 13.4 | 55.0 | 1.31 |
| bench_small | plugin | 43.93±0.86 | 30.71±0.81 | 98.44±1.30 | 0.70 | 19.0 | 18.9 | 55.3 | 1.00 |
| bench_medium | descriptor | 75.02±2.10 | 22.27±0.54 | 277.89±4.08 | 0.30 | 34.6 | 31.7 | 92.2 | 0.92 |
| bench_medium | plugin | 247.01±4.41 | 148.59±3.59 | 412.68±2.63 | 0.60 | 74.1 | 74.0 | 98.2 | 1.00 |
| bench_large | descriptor | 419.76±7.09 | 96.33±1.39 | 1208.33±22.67 | 0.23 | 138.8 | 106.6 | 281.1 | 0.77 |
| bench_large | plugin | 1189.67±11.79 | 645.11±7.01 | 1792.75±34.11 | 0.54 | 313.1 | 313.2 | 309.6 | 1.00 |
| 329_large_stress | descriptor | 75.57±1.41 | 22.26±0.35 | 277.85±2.21 | 0.29 | 34.6 | 31.7 | 96.2 | 0.92 |
| 329_large_stress | plugin | 247.87±4.07 | 148.51±4.12 | 411.62±2.78 | 0.60 | 74.1 | 74.2 | 98.8 | 1.00 |
| bench_multi | descriptor | 51.99±1.32 | 15.57±0.22 | 158.37±0.94 | 0.30 | 20.1 | 29.6 | 86.3 | 1.47 |
| bench_multi | plugin | 174.58±1.59 | 103.43±1.38 | 253.37±1.81 | 0.59 | 53.6 | 53.7 | 90.6 | 1.00 |
| google_corpus | descriptor | 90.22±0.52 | 31.74±0.39 | n/a | 0.35 | 17.5 | 40.9 | n/a | 2.34 |
| google_corpus | plugin | 204.39±1.96 | 97.06±2.00 | n/a | 0.47 | 43.8 | 43.8 | n/a | 1.00 |

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
