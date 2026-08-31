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

Last verified 2026-08-31 · commit `933db0c` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/33365832600)

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
| startup_empty | descriptor | 1.49±0.03 | 2.03±0.07 | 27.99±0.59 | 1.36 | 5.0 | 6.8 | 46.3 | 1.36 |
| startup_empty | plugin | 3.67±0.09 | 4.75±0.41 | 31.15±0.78 | 1.29 | 5.5 | 8.9 | 46.1 | 1.60 |
| 01_basic_message | descriptor | 1.58±0.03 | 2.33±0.05 | 29.19±0.65 | 1.47 | 5.0 | 9.1 | 48.1 | 1.81 |
| 01_basic_message | plugin | 4.10±0.10 | 5.30±0.10 | 32.41±0.56 | 1.29 | 5.5 | 9.1 | 48.8 | 1.64 |
| bench_tiny | descriptor | 2.88±0.06 | 3.04±0.29 | 32.74±0.67 | 1.06 | 5.5 | 9.5 | 48.5 | 1.72 |
| bench_tiny | plugin | 7.40±0.21 | 7.22±0.22 | 37.36±0.42 | 0.98 | 7.6 | 9.5 | 48.4 | 1.25 |
| bench_small | descriptor | 14.43±0.68 | 6.19±0.22 | 70.51±0.64 | 0.43 | 10.3 | 13.5 | 55.2 | 1.32 |
| bench_small | plugin | 44.57±0.79 | 29.58±0.94 | 96.48±7.22 | 0.66 | 18.9 | 18.9 | 57.4 | 1.00 |
| bench_medium | descriptor | 77.89±1.51 | 21.33±0.45 | 268.60±6.33 | 0.27 | 34.6 | 31.5 | 90.3 | 0.91 |
| bench_medium | plugin | 241.69±3.58 | 137.26±3.41 | 393.90±4.28 | 0.57 | 72.2 | 74.1 | 96.7 | 1.03 |
| bench_large | descriptor | 445.51±10.43 | 95.90±4.79 | 1189.24±19.03 | 0.22 | 138.8 | 106.6 | 272.6 | 0.77 |
| bench_large | plugin | 1258.92±13.99 | 609.81±11.82 | 1724.28±21.79 | 0.48 | 309.4 | 315.4 | 315.2 | 1.02 |
| 329_large_stress | descriptor | 82.17±1.65 | 21.69±0.47 | 278.46±8.60 | 0.26 | 34.6 | 31.7 | 96.3 | 0.92 |
| 329_large_stress | plugin | 256.94±11.93 | 139.08±3.86 | 401.85±4.43 | 0.54 | 72.0 | 74.2 | 90.3 | 1.03 |
| bench_multi | descriptor | 54.21±0.64 | 15.42±0.28 | 165.90±3.53 | 0.28 | 20.0 | 29.5 | 87.9 | 1.48 |
| bench_multi | plugin | 176.91±2.71 | 99.13±2.10 | 248.61±3.45 | 0.56 | 53.8 | 53.8 | 88.4 | 1.00 |
| google_corpus | descriptor | 92.34±0.45 | 31.88±0.45 | n/a | 0.35 | 17.5 | 40.9 | n/a | 2.34 |
| google_corpus | plugin | 206.43±2.59 | 95.33±2.27 | n/a | 0.46 | 43.8 | 45.9 | n/a | 1.05 |

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
