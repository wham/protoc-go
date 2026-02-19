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

ALL DONE — 453/453 tests passing.

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
49. ✅ Proto3 enum first-value-must-be-zero validation — reject enums whose first value is not zero with "The first enum value must be zero for open enums." error, handles both top-level and nested enums
50. ✅ Proto3 required field validation — reject `required` fields in proto3 with "Required fields are not allowed in proto3." error, location from TYPE SCI path
51. ✅ Reserved field number validation — reject field numbers 19000-19999 with "Field numbers 19000 through 19999 are reserved for the protocol buffer library implementation." error (applies to all syntaxes, checks message fields, extensions, and nested messages)
52. ✅ Duplicate field number validation — reject duplicate field numbers within a message with `Field number N has already been used in "pkg.Msg" by field "name".` error, location from SCI path for field number
53. ✅ Non-positive field number validation — reject field numbers ≤ 0 with `Field numbers must be positive integers.` error and `Suggested field numbers for pkg.Msg: N` suggestion, location from SCI path for field number
54. ✅ Max field number validation — reject field numbers > 536870911 (2^29-1) with `Field numbers cannot be greater than 536870911.` error and suggestion, location from SCI path for field number
55. ✅ Duplicate enum value validation — reject duplicate enum value numbers without `allow_alias = true` with `"pkg.VALUE" uses the same enum value as "pkg.FIRST"` error, next available value suggestion, handles top-level and nested enums
56. ✅ Duplicate symbol name validation — reject duplicate fully-qualified names (messages, enums, enum values, services, methods, fields, oneofs) with `"Name" is already defined in "scope".` error, location from SCI name path
57. ✅ Proto2 missing label validation — reject fields without `required`/`optional`/`repeated` labels in proto2 syntax with `Expected "required", "optional", or "repeated".` error, collects all errors via `MultiError` type
58. ✅ Map-in-oneof validation — reject `map<K,V>` fields inside `oneof` blocks with `Map fields are not allowed in oneofs.` error at the `<` token position
59. ✅ Oneof label validation — reject `required`/`optional`/`repeated` labels on fields inside `oneof` blocks with `Fields in oneofs must not have labels (required / optional / repeated).` error
60. ✅ No-syntax files default to proto2 — initialize `p.syntax = "proto2"` so files without `syntax` declaration require labels on fields (matching C++ protoc default behavior)
61. ✅ Reserved field name conflict validation — reject fields whose name appears in the message's `reserved` list with `Field name "X" is reserved.` error at field name location, recurses into nested messages
62. ✅ Reserved field number conflict validation — reject fields whose number falls in the message's `reserved` ranges with `Field "X" uses reserved number N.` error + suggestion, location from reserved range start SCI path
63. ✅ Map key type validation — reject float/double/bytes/message/group as map key types with `Key in map fields cannot be float/double, bytes or message types.` error at field span location
64. ✅ Enum value C++ scoping note — when duplicate enum value names are detected across different enums in the same scope, emit the "Note that enum values use C++ scoping rules..." explanatory message matching C++ protoc
65. ✅ Empty enum validation — reject enums with zero values with `Enums must contain at least one value.` error at enum name location, handles both top-level and nested enums
66. ✅ Proto3 group validation — reject `group` fields in proto3 syntax with `Groups are not supported in proto3 syntax.` error at group keyword location
68. ✅ Duplicate syntax/edition declaration validation — reject second `syntax` or `edition` statement with `Expected top-level statement (e.g. "message").` error at duplicate keyword position
69. ✅ Duplicate package declaration validation — reject second `package` statement with `Multiple package definitions.` error at duplicate `package` keyword position (1-indexed line:col, no filename prefix since CLI adds it)
70. ✅ Duplicate oneof name validation — C++ protoc omits line:col for duplicate oneof names (`filename: "X" is already defined in "Y".`), pass 0,0 for oneofs in `collectDupNamesInMsg` and format without line:col when both are 0
70. ✅ Late syntax/edition rejection — reject `syntax` or `edition` statements that appear after any other non-syntax statement (e.g., `package`) with `Expected top-level statement` error, using error recovery to continue parsing and collect subsequent errors (e.g., missing labels in proto2 mode)
71. ✅ Octal/hex integer default value normalization — convert octal (`0755`) and hex (`0xFF`) integer literals in `[default = ...]` to decimal strings to match C++ protoc behavior (`isIntegerType` + `normalizeIntDefault` helpers in parser.go)
72. ✅ Adjacent string concatenation in field default values (`[default = "hello" " world"]`) — concatenate adjacent string tokens in `parseFieldOptions`, updating `valEnd` to last token's end position
73. ✅ `no_standard_descriptor_accessor` message option (field 2 of MessageOptions) with source code info
74. ✅ Multi-token type name spans — track actual last token end position (not string length) for type names with spaces between dots (e.g., `spacetype . Inner`), applies to field type_name, RPC input/output types, and extend extendee names
75. ✅ Recursive import cycle detection — detect self-imports and circular import chains with `File recursively imports itself: A -> B -> A` error, using import stack in `parseRecursive` and SCI location for the import statement
76. ✅ Circular import multi-error reporting — for circular imports (a.proto → b.proto → a.proto), report cycle error at cycle-starting file's import location, then "Import X was not found or had errors." for failed deps, then unresolved type errors ("X is not defined.") matching C++ protoc's error output exactly
77. ✅ Float default value precision — use `simpleDtoa` (%.15g + round-trip) / `simpleFtoa` (%.6g + round-trip as float32) to match C++ protoc's `SimpleDtoa`/`SimpleFtoa` (e.g., `1e10` → `10000000000`)
78. ✅ Proto3 extension range validation — reject `extensions` declarations in proto3 messages with `Extension ranges are not allowed in proto3.` error at extension range start location
79. ✅ Proto3 extend block validation — reject `extend` blocks in proto3 files (file-level and message-level) unless extending an allowed option type (`google.protobuf.*Options`, `FeatureSet`, etc.) with `Extensions in proto3 are only allowed for defining options.` error at extendee name location
80. ✅ Unknown file option validation — reject unknown file options (e.g., `php_generic_services`) with `Option "X" unknown. Ensure that your proto definition file imports the proto which defines the option.` error at option name position
81. ✅ `message_set_wire_format` message option (field 1 of MessageOptions) with source code info, and INT32_MAX (2147483647) extension range end when enabled (instead of 536870912)
82. ✅ `debug_redact` field option (field 16 of FieldOptions) and `unverified_lazy` field option (field 15 of FieldOptions) with source code info
83. ✅ Duplicate file option validation — reject duplicate file options (e.g., two `java_package` declarations) with `Option "X" was already set.` error at option name position
84. ✅ Proto3 optional synthetic oneof ordering — declared oneofs come before synthetic oneofs in `OneofDecl` (deferred creation after message body parsing)
85. ✅ Duplicate message option validation — reject duplicate message options (e.g., two `deprecated` declarations) with `Option "X" was already set.` error at option name position, using per-message `seenMsgOptions` map
86. ✅ Duplicate field option validation — reject duplicate options within field option brackets (e.g., `[deprecated = true, deprecated = false]`) with `Option "X" was already set.` error at option name position, using per-field `seenFieldOpts` map in `parseFieldOptions`
87. ✅ Duplicate enum option validation — reject duplicate enum options (e.g., two `deprecated` declarations) with `Option "X" was already set.` error at option name position, using per-enum `seenEnumOptions` map passed to `parseEnumOption`
88. ✅ Duplicate service option validation — reject duplicate service options (e.g., two `deprecated` declarations) with `Option "X" was already set.` error at option name position (1-indexed line:col), using per-service `seenServiceOptions` map passed to `parseServiceOption`
89. ✅ Duplicate method option validation — reject duplicate method options (e.g., two `deprecated` declarations) with `Option "X" was already set.` error at option name position (1-indexed line:col), using per-method `seenMethodOptions` map passed to `parseMethodOption`
90. ✅ Duplicate enum value option validation — reject duplicate options within enum value option brackets (e.g., `HIGH = 1 [deprecated = true, deprecated = false]`) with `Option "X" was already set.` error at option name position, using per-value `seenEnumValOpts` map
91. ✅ Invalid syntax identifier validation — reject unrecognized syntax values (e.g., `syntax = "proto4"`) with `Unrecognized syntax identifier "X".  This parser only recognizes "proto2" and "proto3".` error at syntax value token position
92. ✅ Boolean file option validation — reject non-identifier values (e.g., `option java_multiple_files = 1;`) for boolean file options with `Value must be identifier for boolean option "google.protobuf.FileOptions.X".` error at value token position
93. ✅ Extension range validation — reject extension field numbers outside declared extension ranges with `"pkg.Msg" does not declare N as an extension number.` error at field number SCI location, checks both file-level and message-level extensions
94. ✅ Proto2 oneof fields — skip "must have label" check for fields inside oneof blocks (set `inOneof` flag on parser)
95. ✅ Duplicate import validation — reject importing the same file twice with `Import "X" was listed twice.` error at import keyword position, using `seenImports` map in parser

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
- Proto3 enum zero validation: C++ protoc requires the first enum value in proto3 (open enums) to be zero. Validated in `validateProto3` via `collectProto3EnumZeroErrors`. Error location from SCI path `[enumPath..., 2, valueIdx, 2]` (field 2=number in EnumValueDescriptorProto). Handles both top-level enums and enums nested inside messages (via `collectProto3MessageErrors`).
- Proto3 required field validation: C++ protoc rejects `required` fields in proto3. Validated in `collectProto3RequiredErrors` (cli.go). Error location from SCI path `[msgPath..., 2, fieldIdx, 5]` (field 5=type in FieldDescriptorProto). In `collectProto3MessageErrors`, required errors are collected before default value errors to match C++ protoc ordering.
- Reserved field number validation: C++ protoc rejects field numbers 19000-19999 (kFirstReservedNumber through kLastReservedNumber) in ALL syntaxes (proto2, proto3, editions). Error format is `filename: Field numbers 19000 through 19999 are reserved for the protocol buffer library implementation.` (no line:col). Validated in `validateReservedFieldNumbers` (cli.go). Checks message fields, file-level extensions, message-level extensions, and recurses into nested messages.
- Duplicate field number validation: C++ protoc rejects duplicate field numbers within a message. Error format is `filename:line:col: Field number N has already been used in "pkg.Msg" by field "name".` Location from SCI path `[msgPath..., 2, fieldIdx, 3]` (field 3=number in FieldDescriptorProto). Validated in `validateDuplicateFieldNumbers` (cli.go). Recurses into nested messages, skips map entry types.
- Non-positive field number validation: C++ protoc rejects field numbers ≤ 0 with two error lines: `Field numbers must be positive integers.` and `Suggested field numbers for pkg.Msg: N` where N is the smallest available positive integer. Location from SCI path `[msgPath..., 2, fieldIdx, 3]`. Validated in `validatePositiveFieldNumbers` (cli.go) before reserved number validation. `suggestFieldNumber` finds the smallest unused positive field number in the message.
- Max field number validation: C++ protoc rejects field numbers > 536870911 (2^29-1, kMaxFieldNumber) with `Field numbers cannot be greater than 536870911.` error plus suggestion. Validated in `validateMaxFieldNumbers` (cli.go) between positive and reserved field number validation. Handles message fields, extensions, and nested messages. Uses same `findFieldNumberLocation` and `suggestFieldNumber` helpers.
- Duplicate enum value validation: C++ protoc rejects duplicate enum value numbers when `allow_alias` is not set. Error format: `"pkg.VALUE" uses the same enum value as "pkg.FIRST". If this is intended, set 'option allow_alias = true;' to the enum definition. The next available enum value is N.` IMPORTANT: enum values are scoped to the parent (package or message), NOT the enum name — so the FQN is `pkg.VALUE` not `pkg.EnumName.VALUE`. Validated in `validateDuplicateEnumValues` (cli.go). `nextAvailableEnumValue` finds the smallest non-negative integer not used. Handles top-level enums and enums nested in messages.
- Duplicate symbol name validation: C++ protoc rejects symbols with duplicate fully-qualified names. Error format: `filename:line:col: "ShortName" is already defined in "scope".` where scope is the package name for top-level definitions or the parent message FQN for nested ones. Validated in `validateDuplicateNames` (cli.go). Checks messages, enums, enum values (scoped to parent), services, methods, fields, and oneofs. Uses `findLocationByPath` to look up the name field location (field 1) in SCI. Skips map entry synthetic types. Run before field number validations.
- Proto2 missing label validation: In proto2, fields must have explicit `required`, `optional`, or `repeated` labels. Error at field's type token position: `Expected "required", "optional", or "repeated".` Parser collects all such errors in `p.errors` slice and returns a `MultiError` at the end (error recovery — continues parsing). `MultiError` includes filename in each error line. CLI's `parseRecursive` detects `*parser.MultiError` and returns it unwrapped to avoid double-prefixing the filename.
- Map-in-oneof validation: `map<K,V>` fields inside `oneof` blocks are rejected with `Map fields are not allowed in oneofs.` error. Detected in `parseOneof` using `PeekAt(1)` to check for `<` after `map`. Error position is at the `<` token (1-based line:col). Must consume `map` first to get the `<` token position.
- Reserved field name conflict validation: C++ protoc rejects fields whose name appears in the message's `reserved_name` list. Error format: `filename:line:col: Field name "X" is reserved.` Location from SCI path `[msgPath..., 2, fieldIdx, 1]` (field 1=name). Validated in `validateReservedNameConflicts` (cli.go) with `collectReservedNameErrors` recursing into nested messages. Skips map entry types. Runs after duplicate field number validation.
- Reserved field number conflict validation: C++ protoc rejects fields whose number falls in a message's `reserved_range`. Error format: `filename:line:col: Field "X" uses reserved number N.` followed by `Suggested field numbers for pkg.Msg: N`. IMPORTANT: location comes from the reserved range start SCI path `[msgPath..., 9, rangeIdx, 1]` (NOT the field number location). Uses `findReservedRangeStartLocation` in cli.go. Runs between duplicate field numbers and reserved name conflicts.
- Map key type validation: C++ protoc rejects float/double/bytes/message/group as map key types. Error format: `filename:line:col: Key in map fields cannot be float/double, bytes or message types.` Location from the map field's SCI span start (path `[msgPath..., 2, fieldIdx]`). Validated in `validateMapKeyTypes` (cli.go) with `collectMapKeyTypeErrors` recursing into nested messages. Finds parent field by matching TypeName to map entry NestedType name.
- Empty enum validation: C++ protoc rejects enums with zero values. Error format: `filename:line:col: Enums must contain at least one value.` Location from SCI path for enum name (field 1). Validated in `validateEmptyEnums` (cli.go) with `collectEmptyEnumErrors` recursing into nested messages. Runs before duplicate enum value validation.
- Proto3 group validation: C++ protoc rejects groups in proto3 with `Groups are not supported in proto3 syntax.` at the "group" keyword position. Validated in `collectProto3GroupErrors` (cli.go), called first in `collectProto3MessageErrors` before required/default checks. Uses existing `findFieldTypeLocation` to get the TYPE_GROUP field's type SCI path position.
- Late syntax/edition rejection: `syntax` or `edition` must be the very first statement. If any other statement (e.g., `package`, `import`, `message`) appears before it, `syntax`/`edition` is rejected with `Expected top-level statement (e.g. "message").` error. Uses error recovery (skips to `;` and continues parsing) so subsequent errors (e.g., missing labels in proto2 default mode) are also collected. `hadNonSyntaxStmt` flag in parser tracks this.
- Octal/hex integer default values: C++ protoc converts octal (0755) and hex (0xFF) integer literals in default values to decimal strings (493, 255). Handled by `isIntegerType` and `normalizeIntDefault` in parser.go. Applied after float normalization in `parseFieldOptions` for all integer field types (int32, int64, uint32, uint64, sint32, sint64, fixed32, fixed64, sfixed32, sfixed64).
- Duplicate oneof name error format: C++ protoc omits line:col for duplicate oneof names, outputting `filename: "X" is already defined in "Y".` (with space after colon, no line:col). Other duplicate symbols (messages, fields, enums, etc.) include line:col. In `collectDupNamesInMsg`, oneofs pass 0,0 to the `check` function, and `check` formats without line:col when both are 0.
- Multi-token type name spans: when a type reference has spaces between tokens (e.g., `spacetype . Inner`), the SCI span end must use the actual last token's end position (`lastTok.Column + len(lastTok.Value)`), not `startCol + len(concatenatedTypeName)`. Affects `parseField` (field type_name), `parseMethod` (input/output types), `parseTopLevelExtend` and `parseNestedExtend` (extendee names). Track a `typeEndTok`/`extNameEndTok`/etc. variable as tokens are consumed.
- Recursive import cycle detection: `parseRecursive` takes an `importStack []string` parameter tracking the current import chain. Before processing a file, check if it's already in the stack — if so, build the chain string (e.g., `test.proto -> test.proto`) and return error with location from SCI path `[3, depIdx]` of the importing file's import statement. The `findImportLocation` helper looks up the SCI entry for the import. `newStack := append(importStack, filename)` is passed to recursive calls.
- Circular import multi-error: `parseRecursive` returns `(bool, error)` and accepts `collectErrors *[]string`. When a cycle is detected, the error is reported at the cycle-starting file's import of the NEXT file in the chain (e.g., a.proto:5:1 for `import "b.proto"` in a.proto). Self-imports (dep == filename) skip follow-on errors. For non-self circular imports: after cycle detection, the importing file adds "Import X was not found or had errors." errors for each failed dep, then calls `parser.CheckUnresolvedTypes` with restricted availableFiles (excluding failed deps) to find unresolved type references ("X is not defined." errors). The `CheckUnresolvedTypes` function in parser.go builds a types map from the file's own types + available deps, then checks all field TypeName, service method InputType/OutputType references against the map. Unresolved types are reported with position from SCI path (type_name field 6 for message fields, input_type field 2 / output_type field 3 for methods).
- Float default precision: C++ protoc normalizes float defaults via `SimpleDtoa` (%.15g + round-trip, fallback to %.17g) and `SimpleFtoa` (%.6g as float32 + round-trip, fallback to %.9g). Go's `strconv.FormatFloat(v, 'g', -1, 64)` uses shortest representation which differs (e.g., `1e+10` vs `10000000000`). Implemented `simpleDtoa` and `simpleFtoa` in parser.go matching C++ behavior. TYPE_FLOAT values are cast to float32 before formatting.
- Proto3 extend block validation: C++ protoc rejects `extend` blocks in proto3 files unless the extendee is an allowed option type (google.protobuf.*Options, FeatureSet, SourceCodeInfo, GeneratedCodeInfo, StreamOptions). Error format: `filename:line:col: Extensions in proto3 are only allowed for defining options.` Location from SCI path for extendee (field 2 of FieldDescriptorProto): `[7, extIdx, 2]` for file-level, `[msgPath..., 6, extIdx, 2]` for message-level. Validated in `collectProto3ExtendErrors` (cli.go), called from both `validateProto3` (file-level) and `collectProto3MessageErrors` (message-level). `allowedProto3Extendee` checks if FQN starts with `.google.protobuf.` and suffix matches known types.
- Duplicate file option validation: parser tracks `seenFileOptions` map in parser struct. When a file option name is encountered a second time, returns `Option "X" was already set.` error at the option name token position (1-indexed line:col). Applies to all recognized file options (java_package, go_package, etc.).
- Duplicate method option validation: parser tracks `seenMethodOptions` map per method (created in method body parsing loop). When a method option name is encountered a second time, returns `Option "X" was already set.` error at the option name token position (1-indexed line:col). Passed to `parseMethodOption` same pattern as service/enum/field/message options.
- Proto3 optional synthetic oneof ordering: declared oneofs are placed before synthetic oneofs in OneofDecl by deferring synthetic oneof creation until after message body parsing completes. This matches C++ protoc behavior where declared oneof blocks get lower indices than synthetic oneofs from proto3 optional fields.
- Boolean file option validation: boolean file options (java_multiple_files, cc_generic_services, java_generic_services, py_generic_services, deprecated, java_string_check_utf8, cc_enable_arenas) must use identifier values (`true`/`false`), not integers (`0`/`1`) and not string literals (`"true"`/`"false"`). Error: `Value must be identifier for boolean option "google.protobuf.FileOptions.X".` at value token position. Uses `validateBool` helper in `parseFileOption` — checks both `valTok.Type == TokenIdent` AND value is "true"/"false".
- Extension range validation: extension field numbers must fall within the extendee message's declared extension ranges. Error format: `filename:line:col: "pkg.Msg" does not declare N as an extension number.` Location from SCI path for field number: `[7, extIdx, 3]` for file-level, `[msgPath..., 6, extIdx, 3]` for message-level. `validateExtensionRanges` in cli.go builds a map of message FQN → extension ranges from all files, then checks each extension field. `isInExtensionRange` checks if a number falls within any range (Start inclusive, End exclusive).
- Proto2 oneof fields: fields inside `oneof` blocks do NOT require `required`/`optional`/`repeated` labels even in proto2 syntax. Parser uses `p.inOneof` flag (set in `parseOneof` before calling `parseField`, cleared after) to skip the label check in `parseField`'s default case. No label SCI is emitted for oneof fields (labelTok stays nil).
- Duplicate import validation: C++ protoc rejects importing the same file twice with `Import "X" was listed twice.` error at the `import` keyword position (1-indexed line:col). Parser tracks `seenImports` map (set in `parseImport`). Error returned before adding to `fd.Dependency`, so the duplicate never enters the dependency list.
