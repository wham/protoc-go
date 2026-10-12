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

Last verified 2026-10-12 · commit `9b332b3` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/38215846848)

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
| startup_empty | descriptor | 1.14±0.03 | 1.56±0.06 | 20.70±0.32 | 1.37 | 5.0 | 6.8 | 46.1 | 1.35 |
| startup_empty | plugin | 3.05±3.47 | 3.52±0.05 | 23.12±2.82 | 1.15 | 5.5 | 8.7 | 48.0 | 1.58 |
| 01_basic_message | descriptor | 1.29±0.04 | 1.73±0.07 | 21.64±0.41 | 1.34 | 5.0 | 7.1 | 46.7 | 1.41 |
| 01_basic_message | plugin | 3.34±0.06 | 3.99±0.09 | 24.61±0.43 | 1.19 | 5.5 | 7.1 | 48.5 | 1.28 |
| bench_tiny | descriptor | 2.24±0.03 | 2.31±0.07 | 24.65±0.32 | 1.03 | 5.5 | 9.5 | 48.7 | 1.71 |
| bench_tiny | plugin | 5.83±0.08 | 5.70±3.36 | 29.04±0.18 | 0.98 | 7.6 | 9.5 | 46.9 | 1.26 |
| bench_small | descriptor | 10.82±0.48 | 4.86±0.17 | 55.49±0.75 | 0.45 | 10.3 | 13.6 | 57.2 | 1.32 |
| bench_small | plugin | 34.00±0.67 | 23.13±0.27 | 74.25±0.76 | 0.68 | 18.9 | 19.0 | 57.2 | 1.00 |
| bench_medium | descriptor | 58.99±1.30 | 17.96±0.52 | 207.34±2.91 | 0.30 | 34.6 | 31.7 | 96.4 | 0.92 |
| bench_medium | plugin | 193.05±3.26 | 109.09±1.56 | 302.50±3.16 | 0.57 | 72.1 | 72.1 | 98.3 | 1.00 |
| bench_large | descriptor | 373.50±7.05 | 81.01±1.70 | 921.54±9.22 | 0.22 | 138.8 | 108.6 | 279.0 | 0.78 |
| bench_large | plugin | 988.23±20.89 | 482.51±24.31 | 1340.26±28.23 | 0.49 | 311.4 | 313.5 | 313.3 | 1.01 |
| 329_large_stress | descriptor | 59.61±2.07 | 17.64±0.27 | 206.67±2.74 | 0.30 | 34.6 | 31.7 | 96.1 | 0.92 |
| 329_large_stress | plugin | 194.25±2.42 | 108.01±2.28 | 307.16±26.12 | 0.56 | 74.1 | 74.0 | 100.5 | 1.00 |
| bench_multi | descriptor | 41.02±1.08 | 12.46±0.24 | 120.19±3.00 | 0.30 | 20.1 | 29.6 | 88.5 | 1.47 |
| bench_multi | plugin | 137.20±1.97 | 75.95±11.25 | 188.34±1.79 | 0.55 | 53.8 | 53.9 | 88.6 | 1.00 |
| google_corpus | descriptor | 70.94±1.38 | 25.02±0.53 | n/a | 0.35 | 17.5 | 41.0 | n/a | 2.34 |
| google_corpus | plugin | 159.19±3.38 | 73.55±1.87 | n/a | 0.46 | 45.7 | 43.8 | n/a | 0.96 |

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
