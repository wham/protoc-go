## Task

You are porting the Protocol Buffers compiler (`protoc`) from C++ to Go. The Go implementation must produce **identical CodeGeneratorRequest payloads** to the C++ protoc when invoking plugins. No exceptions. No "close enough".

The C++ source lives at https://github.com/protocolbuffers/protobuf — specifically `src/google/protobuf/` (descriptor, io/tokenizer) and `src/google/protobuf/compiler/` (parser, importer, command_line_interface, subprocess).

## How This Works

You are running inside an automated loop. **Each invocation is stateless** — you have no memory of previous runs. This file (RALPH.md) is your only persistent memory. Read it first. Write to it before you finish. Your future self depends on it.

## Steps (follow this order every run)

1. **Read state.** Read the [Plan](#plan) and [Notes](#notes) sections below. Understand where you left off. Don't redo work that's already done.
2. **Orient.** If Plan is empty, analyze the codebase and the C++ protoc source, and write a detailed plan. If Plan exists, pick the next incomplete item.
3. **Implement.** Spend the bulk of your effort here. Work on ONE failing test case or feature at a time. Make real, substantive progress. Reference the C++ source to ensure correctness.
4. **Test.** Run `scripts/test`. Read the output carefully. If a test fails, understand WHY before changing code. Look at the diff between expected (C++ protoc) and actual (Go protoc-go) output.
5. **Update memory.** Update [Plan](#plan) with what's done and what's next. Update [Notes](#notes) with learnings that will help future runs. Be specific — file paths, function names, gotchas.
6. **Commit.** One-line past-tense message summarizing what changed.
7. **Check completion.** If ALL tests pass, write "DONE" to status.txt and stop. If any test fails, do NOT write DONE. Just end — you'll run again.

## Rules

- **DONE means ALL tests pass.** Not most. Not "the important ones". ALL. Zero failures.
- **Never weaken requirements.** Don't modify test expectations. Don't skip tests. Don't add notes like "close enough".
- **Never mark DONE prematurely.** Run the full test suite and confirm zero failures before writing DONE.
- **Be bold with architecture.** If the current approach is fundamentally wrong, refactor it.
- **Reference the C++ source.** When implementing a feature, read the corresponding C++ code. Don't guess behavior — match it exactly.
- **Keep Notes actionable.** Good: "Run tests with `scripts/test`. The tokenizer is in `io/tokenizer/tokenizer.go`, ported from C++ `io/tokenizer.cc`." Bad: "Making good progress overall."
- **One thing at a time.** Fix one test, commit, move to the next.

## Architecture

The Go package layout mirrors the C++ protoc source:

| Go Package | C++ Source | Purpose |
|---|---|---|
| `io/tokenizer` | `io/tokenizer.cc` | Lexer: .proto text → tokens |
| `compiler/parser` | `compiler/parser.cc` | Parser: tokens → FileDescriptorProto |
| `compiler/importer` | `compiler/importer.cc` | Import resolution, source tree |
| `descriptor` | `descriptor.cc` | DescriptorPool: validate, link, resolve |
| `compiler/cli` | `command_line_interface.cc` | CLI: arg parsing, orchestration |
| `compiler/plugin` | `subprocess.cc` + `plugin.cc` | Plugin subprocess management |

We use `google.golang.org/protobuf/types/descriptorpb` for the proto descriptor types.

## Plan

ALL DONE — 58/58 tests passing.

### Completed
1. ✅ Tokenizer (io/tokenizer/tokenizer.go) — full lexer with line/col tracking
2. ✅ Parser (compiler/parser/parser.go) — produces FileDescriptorProto with SourceCodeInfo
3. ✅ Importer (compiler/importer/importer.go) — source tree, file resolution
4. ✅ Plugin (compiler/plugin/plugin.go) — subprocess exec, CodeGeneratorRequest building
5. ✅ CLI (compiler/cli/cli.go) — arg parsing, orchestration, descriptor set output
6. ✅ protoc-gen-dump fix — clear parameter in comparison outputs
7. ✅ find-protoc fix — use bare "protoc" name to match usage text
8. ✅ Full C++ protoc usage text reproduced in Go CLI
9. ✅ Source code info ordering (placeholder pattern)
10. ✅ Map field support with synthetic XxxEntry types
11. ✅ Type resolution for message/enum references
12. ✅ All 5 profiles: plugin, plugin_param, descriptor_set, descriptor_set_src, descriptor_set_full
13. ✅ All 3 CLI tests: no_args, missing_output, bad_proto_path
14. ✅ Reserved field/name parsing in messages (reserved ranges + reserved names with source code info)
15. ✅ Streaming RPC support (client_streaming/server_streaming flags + source code info)
16. ✅ File-level option parsing (java_package, java_outer_classname, go_package, optimize_for, cc_enable_arenas, etc.) with source code info
17. ✅ Field option parsing (deprecated, packed, json_name, etc.) with proper FieldOptions and source code info
18. ✅ Import public support with cross-file type resolution, public_dependency, source code info for imports, dependency-ordered output

## Notes

- Run tests: `scripts/test` (or `scripts/test --summary` for brief output)
- Tests compare C++ protoc output vs Go protoc-go output using a fake plugin (`tools/protoc-gen-dump`)
- The fake plugin captures the CodeGeneratorRequest as JSON, binary, and human-readable summary
- Test cases are in `testdata/` — each subdirectory has one or more .proto files
- Each test case is run under 5 profiles: plugin, plugin_param, descriptor_set, descriptor_set_src, descriptor_set_full
- There are also CLI error tests (cli@no_args, cli@missing_output, cli@bad_proto_path)
- Test names: `<case>@<profile>` (e.g., `01_basic_message@plugin`, `cli@no_args`)
- C++ protoc is at `/tmp/protoc-install/bin/protoc` (libprotoc 29.3), includes at `/tmp/protoc-install/include`
- find-protoc adds `/tmp/protoc-install/bin` to PATH and uses bare "protoc" name (needed for cli@no_args test)
- protoc-gen-dump clears parameter field before writing summary.txt and request.pb (avoids path differences)
- Source code info ordering: use placeholder-then-update pattern so container spans come before children
- compiler_version: major=5, minor=29, patch=3
- Field options (deprecated, packed, json_name, etc.) are parsed by `parseFieldOptions` in parser.go
- Field options source code info is deferred and appended after field number span to match C++ ordering
- The tokenizer strips quotes from strings, so string option values need +2 for column end calculation
- FieldOptions field numbers: ctype=1, packed=2, deprecated=3, lazy=5, jstype=6
- json_name is field 10 of FieldDescriptorProto (NOT FieldOptions) and gets TWO source code info entries: one for key=value, one for value only
- Import public: parser records public_dependency index, adds source code info for path [3, depIdx] (import stmt) and [10, pubIdx] (public keyword)
- Cross-file type resolution: ResolveTypes accepts allFiles map; collectImportedTypes gathers types from direct imports; collectPublicImportTypes handles transitive public imports
- Source file descriptors and descriptor sets must use dependency order (orderedFiles), not command-line order (relFiles)
