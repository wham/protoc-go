The `.proto` files of the kaja.tools demo services, copied from
[kaja-tools/website](https://github.com/kaja-tools/website) (MIT) at commit
`ab4619b`: `apps/quirks/proto` and `apps/seating/proto`. They are the corpus of
the `kaja_*` cases in `protoc/bench_test.go`, an application's real schemas
compiled through the Go API the way the website's `scripts/protoc` does with
the CLI. Not a correctness fixture: no `.proto` sits at this level, so
`scripts/test` does not register the directory.
