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

ALL DONE — 118/118 tests passing.

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
19. ✅ Proto2 support: required/optional labels, default values, proto2 syntax handling
20. ✅ Proto3 optional fields: synthetic oneofs (`_<field_name>`), `proto3_optional` flag, `oneof_index`
21. ✅ Extension range parsing (`extensions 100 to 199; extensions 1000 to max;`) with extensionRange + source code info
22. ✅ Enum option parsing (`allow_alias`, `deprecated`) with EnumOptions and source code info
23. ✅ Comment tracking in SourceCodeInfo (leading, trailing, detached comments) with file-level span fix
24. ✅ Enum value options (`deprecated = true`) with EnumValueOptions and source code info
25. ✅ Message option parsing (`option deprecated = true/false;`) with MessageOptions and source code info
26. ✅ Service option parsing (`option deprecated = true;`) with ServiceOptions and source code info
27. ✅ Method option parsing (`option deprecated = true;`) with MethodOptions and source code info
28. ✅ Enum reserved ranges and reserved names parsing with source code info
29. ✅ Fully-qualified type names (`.pkg.Type`) in field types, map values, and RPC input/output types
30. ✅ Empty statements (standalone `;`) at top-level file scope

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
- Proto2 support: parser handles `required`, `optional`, `repeated` labels (previously only `repeated`); `default` option sets FieldDescriptorProto.DefaultValue with source code info at path [..., 7]; proto2 syntax declaration omits fd.Syntax (C++ protoc leaves it unset for proto2)
- Label source code info (path [..., 4]) is emitted for ALL explicit labels, not just `repeated`
- Proto3 optional: when syntax=proto3 and `optional` keyword is used, set `Proto3Optional=true` on field, create synthetic `OneofDecl` named `_<fieldname>`, set `OneofIndex` on field. No source code info is generated for synthetic oneofs.
- Extension ranges: field 5 of DescriptorProto. Similar to reserved ranges (field 9). `max` keyword maps to 536870912 (2^29, kMaxRangeSentinel). Source code info paths: [4,msgIdx,5] (stmt), [4,msgIdx,5,rangeIdx] (range), [4,msgIdx,5,rangeIdx,1] (start), [4,msgIdx,5,rangeIdx,2] (end).
- Enum options: field 3 of EnumDescriptorProto. `allow_alias` is field 2, `deprecated` is field 3 of EnumOptions. Source code info: [5,enumIdx,3] for statement, [5,enumIdx,3,fieldNum] for specific option. Both share the same span covering the full `option ... ;` statement.
- Comment tracking: tokenizer collects comments between tokens and classifies them as PrevTrailing (trailing comment of previous token), Detached (comments separated by blank lines), and Leading (last comment block before token). Parser's `attachComments(locIdx, firstTokenIdx)` attaches leading/detached from firstToken and trailing from the next-token-after-terminator. File-level span starts at first non-comment token, not line 0.
- Enum value options: `[deprecated = true]` after `= N` in enum values. Parsed inline (not with skipBracketedOptions). EnumValueOptions field numbers: deprecated=1. Source code info path: [..., 3] for options bracket span, [..., 3, fieldNum] for specific option spanning name through value.
- Message options: field 7 of DescriptorProto. `deprecated` is field 3 of MessageOptions. Source code info: [4,msgIdx,7] for statement, [4,msgIdx,7,3] for deprecated. Both share same span covering full `option ... ;` statement. Parsed by `parseMessageOption` in parser.go.
- Service options: field 3 of ServiceDescriptorProto. `deprecated` is field 33 of ServiceOptions. Source code info: [6,svcIdx,3] for statement, [6,svcIdx,3,33] for deprecated. Both share same span. Parsed by `parseServiceOption` in parser.go.
- Method options: field 4 of MethodDescriptorProto. `deprecated` is field 33 of MethodOptions. Source code info: [6,svcIdx,2,methodIdx,4] for options, [6,svcIdx,2,methodIdx,4,33] for deprecated. Both share same span. Parsed by `parseMethodOption` in parser.go. Method body `{ ... }` is now parsed properly instead of blindly skipping tokens.
- Negative enum values: when parsing `= -1`, the source code info span for the enum value number (path [..., 2]) must start at the minus sign column, not the digit column. Track the minus token and use its column as span start.
- Enum reserved ranges: field 4 of EnumDescriptorProto (`reserved_range`), uses `EnumReservedRange` with inclusive end (unlike message reserved which is exclusive). Source code info paths: [5,enumIdx,4] (stmt), [5,enumIdx,4,rangeIdx] (range), [...,1] (start), [...,2] (end). Single values have start==end.
- Enum reserved names: field 5 of EnumDescriptorProto (`reserved_name`). Source code info: [5,enumIdx,5] (stmt), [5,enumIdx,5,nameIdx] (individual name). Name spans include +2 for quotes.
