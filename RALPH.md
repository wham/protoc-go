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

ALL DONE — 208/208 tests passing.

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
31. ✅ Empty statements inside message, enum, and service bodies
32. ✅ `max` keyword in message reserved ranges (`reserved 100 to max;`)
33. ✅ String escape sequences in tokenizer (`\n`, `\t`, `\r`, `\\`, `\"`, `\'`, hex `\xHH`, octal `\NNN`)
34. ✅ Extend blocks (`extend Message { ... }`) with extension fields, extendee resolution, and source code info
35. ✅ Proto2 group fields (`repeated group Result = 1 { ... }`) with TYPE_GROUP, nested type generation, and source code info
36. ✅ Negative default value spans (`[default = -40]`) — span includes minus sign
37. ✅ Weak import support (`import weak "file.proto"`) with weak_dependency field and source code info
38. ✅ Adjacent string literal concatenation in file options (`option java_package = "com.example" ".concat";`)
39. ✅ `java_string_check_utf8` file option (field 27 of FileOptions)
40. ✅ Nested extend blocks (`extend` inside message bodies) with `DescriptorProto.extension` (field 6), source code info, and type resolution
41. ✅ String escape raw length tracking — Token.RawLen field for correct source code info spans on strings with escape sequences
42. ✅ Edition support (`edition = "2023"`) — sets `syntax="editions"` and `edition=EDITION_2023`, fields default to LABEL_OPTIONAL without synthetic oneofs
43. ✅ Method `idempotency_level` option (`NO_SIDE_EFFECTS`, `IDEMPOTENT`, `IDEMPOTENCY_UNKNOWN`) with MethodOptions field 34 and source code info
44. ✅ Oneof option validation — reject unknown options (e.g., `deprecated`) inside oneof blocks with matching C++ protoc error message
45. ✅ Float literals starting with dot (`.5`, `.25`) — tokenizer `readFloatStartingWithDot`, plus default value normalization via `strconv.FormatFloat` to match C++ `SimpleDtoa`/`SimpleFtoa`
46. ✅ Inf/NaN default values (`inf`, `-inf`, `nan`) — use lowercase C++ style instead of Go's `+Inf`/`NaN` formatting
47. ✅ Map field options (`[deprecated = true]` etc.) — parse with `parseFieldOptions` instead of `skipBracketedOptions`, with source code info
48. ✅ Proto3 explicit default value validation — reject `[default = ...]` in proto3 with error matching C++ protoc, using source code info for line/col

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
- Empty statements (`;`) must be handled not only at top-level file scope but also inside message, enum, and service body loops. Just consume the token and continue.
- Message reserved ranges support `max` keyword same as extension ranges: maps to 536870912 (kMaxRangeSentinel, 2^29). Parsed in `parseMessageReserved`.
- String escape sequences: tokenizer's `readString()` now handles C-style escapes (`\n`, `\t`, `\r`, `\a`, `\b`, `\f`, `\v`, `\\`, `\'`, `\"`, `\?`), hex escapes (`\xHH`), and octal escapes (`\NNN` up to 3 digits). Values stored unescaped in token.
- Extend blocks: `extend TypeName { fields... }` creates entries in `fd.Extension` (field 7 of FileDescriptorProto). Each field gets `Extendee` set to the fully-qualified extendee name. Source code info ordering: [7] (block), [7,N] (field), [7,N,2] (extendee — inserted right after field span), [7,N,4] (label), [7,N,5] (type), [7,N,1] (name), [7,N,3] (number). Extendee resolution handled in `ResolveTypes` alongside service methods.
- Proto2 groups: `label group Name = N { ... }` creates a field with TYPE_GROUP (lowercase name) and a nested message type (original name). The field has both `Type = TYPE_GROUP` and `TypeName` set. Source code info order: field span (placeholder), label, type ("group" keyword at path 5), name, number, nested type span (placeholder, same span as field), nested type name, type_name (path 6, same span as name). `resolveMessageFields` must not override TYPE_GROUP when resolving type names. `isGroupField` detects groups by checking if a label keyword is followed by "group". `PeekAt(offset)` added to tokenizer for lookahead.
- Negative default values: when parsing `[default = -40]`, the source code info span for the default value (path [..., 7]) must start at the minus sign column, not the digit column. Save the minus token and use its column as span start.
- Weak imports: `import weak "file.proto"` sets `weak_dependency` (field 11 of FileDescriptorProto) with the dependency index. Source code info path `[11, weakIdx]` for the "weak" keyword, span covers the keyword text. Similar to public imports (field 10).
- String concatenation: C++ protoc allows adjacent string literals to be concatenated (like C/C++). In `parseFileOption`, after reading the first string token, keep consuming adjacent string tokens and concatenate their values. This is used in options like `option java_package = "com.example" ".concat";`.
- Edition support: `edition = "2023";` parsed by `parseEdition` in parser.go. Sets `fd.Syntax = "editions"` and `fd.Edition = EDITION_2023`. SCI at path [12] (same as syntax). Fields without explicit labels get LABEL_OPTIONAL (no Proto3Optional, no synthetic oneofs). The `editionMap` in parser.go maps edition strings to `descriptorpb.Edition` enum values.
- Nested extend blocks: `extend TypeName { fields... }` inside a message body creates entries in `msg.Extension` (field 6 of DescriptorProto), NOT `fd.Extension`. Source code info paths use `[4,msgIdx,6]` for the block, `[4,msgIdx,6,extIdx]` for each field. Type/extendee resolution handled in `resolveMessageFields`. Parsed by `parseNestedExtend` in parser.go.
- Oneof options: `OneofOptions` in protoc v29.3 only has `features` (field 1) and `uninterpreted_option` (field 999). Standard options like `deprecated` are rejected with error: `Option "X" unknown. Ensure that your proto definition file imports the proto which defines the option.` Error is produced at parse time in `parseOneof`. CLI error wrapping uses `%s:%w` (no space) to match C++ format `filename:line:col: message`.
- Float literals starting with `.` (e.g., `.5`, `.25`): tokenizer's `readFloatStartingWithDot` handles the `.digits[eE[+-]digits]` pattern. Default values for TYPE_DOUBLE/TYPE_FLOAT are normalized via `strconv.FormatFloat(v, 'g', -1, 64)` to match C++ `SimpleDtoa` behavior (`.5` → `0.5`, `1.0` → `1`).
- Inf/NaN default values: C++ protoc uses lowercase `inf`, `-inf`, `nan`. Go's `strconv.FormatFloat` produces `+Inf`, `-Inf`, `NaN`. Special-case these before float normalization using `strings.ToLower` and matching `inf`/`-inf`/`nan`/`infinity`/`-infinity`.
- Map field options: `map<K,V> name = N [deprecated = true];` — options are parsed via `parseFieldOptions` (same as regular fields). The field is created before parsing options so `parseFieldOptions` can set them in place. SCI entries appended after number span to match C++ ordering.
- Proto3 default value validation: C++ protoc rejects explicit default values in proto3 during descriptor validation (descriptor.cc), not parsing. Our Go port validates after parsing+type resolution in `validateProto3` (cli.go). Collects all errors (not just first) to match C++ behavior. Error line/col comes from source code info for the default_value field (path ending in `[2, fieldIdx, 7]`).
