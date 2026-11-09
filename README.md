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

Last verified 2026-11-09 · commit `9b332b3` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/42574170594)

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

Across 16 compile cases: Go faster on 12, C++ faster on 3, tie on 1.

| case | variant | cpp ms(±sd) | go ms(±sd) | buf ms(±sd) | go/cpp | cpp peak MB | go peak MB | buf peak MB | go/cpp |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| startup_empty | descriptor | 1.39±0.05 | 2.38±0.08 | 28.65±1.00 | 1.71 | 5.1 | 5.6 | 42.6 | 1.10 |
| startup_empty | plugin | 4.57±0.05 | 5.83±1.35 | 32.88±0.42 | 1.28 | 5.1 | 5.7 | 43.6 | 1.11 |
| 01_basic_message | descriptor | 1.50±0.03 | 2.50±0.06 | 30.33±0.57 | 1.67 | 5.1 | 6.1 | 44.4 | 1.21 |
| 01_basic_message | plugin | 4.81±0.12 | 6.44±1.18 | 35.37±5.28 | 1.34 | 5.1 | 6.1 | 44.4 | 1.19 |
| bench_tiny | descriptor | 2.66±0.04 | 3.40±0.11 | 33.46±0.30 | 1.28 | 5.6 | 6.9 | 45.1 | 1.24 |
| bench_tiny | plugin | 8.75±4.54 | 9.58±5.69 | 40.92±0.53 | 1.09 | 5.9 | 7.0 | 44.6 | 1.18 |
| bench_small | descriptor | 12.37±0.06 | 8.14±0.23 | 76.28±0.64 | 0.66 | 10.3 | 10.2 | 51.7 | 0.99 |
| bench_small | plugin | 49.97±2.09 | 38.87±1.42 | 109.78±1.39 | 0.78 | 16.5 | 16.3 | 52.3 | 0.99 |
| bench_medium | descriptor | 76.05±0.75 | 34.69±1.07 | 291.47±5.18 | 0.46 | 34.6 | 27.8 | 91.2 | 0.80 |
| bench_medium | plugin | 287.37±16.95 | 192.09±37.07 | 458.75±3.85 | 0.67 | 70.9 | 70.8 | 95.4 | 1.00 |
| bench_large | descriptor | 467.25±13.16 | 154.08±2.45 | 1259.25±16.08 | 0.33 | 138.8 | 100.9 | 248.9 | 0.73 |
| bench_large | plugin | 1439.57±20.12 | 866.31±12.45 | 2008.39±14.24 | 0.60 | 311.8 | 309.8 | 314.0 | 0.99 |
| 329_large_stress | descriptor | 76.45±1.41 | 33.47±0.35 | 291.93±3.03 | 0.44 | 34.6 | 27.8 | 91.2 | 0.80 |
| 329_large_stress | plugin | 284.40±2.17 | 191.95±4.43 | 457.63±2.01 | 0.67 | 71.5 | 71.0 | 95.8 | 0.99 |
| bench_multi | descriptor | 50.75±1.37 | 21.18±0.26 | 166.18±2.01 | 0.42 | 20.1 | 25.5 | 82.3 | 1.27 |
| bench_multi | plugin | 200.45±1.00 | 134.61±2.00 | 285.51±3.57 | 0.67 | 52.0 | 51.8 | 85.4 | 1.00 |
| google_corpus | descriptor | 85.90±0.85 | 45.07±1.08 | n/a | 0.52 | 17.5 | 35.3 | n/a | 2.01 |
| google_corpus | plugin | 222.77±2.34 | 126.41±1.72 | n/a | 0.57 | 41.3 | 41.2 | n/a | 1.00 |

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
