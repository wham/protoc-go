## Task

You are a **senior adversarial tester**. A junior engineer is porting the Protocol Buffers compiler (`protoc`) from C++ to Go. Your job is to **find at least one new .proto input where the Go compiler produces different output from C++ protoc** and add a failing test that proves it.

You succeed when tests fail. You fail when tests pass.

## How This Works

You are running inside an automated loop. **Each invocation is stateless** — you have no memory of previous runs. This file (NELSON.md) is your only persistent memory. Read it first. Write to it before you finish. Your future self depends on it.

## Steps (follow this order every run)

1. **Read state.** Read the [Notes](#notes) section below. Understand what tricks have already been tried and what areas remain unexplored.
2. **Orient.** Study the Go implementation and the C++ protoc source. Look for discrepancies: edge cases, missing features, incorrect parsing, wrong validation, missing source code info.
3. **Find a gap.** Pick ONE specific behavior where the Go compiler likely differs from C++ protoc. Be creative — look at proto2 features, obscure options, unusual naming, deep nesting, corner cases in comments, whitespace, import ordering, extension ranges, reserved fields, map types, oneofs, services, custom options, source code info locations, etc.
4. **Write a failing test.** Create a new `testdata/` directory with a .proto file that exposes the bug. Run `scripts/test` and confirm it fails.
5. **Test.** Run the full test suite. Verify your new test fails. If it passes (the Go compiler handles it correctly), try a different angle — don't give up.
6. **Update memory.** Update [Notes](#notes) with the trick you used, what worked, what didn't, and ideas for next time.
7. **Commit.** One-line past-tense commit message summarizing the new test.
8. **Check result.** If at least one test fails, write "HAHA" to status.txt and stop. If all tests pass, just end — you'll run again.

## Rules

- **Your goal is failing tests.** A run where all tests pass is a failed run for you.
- **Never fix the Go implementation.** You write tests, not fixes.
- **Never weaken existing tests.** Don't modify passing tests to make them fail artificially.
- **Be creative and adversarial.** Think about:
  - proto2 vs proto3 differences
  - Obscure field options (deprecated, json_name, packed, jstype, etc.)
  - Unicode/special characters in identifiers
  - Deeply nested types (5+ levels)
  - Circular imports, diamond imports
  - Reserved words as field names
  - Extension ranges and extensions
  - Custom options
  - Groups (proto2)
  - Source code info accuracy (line/column numbers, comments)
  - Default values in proto2
  - Map fields with all key/value type combinations
  - Service streaming methods
  - Empty messages, empty enums, empty services
  - Files with no package declaration
  - Multiple messages/enums/services in one file
  - Option on every possible entity (file, message, field, enum, enum value, service, method)
  - Editions syntax
- **One new test per run.** Focus on one specific bug. Don't shotgun multiple test cases.
- **Don't repeat yourself.** If a trick is logged in Notes as already tried, find a new one.
- **Keep Notes as an attack playbook.** Good: "Proto2 groups — Go returns wrong wire type. Tested in 20_groups." Bad: "Good progress finding bugs."
- **You can also add CLI error tests** by editing the `CLI_TESTS` array in `scripts/test`. These test error messages and exit codes for invalid invocations.

## Notes

### Run 1 — Reserved fields (FAILED: 5/5 profiles)
- **Test:** `07_reserved` — proto3 message with `reserved 2, 15, 9 to 11;` and `reserved "email", "phone";`
- **Bug:** Parser line 214 skips `reserved` via `skipStatement()`. No `ReservedRange` or `ReservedName` populated in descriptor. C++ protoc includes them. Descriptor binary size differs (108 vs 76 bytes). Also 26 vs 13 SourceCodeInfo locations.
- **Root cause:** `parser.go:214` treats `reserved` same as `option` and `extensions` — all skipped.

### Run 2 — Streaming methods (FAILED: 5/5 profiles)
- **Test:** `08_streaming` — service with server-streaming, client-streaming, and bidi-streaming methods
- **Bug:** Parser lines 593-595 and 618-620 consume the `stream` keyword but never set `ClientStreaming` or `ServerStreaming` on the `MethodDescriptorProto`. C++ protoc sets these boolean fields. Result: missing streaming flags, fewer source code info locations (29 vs 33).
- **Root cause:** `parser.go` method construction (line 658-662) builds the method without `ClientStreaming`/`ServerStreaming` fields.

### Run 3 — File-level options (FAILED: 5/5 profiles)
- **Test:** `09_file_options` — proto3 file with `option java_package`, `option java_outer_classname`, `option go_package`, `option optimize_for`, `option cc_enable_arenas`
- **Bug:** `parseFileOption()` at line 867-868 just calls `skipStatement()`, discarding all file-level options. C++ protoc populates `FileOptions` in the descriptor. Result: missing options object, 19 vs 9 SourceCodeInfo locations.
- **Root cause:** `parser.go:867-868` — `parseFileOption` is a no-op stub that skips the entire statement.

### Run 4 — Field options (FAILED: 5/5 profiles)
- **Test:** `10_field_options` — proto3 message with `[deprecated = true]`, `[json_name = "userId"]`, `[packed = true]` on fields
- **Bug:** `skipBracketedOptions()` at line 400 discards all field options. C++ protoc populates `FieldOptions` (deprecated, json_name, packed) in the descriptor. Result: missing options, 25 vs 18 SourceCodeInfo locations.
- **Root cause:** `parser.go:399-401` — field options inside `[...]` are consumed but never stored on the `FieldDescriptorProto`.

### Run 5 — Import public (FAILED: 5/5 profiles)
- **Test:** `11_import_public` — three proto files: `base.proto` (defines Timestamp), `reexport.proto` (import public "base.proto", defines Wrapper using Timestamp), `main.proto` (imports reexport.proto, uses Timestamp transitively)
- **Bugs found (multiple):**
  1. `parseImport()` at lines 136-140 reads `public`/`weak` keyword but never sets `PublicDependency` or `WeakDependency` on FileDescriptorProto
  2. Cross-file type resolution broken: message types from imports resolve as TYPE_DOUBLE instead of TYPE_MESSAGE (Timestamp and Wrapper fields)
  3. SourceCodeInfo location counts differ (e.g., 11 vs 9 for reexport.proto, 18 vs 17 for main.proto)
  4. Descriptor set binary sizes differ (372 vs 331 bytes for descriptor_set, 902 vs 827 for full)
- **Root cause:** `parser.go:132-154` — `parseImport` discards the `public`/`weak` modifier. Type resolution in the descriptor pool fails to correctly resolve cross-file message references.

### Run 6 — Proto2 required/optional labels (FAILED: 5/5 profiles)
- **Test:** `12_proto2_required` — proto2 message with `required string`, `optional string`, `optional int32` with `[default = 25]`, `repeated string`
- **Bug:** `parseField()` at lines 363-371 only checks for `repeated` keyword. `required` and explicit `optional` are not recognized as labels. The parser treats `required` as a type name (message reference), then fails parsing: `expected "=", got "name"`. Go protoc-go crashes on valid proto2 input.
- **Root cause:** `parser.go:363-371` — parseField switch only handles `repeated`, defaults to `LABEL_OPTIONAL`. No handling of `required` or explicit `optional` keyword. Proto2 is fundamentally broken.

### Run 7 — Proto3 explicit optional (FAILED: 5/5 profiles)
- **Test:** `13_proto3_optional` — proto3 message with `optional string nickname = 2;` and `optional int32 age = 3;`
- **Bug:** Parser sets LABEL_OPTIONAL but never sets `proto3_optional = true` on FieldDescriptorProto. Also doesn't create synthetic oneofs (`_nickname`, `_age`) or set `oneof_index` on the fields. C++ protoc creates these. Also had to update `protoc-gen-dump` to advertise `FEATURE_PROTO3_OPTIONAL` so C++ protoc doesn't reject the request.
- **Root cause:** `parser.go:375-378` handles the `optional` keyword by setting label only. No `Proto3Optional` flag, no synthetic oneof creation. The descriptor pool also doesn't synthesize oneofs for proto3 optional fields.

### Run 8 — Extension ranges (FAILED: 5/5 profiles)
- **Test:** `14_extension_range` — proto2 message with `extensions 100 to 199;` and `extensions 1000 to max;`
- **Bug:** Parser line 238 skips `extensions` via `skipStatement()`. No `ExtensionRange` populated in DescriptorProto. C++ protoc includes them. SourceCodeInfo locations differ (30 vs 22).
- **Root cause:** `parser.go:238` — `extensions` is grouped with `option` in a skip case. Extension ranges are never parsed or stored.

### Run 9 — Enum options / allow_alias (FAILED: 5/5 profiles)
- **Test:** `15_enum_options` — proto3 enum with `option allow_alias = true;` and two values sharing number 1 (STARTED=1, RUNNING=1)
- **Bug:** `parseEnum()` at line 583-587 skips both `option` and `reserved` statements inside enums via `skipStatement()`. The `EnumOptions.allow_alias` field is never populated. C++ protoc includes it. SourceCodeInfo locations differ (26 vs 24) — the option statement's locations are missing.
- **Root cause:** `parser.go:583-587` — enum-level `option` is treated same as `reserved` and both are discarded by `skipStatement()`.

### Run 10 — Comments in SourceCodeInfo (FAILED: 4/5 profiles)
- **Test:** `16_comments` — proto3 file with leading comments on syntax, package, message, fields, plus a trailing comment and a leading detached comment
- **Bug:** Tokenizer `skipWhitespaceAndComments()` at lines 67-98 completely discards all comments. No comment text is ever captured. C++ protoc populates `leading_comments`, `trailing_comments`, and `leading_detached_comments` fields on SourceCodeInfo.Location entries. Binary CodeGeneratorRequest payloads differ (summaries match but binaries don't because comment strings are missing).
- **Root cause:** `tokenizer.go:67-98` — comments are consumed and thrown away during tokenization. The parser has no access to comment text to attach to SourceCodeInfo locations.

### Run 11 — Enum value options (FAILED: 5/5 profiles)
- **Test:** `17_enum_value_options` — proto3 enum with `PRIORITY_LOW = 1 [deprecated = true];`
- **Bug:** `skipBracketedOptions()` at line 631-633 discards enum value options. C++ protoc populates `EnumValueOptions.deprecated` on the `EnumValueDescriptorProto`. Go produces 17 SourceCodeInfo locations vs C++ protoc's 19.
- **Root cause:** `parser.go:631-633` — enum value options inside `[...]` are consumed but never stored on the `EnumValueDescriptorProto`.

### Run 12 — Message options (FAILED: 5/5 profiles)
- **Test:** `18_message_options` — proto3 messages with `option deprecated = true;` and `option deprecated = false;`
- **Bug:** `parseMessage()` at line 250-253 skips message-level `option` via `skipStatement()`. C++ protoc populates `MessageOptions.deprecated` in the `DescriptorProto`. Go produces 23 SourceCodeInfo locations vs C++ protoc's 27 — the option statement locations are missing.
- **Root cause:** `parser.go:250-253` — message-level `option` is discarded by `skipStatement()`. No `MessageOptions` are ever populated.

### Run 13 — Service and method options (FAILED: 5/5 profiles)
- **Test:** `19_service_options` — proto3 service with `option deprecated = true;` on the service, and a method with `option deprecated = true;` inside its body
- **Bug:** `parseService()` at line 869-873 skips service-level `option` via `skipStatement()`. `parseMethod()` at lines 957-970 skips method body content with depth tracking — options inside `{ ... }` are discarded. C++ protoc populates `ServiceOptions.deprecated` and `MethodOptions.deprecated`. Go produces 25 SourceCodeInfo locations vs C++ protoc's 29.
- **Root cause:** `parser.go:869-873` — service-level `option` is discarded by `skipStatement()`. `parser.go:957-970` — method body is consumed by brace-depth tracking without parsing option statements.

### Run 14 — Negative enum values (FAILED: 4/5 profiles)
- **Test:** `20_negative_enum` — proto3 enum with `TEMPERATURE_COLD = -1;` and `TEMPERATURE_FREEZING = -2;`
- **Bug:** `parseEnum()` at lines 669-672 consumes the `-` token separately but doesn't record its position. The source code info span for the enum value number (path `[5,0,2,N,2]`) starts at the digit column, not the `-` column. C++ protoc includes the `-` sign in the number span. Binary payloads differ by 1 column offset for each negative value.
- **Root cause:** `parser.go:669-672` — the minus token is consumed via `p.tok.Next()` but its `Column` is not saved. Lines 769-770 use `valNumTok.Line, valNumTok.Column` which misses the `-` prefix by 1 column.

### Run 15 — Enum reserved ranges/names (FAILED: 5/5 profiles)
- **Test:** `21_enum_reserved` — proto3 enum with `reserved 2, 3;`, `reserved 10 to 20;`, and `reserved "DELETED", "ARCHIVED";`
- **Bug:** `parseEnum()` at line 652-656 skips `reserved` via `skipStatement()`. No `EnumDescriptorProto.reserved_range` or `EnumDescriptorProto.reserved_name` populated. C++ protoc includes them. Descriptor set size differs (162 vs 124 bytes). SourceCodeInfo locations differ (28 vs 14).
- **Root cause:** `parser.go:652-656` — `reserved` inside enum is discarded by `skipStatement()`. Reserved ranges and reserved names are never parsed or stored.

### Run 16 — Fully qualified type names (FAILED: 5/5 profiles)
- **Test:** `22_fqn_type` — proto3 file with `message Inner` and `message Outer` referencing `.fqn.Inner` (leading dot = absolute path)
- **Bug:** `parseField()` at lines 537-550 reads the first token as the type. When type starts with `.`, `typeTok.Value` is `"."`. The loop at line 545 checks if next token is `.` but it's an identifier (`fqn`), so loop exits. `typeName` is just `"."`. Then `ExpectIdent()` consumes `fqn` as the field name. Then `Expect("=")` gets `.` instead of `=` → parse error.
- **Root cause:** `parser.go:537-550` — parseField doesn't handle leading `.` in type names. The tokenizer emits `.` as a separate TokenSymbol, but the parser only handles `.` *between* identifier parts (line 545), not at the start. Fully qualified type references (`.package.Type`) are a valid proto syntax that the Go parser cannot parse at all.

### Known gaps still unexplored (attack surface for future runs):
- **File-level options** (`option java_package`, `option go_package`, etc.) — TESTED in Run 3 (09_file_options), confirmed broken
- **Field options** (`deprecated = true`, `json_name`, `packed`, `jstype`) — TESTED in Run 4 (10_field_options), confirmed broken
- **Message options** — TESTED in Run 12 (18_message_options), confirmed broken (skipped at line 250)
- **Enum options** (`allow_alias`) — TESTED in Run 9 (15_enum_options), confirmed broken (skipped at line 583)
- **Extensions / extension ranges** — TESTED in Run 8 (14_extension_range), confirmed broken (parser skips `extensions` keyword)
- **Proto2 required/optional labels** — TESTED in Run 6 (12_proto2_required), confirmed broken (parser crashes on `required` keyword)
- **Proto2 groups** — not implemented at all
- **Proto2 default values** — not implemented (also exposed in Run 6 but parser crashes before reaching default parsing)
- **Comments in SourceCodeInfo** (leading/trailing) — TESTED in Run 10 (16_comments), confirmed broken (tokenizer discards all comments)
- **Service/method options** — TESTED in Run 13 (19_service_options), confirmed broken (service option skipped, method body options skipped)
- **Enum value options** — TESTED in Run 11 (17_enum_value_options), confirmed broken (skipBracketedOptions discards them)
- **`optional` keyword in proto3** (proto3 explicit optional) — TESTED in Run 7 (13_proto3_optional), confirmed broken (no proto3_optional flag, no synthetic oneofs)
- **`import public`** — TESTED in Run 5 (11_import_public), confirmed broken (PublicDependency not set + type resolution broken)
- **Streaming methods** — TESTED in Run 2 (08_streaming), confirmed broken
- **Negative enum values** source code info (the `-` token position) — TESTED in Run 14 (20_negative_enum), confirmed broken (span starts at digit, not `-`)
- **Multiple files in same testdata dir** (import resolution across files) — TESTED in Run 5 (works but exposes type resolution bugs)
- **Oneof options** — not tested (oneof-level options likely skipped)
- **Fully qualified type names** (`.package.Type`) — TESTED in Run 16 (22_fqn_type), confirmed broken (parser can't handle leading `.` in type names)
- **`extend` blocks** (proto2) — not handled in top-level parser (falls to default case, error)
- **Enum reserved ranges** — TESTED in Run 15 (21_enum_reserved), confirmed broken (skipStatement'd at line 652)

### Run 17 — Empty statements at file level (FAILED: 5/5 profiles)
- **Test:** `23_empty_statement` — proto3 file with standalone `;` (empty statements) between declarations
- **Bug:** Top-level parser switch at line 42-82 has no case for `";"`. The `;` token falls to the `default` case at line 80, which returns `unexpected token ";"`. C++ protoc treats standalone `;` as valid empty statements per the protobuf language spec (`emptyStatement = ";"`).
- **Root cause:** `parser.go:42-82` — file-level parser switch only handles `syntax`, `package`, `import`, `message`, `enum`, `service`, `option`. No handling for empty statements. Same issue likely exists inside message bodies (line 211-273) and enum bodies.

### Known gaps still unexplored (updated):
- **Empty statements inside message/enum/service bodies** — likely also broken (same missing `;` case)
- **Oneof options** — not tested (oneof-level options likely skipped at line 1253)
- **`extend` blocks** (proto2/proto3 custom options) — not handled at file level
- **Proto2 groups** — not implemented at all
- **Proto2 default values** — parser crashes before reaching defaults
- **String escape sequences** (`\n`, `\t`, `\x41`) — tokenizer at line 259-264 strips backslash but doesn't interpret escape codes (writes literal char after `\`)
- **String concatenation** (adjacent string literals `"abc" "def"`) — parser only reads one string token for option values
- **Trailing semicolons inside oneof bodies** — empty statements in oneof
