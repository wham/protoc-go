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

Last verified 2026-10-26 · commit `9b332b3` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/40282800116)

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

Across 16 compile cases: Go faster on 11, C++ faster on 2, tie on 3.

| case | variant | cpp ms(±sd) | go ms(±sd) | buf ms(±sd) | go/cpp | cpp peak MB | go peak MB | buf peak MB | go/cpp |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| startup_empty | descriptor | 1.44±0.03 | 1.96±0.08 | 27.18±0.43 | 1.36 | 5.0 | 6.8 | 45.8 | 1.35 |
| startup_empty | plugin | 3.78±0.08 | 4.38±0.10 | 30.14±0.58 | 1.16 | 5.5 | 6.8 | 48.1 | 1.23 |
| 01_basic_message | descriptor | 1.62±0.03 | 2.28±0.06 | 28.25±0.78 | 1.41 | 5.0 | 7.1 | 48.0 | 1.41 |
| 01_basic_message | plugin | 4.15±0.08 | 5.05±0.13 | 32.22±6.27 | 1.22 | 5.5 | 7.6 | 46.6 | 1.37 |
| bench_tiny | descriptor | 2.87±0.04 | 2.98±0.39 | 32.49±1.48 | 1.04 | 5.5 | 9.4 | 48.6 | 1.70 |
| bench_tiny | plugin | 7.34±0.17 | 7.27±0.23 | 37.42±0.58 | 0.99 | 7.6 | 9.4 | 46.6 | 1.25 |
| bench_small | descriptor | 13.85±0.13 | 6.40±0.25 | 70.66±1.98 | 0.46 | 10.3 | 11.6 | 55.5 | 1.13 |
| bench_small | plugin | 43.82±1.05 | 29.18±23.90 | 95.03±1.00 | 0.67 | 18.8 | 19.0 | 55.4 | 1.01 |
| bench_medium | descriptor | 77.04±1.41 | 23.77±0.50 | 270.19±4.13 | 0.31 | 34.6 | 31.7 | 96.2 | 0.92 |
| bench_medium | plugin | 244.51±2.87 | 140.63±4.44 | 400.74±7.36 | 0.58 | 73.9 | 74.1 | 96.5 | 1.00 |
| bench_large | descriptor | 453.28±4.78 | 101.57±1.78 | 1170.26±25.03 | 0.22 | 138.7 | 110.7 | 278.4 | 0.80 |
| bench_large | plugin | 1252.08±18.16 | 618.02±12.46 | 1716.49±14.21 | 0.49 | 315.3 | 313.3 | 313.6 | 0.99 |
| 329_large_stress | descriptor | 81.37±0.89 | 22.97±0.63 | 272.99±2.81 | 0.28 | 34.6 | 31.7 | 96.3 | 0.92 |
| 329_large_stress | plugin | 247.35±5.83 | 139.09±2.65 | 403.64±4.88 | 0.56 | 72.3 | 74.0 | 98.4 | 1.02 |
| bench_multi | descriptor | 53.35±0.42 | 15.85±0.17 | 159.31±1.85 | 0.30 | 20.1 | 29.6 | 88.0 | 1.47 |
| bench_multi | plugin | 176.56±2.97 | 98.88±0.77 | 248.15±2.79 | 0.56 | 53.6 | 53.8 | 90.1 | 1.00 |
| google_corpus | descriptor | 91.70±0.50 | 32.67±0.33 | n/a | 0.36 | 17.5 | 41.3 | n/a | 2.36 |
| google_corpus | plugin | 204.76±1.64 | 94.76±2.44 | n/a | 0.46 | 43.9 | 43.8 | n/a | 1.00 |

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
