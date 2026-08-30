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

Last verified 2026-08-29 · commit `e30986b` · Go 1.23.12 on ubuntu24 · [run log](https://github.com/wham/protoc-go/actions/runs/33270684392)

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

Across 14 compile cases: Go faster on 10, C++ faster on 4, tie on 0.

| case | variant | C++ ms | Go ms | go/cpp | C++ peak MB | Go peak MB | go/cpp |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| startup_empty | descriptor | 1.37±0.08 | 1.98±0.07 | 1.44 | 5 | 6.8 | 1.36 |
| startup_empty | plugin | 3.37±0.13 | 4.04±0.10 | 1.20 | 5.5 | 7 | 1.26 |
| 01_basic_message | descriptor | 1.56±0.05 | 2.30±0.23 | 1.47 | 5 | 9.1 | 1.81 |
| 01_basic_message | plugin | 3.77±0.10 | 4.77±0.12 | 1.27 | 5.5 | 9.1 | 1.64 |
| bench_tiny | descriptor | 2.77±0.06 | 2.99±0.10 | 1.08 | 5.5 | 9.5 | 1.71 |
| bench_tiny | plugin | 6.89±0.15 | 7.61±0.28 | 1.11 | 7.6 | 9.3 | 1.24 |
| bench_small | descriptor | 13.80±0.26 | 6.90±0.24 | 0.50 | 10.3 | 13.4 | 1.31 |
| bench_small | plugin | 46.47±2.26 | 30.93±0.82 | 0.67 | 18.9 | 18.8 | 1.00 |
| bench_medium | descriptor | 75.03±2.87 | 23.01±0.76 | 0.31 | 34.6 | 31.7 | 0.92 |
| bench_medium | plugin | 254.70±7.43 | 149.81±4.64 | 0.59 | 74.2 | 72 | 0.97 |
| bench_large | descriptor | 445.34±12.93 | 97.95±3.54 | 0.22 | 138.7 | 108.5 | 0.78 |
| bench_large | plugin | 1254.47±16.19 | 655.84±9.16 | 0.52 | 311.4 | 311.4 | 1.00 |
| 329_large_stress | descriptor | 75.42±3.08 | 23.21±0.68 | 0.31 | 34.6 | 31.7 | 0.92 |
| 329_large_stress | plugin | 259.25±5.34 | 153.77±7.04 | 0.59 | 74 | 74.1 | 1.00 |
| google_corpus | descriptor | 92.15±1.90 | 33.30±0.85 | 0.36 | 17.5 | 40.8 | 2.34 |
| google_corpus | plugin | 208.16±2.83 | 100.52±2.54 | 0.48 | 43.7 | 43.8 | 1.00 |

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
scripts/bench           # performance comparison
```

Requires Go 1.23+, plus a C++ `protoc` on your PATH for the comparison suites (e.g. `brew install protobuf`).

Releases are cut by a `release: major|minor|patch|none` label on the pull
request, so a change nobody can observe never mints a version.
