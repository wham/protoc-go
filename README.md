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

Last verified 2026-09-21 · commit `9b332b3` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/35570421367)

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
| startup_empty | descriptor | 1.33±0.02 | 1.83±0.08 | 26.34±0.51 | 1.38 | 5.0 | 6.8 | 46.0 | 1.35 |
| startup_empty | plugin | 3.29±0.09 | 3.96±0.09 | 29.39±0.97 | 1.20 | 5.5 | 6.8 | 47.9 | 1.23 |
| 01_basic_message | descriptor | 1.54±0.04 | 2.29±0.16 | 28.32±0.57 | 1.49 | 5.0 | 7.1 | 48.3 | 1.41 |
| 01_basic_message | plugin | 3.77±0.10 | 4.59±0.17 | 31.64±1.01 | 1.22 | 7.5 | 7.2 | 48.0 | 0.95 |
| bench_tiny | descriptor | 2.81±0.04 | 2.86±0.18 | 32.75±0.62 | 1.02 | 5.5 | 9.5 | 48.6 | 1.72 |
| bench_tiny | plugin | 6.96±0.55 | 7.12±0.27 | 37.37±0.57 | 1.02 | 7.6 | 9.5 | 48.2 | 1.26 |
| bench_small | descriptor | 13.79±0.30 | 6.53±0.36 | 72.39±0.63 | 0.47 | 10.3 | 11.6 | 55.0 | 1.13 |
| bench_small | plugin | 45.44±1.09 | 31.47±0.63 | 100.34±1.15 | 0.69 | 18.9 | 19.0 | 55.3 | 1.00 |
| bench_medium | descriptor | 77.57±4.23 | 24.00±0.47 | 283.21±2.03 | 0.31 | 34.6 | 31.7 | 94.1 | 0.92 |
| bench_medium | plugin | 251.43±1.92 | 150.73±4.21 | 418.93±5.45 | 0.60 | 74.0 | 73.9 | 98.5 | 1.00 |
| bench_large | descriptor | 424.50±14.33 | 102.57±1.79 | 1212.46±22.84 | 0.24 | 138.7 | 108.7 | 276.5 | 0.78 |
| bench_large | plugin | 1225.75±12.68 | 657.84±10.78 | 1808.55±18.40 | 0.54 | 311.4 | 313.6 | 313.2 | 1.01 |
| 329_large_stress | descriptor | 74.30±0.88 | 23.35±0.38 | 279.69±2.36 | 0.31 | 34.6 | 31.8 | 94.3 | 0.92 |
| 329_large_stress | plugin | 248.56±4.32 | 150.76±6.48 | 413.28±3.56 | 0.61 | 74.1 | 73.8 | 90.1 | 1.00 |
| bench_multi | descriptor | 53.45±1.18 | 16.16±0.16 | 162.51±1.89 | 0.30 | 20.1 | 29.6 | 82.3 | 1.48 |
| bench_multi | plugin | 176.37±2.64 | 106.14±2.22 | 256.05±3.67 | 0.60 | 53.7 | 53.8 | 90.9 | 1.00 |
| google_corpus | descriptor | 90.21±0.57 | 33.67±0.46 | n/a | 0.37 | 17.5 | 40.9 | n/a | 2.34 |
| google_corpus | plugin | 204.93±1.38 | 97.29±2.19 | n/a | 0.47 | 43.8 | 43.8 | n/a | 1.00 |

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
