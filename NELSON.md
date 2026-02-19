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

### Run 18 — Empty statements inside message/enum/service bodies (FAILED: 5/5 profiles)
- **Test:** `24_empty_stmt_body` — proto3 file with `;` inside message body, enum body, and service body
- **Bug:** Message body parser (lines 214-277) has no `case ";"`. The `;` falls to the `default` case at line 261, which calls `parseField()`. `parseField` tries to interpret `;` as a type name and fails. C++ protoc allows empty statements inside all body types per the language spec (`emptyStatement = ";"`).
- **Root cause:** `parser.go:214-277` — message body switch handles `message`, `enum`, `oneof`, `map`, `reserved`, `option`, `extensions` but not `";"`. Same issue in enum body and service body parsers. File-level fix (Run 17, line 80-82) was not applied to inner body parsers.

### Run 19 — Message reserved "to max" (FAILED: 5/5 profiles)
- **Test:** `25_reserved_max` — proto3 message with `reserved 100 to max;`
- **Bug:** `parseMessageReserved()` at lines 340-353 handles `to` keyword but always calls `ExpectInt()` for the end value. Unlike `parseExtensionRange()` (line 408) which checks for `max`, the reserved range parser does not. When `max` (an identifier token) is encountered, `ExpectInt()` fails with "expected integer, got 'max'". C++ protoc accepts `reserved N to max;` and sets end to 536870912 (exclusive sentinel = 2^29).
- **Root cause:** `parser.go:340-353` — `parseMessageReserved` is missing the `if p.tok.Peek().Value == "max"` check that exists in `parseExtensionRange` at lines 408-415. The `max` keyword is only handled for extension ranges, not for message reserved ranges.

### Run 20 — String escape sequences (FAILED: 5/5 profiles)
- **Test:** `26_string_escape` — proto3 file with `option java_package = "com.example\ttest";` and `option go_package = "example.com/escape\ntest";`
- **Bug:** `readString()` at tokenizer.go:259-264 handles backslash escapes by stripping `\` and writing the literal next byte. So `\t` becomes literal `t`, `\n` becomes literal `n`. C++ protoc interprets escape sequences: `\t` → tab (0x09), `\n` → newline (0x0A). Binary CodeGeneratorRequest payloads differ because the option string values contain different bytes.
- **Root cause:** `tokenizer.go:259-264` — the escape handler does `sb.WriteByte(t.input[t.pos])` after consuming `\`, which writes the raw character instead of interpreting it as an escape code. Missing interpretation for `\n`, `\t`, `\r`, `\a`, `\b`, `\f`, `\v`, `\xNN`, `\NNN` (octal).

### Run 21 — Extend blocks (FAILED: 5/5 profiles)
- **Test:** `27_extend` — proto2 file with `message Extendable { extensions 100 to 200; }` and `extend Extendable { optional string nickname = 100; }`
- **Bug:** File-level parser switch at lines 42-85 has no case for `"extend"`. The `extend` token falls to the `default` case at line 83, which returns `unexpected token "extend"`. C++ protoc handles extend blocks and populates `FileDescriptorProto.extension`.
- **Root cause:** `parser.go:42-85` — file-level parser switch only handles `syntax`, `package`, `import`, `message`, `enum`, `service`, `option`, `";"`. No handling for `extend` blocks. The `extend` keyword is valid at file level (for defining extensions to messages) and inside message bodies (for nested extensions).

### Run 22 — Proto2 groups (FAILED: 5/5 profiles)
- **Test:** `28_proto2_group` — proto2 message with `repeated group Result = 1 { ... }` containing required/optional fields
- **Bug:** `parseField()` at lines 621-669 reads `group` as a type name (not a builtin, treated as message reference), then reads `Result` as the field name, `=` and `1` as the field number. Then `Expect(";")` at line 669 gets `{` instead, producing error: `expected ";", got "{"`. C++ protoc handles groups by creating both a nested DescriptorProto (for the group type) and a field (with TYPE_GROUP wire type).
- **Root cause:** `parser.go:591-710` — `parseField` has no `group` keyword handling. Groups require special parsing: they have a name (which becomes a nested message), a field number, and a message body delimited by `{ }`. The parser only handles regular field syntax (type, name, `=`, number, `;`).

### Run 23 — Negative default value SourceCodeInfo span (FAILED: 4/5 profiles)
- **Test:** `29_negative_default` — proto2 message with `optional int32 min_temp = 1 [default = -40];` and `optional int32 max_temp = 2 [default = 100];`
- **Bug:** `parseFieldOptions()` at line 1781-1784 consumes the `-` token for negative defaults but doesn't record its position. The source code info span for path `[7]` (default_value) starts at the digit column (42) instead of the minus column (41). C++ protoc span: `[5, 41, 44]`. Go span: `[5, 42, 44]`. Positive defaults are unaffected (row 18 matches: `[6, 41, 44]`).
- **Root cause:** `parser.go:1781-1784` — the minus token is consumed via `p.tok.Next()` but its column position is discarded. Line 1846-1847 uses `valTok.Column` (the number after minus) as the span start. Should save minus token position and use it as `startCol` when `negative == true`.

### Known gaps still unexplored (updated):
- **Empty statements inside oneof bodies** — likely also broken (same missing `;` case in parseOneof)
- **Oneof options** — not tested (oneof-level options likely skipped at line 1345-1349)
- **`extend` inside message bodies** — likely also not handled (same issue as file-level)
- **Proto2 default values** — proto2 fields now parse, but `[default = ...]` for enum-typed fields may not work; also negative float defaults likely have same span bug
- **String concatenation** (adjacent string literals `"abc" "def"`) — TESTED in Run 25 (31_string_concat), confirmed broken (parser reads one token, expects `;`)
- **Map field with enum value type** — `map<string, SomeEnum>` might resolve to TYPE_MESSAGE instead of TYPE_ENUM in the synthetic entry
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Weak imports** (`import weak "..."`) — TESTED in Run 24 (30_weak_import), confirmed broken (`WeakDependency` not populated, source code info missing)
- **Extension range options** (`extensions 100 to 199 [(my_option) = "foo"];`) — not handled
- **`group` inside oneof** — proto2 allows `oneof { group ... }`, same issue as regular groups
- **Proto2 groups** — TESTED in Run 22 (28_proto2_group), confirmed broken (parser has no group keyword handling)
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug as negative integers
- **Proto2 string default values with escape sequences** — span computation uses decoded string length + 2 for quotes, but doesn't account for multi-byte escape sequences in source (e.g., `\t` is 2 chars in source but 1 byte decoded)

### Run 24 — Weak imports (FAILED: 5/5 profiles)
- **Test:** `30_weak_import` — proto3 file with `import weak "base.proto";` referencing a base.proto with a Timestamp message
- **Bug:** `parseImport()` at lines 162-164 consumes the `weak` keyword but never sets `WeakDependency` on `FileDescriptorProto`. C++ protoc populates `weak_dependency` (field 11) with the dependency index. Also missing source code info for the weak keyword path `[11, N]`. Result: 15 vs 14 SourceCodeInfo locations, descriptor set 221 vs 219 bytes.
- **Root cause:** `parser.go:162-164` — `isWeak` is never tracked. After the `if isPublic` block (lines 182-187), there's no equivalent `if isWeak` block to set `fd.WeakDependency` or add source code info for path `[11, weakIdx]`.

### Run 25 — String concatenation (FAILED: 5/5 profiles)
- **Test:** `31_string_concat` — proto3 file with `option java_package = "com.example" ".concat";` and `option go_package = "example.com/" "concat/test";` (adjacent string literals)
- **Bug:** `parseFileOption()` at line 1651 reads ONE value token via `p.tok.Next()`, then line 1654 expects `;`. When the value is split across adjacent string literals (`"abc" "def"`), the parser reads `"abc"` and then fails with `expected ";", got ".concat"`. C++ protoc concatenates adjacent string literals into a single value per the protobuf language spec.
- **Root cause:** `parser.go:1651` — value reading uses a single `p.tok.Next()` call. No loop to check if the next token is also a string and concatenate. The tokenizer's `ExpectString()` also reads only one token. C++ protoc's parser uses `ConsumeString()` which loops over adjacent string tokens. This affects all contexts where string values are read: option values, import paths (though imports use single strings), default values, etc.

### Run 26 — Unhandled file option java_string_check_utf8 (FAILED: 5/5 profiles)
- **Test:** `32_unhandled_file_option` — proto3 file with `option java_string_check_utf8 = true;`
- **Bug:** `parseFileOption()` switch at lines 1676-1740 doesn't have a case for `java_string_check_utf8` (FileOptions field 27). The `default` case at line 1737-1739 does `return nil`, silently discarding the option. C++ protoc populates `FileOptions.java_string_check_utf8 = true`. Descriptor set size differs (92 vs 89 bytes). SourceCodeInfo locations differ (11 vs 9) — the option statement locations at paths `[8]` and `[8, 27]` are missing because `return nil` exits before the source code info code at lines 1742-1753.
- **Root cause:** `parser.go:1676-1740` — `parseFileOption` switch handles 16 standard options but is missing `java_string_check_utf8` (field 27). Any unrecognized option name hits the `default` case and is silently dropped. Other potentially missing standard options could also trigger this same pattern.

### Run 27 — Extend inside message body (FAILED: 5/5 profiles)
- **Test:** `33_nested_extend` — proto2 file with `message Container { extend Base { optional string tag = 100; } }`
- **Bug:** Message body parser switch (lines 228-304) has no `case "extend":`. The `extend` keyword falls to the `default` case, is treated as a field type name by `parseField`. `Base` is treated as the field name, then `Expect("=")` gets `{` instead → parse error: `expected "=", got "{"`. C++ protoc handles nested extend blocks and populates `DescriptorProto.extension` and `FileDescriptorProto.extension` correctly.
- **Root cause:** `parser.go:228-304` — message body switch handles `message`, `enum`, `oneof`, `map`, `reserved`, `option`, `extensions`, `";"` but not `extend`. Nested extend blocks require dedicated parsing: consume `extend ExtendedType { ... }`, parse fields inside, and store them on the containing message's `extension` field.

### Run 28 — String default value with escape sequences (FAILED: 4/5 profiles)
- **Test:** `34_string_default_escape` — proto2 message with `optional string greeting = 1 [default = "hello\tworld"];` and `optional string farewell = 2 [default = "good\nbye"];`
- **Bug:** `parseFieldOptions()` at line 1878-1881 computes the default value's SourceCodeInfo span end as `valTok.Column + len(valTok.Value) + 2`. For strings with escape sequences, `len(valTok.Value)` counts the *decoded* bytes (e.g., `\t` → 1 byte), but the source text is longer (e.g., `\t` is 2 characters in source). So the span end column is off by 1 for each escape sequence in the string. C++ protoc computes the span from actual source positions, so it correctly covers the full source string including escape sequences.
- **Root cause:** `parser.go:1878-1881` — `valEnd = valTok.Column + len(valTok.Value) + 2` doesn't account for the difference between decoded string length and source string length. Source `"hello\tworld"` is 14 chars, but decoded is 11 chars + 2 quotes = 13, off by 1.

### Known gaps still unexplored (updated):
- **Empty statements inside oneof bodies** — likely also broken (same missing `;` case in parseOneof)
- **Oneof options** — not tested (oneof-level options likely skipped at line 1485-1489)
- **Proto2 default values** — proto2 fields now parse, but `[default = ...]` for enum-typed fields may not work
- **Map field with enum value type** — `map<string, SomeEnum>` might resolve to TYPE_MESSAGE instead of TYPE_ENUM in the synthetic entry (but resolveMessageFields may fix it)
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug as negative integers
- **Other missing file options** — `java_generate_equals_and_hash` (20, deprecated), any other standard options not in the switch
- **Missing message/enum/service/method options** — similar pattern: only a few built-in options are in each switch
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]` — does it resolve correctly?
- **`extend` inside oneof** — proto2 allows group/extend inside oneof, same issues
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145` — span computation even more wrong (4 or 5 source chars → 1 decoded byte)
- **String default with multiple escapes** — each escape adds 1 char discrepancy, accumulating error

### Run 29 — Edition syntax (FAILED: 5/5 profiles)
- **Test:** `35_edition` — file with `edition = "2023";` instead of `syntax = "proto3";`
- **Bug:** File-level parser switch at lines 42-88 has no case for `"edition"`. The `edition` token falls to the `default` case at line 88, which returns `unexpected token "edition"`. C++ protoc v29.3 fully supports editions (`edition = "2023"`) and produces valid FileDescriptorProto with `edition` field (field 14) and `FeatureSet` entries.
- **Root cause:** `parser.go:42-88` — file-level parser switch only handles `syntax`, `package`, `import`, `message`, `enum`, `service`, `option`, `extend`, `";"`. No handling for `edition` keyword. Editions require: parsing `edition = "2023";`, setting `fd.Edition` field, and resolving feature defaults for the edition. Go protoc-go has zero edition support.
- **Also:** Updated `protoc-gen-dump` to advertise `FEATURE_SUPPORTS_EDITIONS` with `minimum_edition = EDITION_PROTO2` and `maximum_edition = EDITION_2023` so C++ protoc sends edition files to the dump plugin.

### Run 30 — Method idempotency_level option (FAILED: 5/5 profiles)
- **Test:** `36_idempotency_level` — proto3 service with two methods using `option idempotency_level = NO_SIDE_EFFECTS;` and `option idempotency_level = IDEMPOTENT;`
- **Bug:** `parseMethodOption()` at lines 1421-1427 only handles `deprecated` in its switch. The `default` case at line 1425-1426 does `return nil`, silently discarding `idempotency_level` (field 34 of MethodOptions). C++ protoc populates `MethodOptions.idempotency_level` with the enum value. Go produces 33 SourceCodeInfo locations vs C++ protoc's 37 — the 4 missing locations are for the two option statements (2 locations each: option container path + specific field path).
- **Root cause:** `parser.go:1421-1427` — `parseMethodOption` switch only handles `deprecated`. `idempotency_level` (and any other method option) hits the `default` case and is silently dropped. Same pattern as `parseMessageOption` (only handles `deprecated`), `parseServiceOption` (only handles `deprecated`).

### Run 31 — Oneof options (FAILED: 5/5 profiles)
- **Test:** `37_oneof_options` — proto3 message with `oneof payload { option deprecated = true; ... }` (without importing descriptor.proto)
- **Bug:** `parseOneof()` at lines 1607-1611 skips oneof-level `option` via `skipStatement()`. Go silently accepts the option and produces a valid descriptor (without the option populated). C++ protoc correctly rejects it with `Option "deprecated" unknown. Ensure that your proto definition file imports the proto which defines the option.` because `OneofOptions.deprecated` requires importing `descriptor.proto`.
- **Root cause:** `parser.go:1607-1611` — oneof-level `option` is silently discarded by `skipStatement()`. No validation is performed. Two bugs: (1) options are never stored on `OneofDescriptorProto.Options`, and (2) unknown options are not rejected. C++ protoc validates that the option name maps to a known field in the relevant options message.

### Run 32 — Float literal starting with dot (FAILED: 5/5 profiles)
- **Test:** `38_float_literal_dot` — proto2 message with `optional double ratio = 1 [default = .5];` and `optional float threshold = 2 [default = .25];`
- **Bug:** Tokenizer dispatch at `tokenizer.go:68` only starts `readNumber()` when `ch >= '0' && ch <= '9'`. A `.` character falls through to line 72-74 and is emitted as `TokenSymbol(".")`. The subsequent digits (e.g., `5`) are then read as a separate `TokenInt("5")`. So `.5` becomes two tokens instead of one `TokenFloat(".5")`. In `parseFieldOptions`, the default value `valTok` is `"."`, then when looking for `]` or `,`, it sees `5` → error: `expected ";", got "]"`. C++ protoc's tokenizer handles `.N` as a valid float literal per the protobuf grammar (`floatLit = "." decimals [ exponent ]`).
- **Root cause:** `tokenizer.go:68` — the character dispatch only considers `'0'-'9'` as number starters. The `.` case (which starts a float literal like `.5`, `.25`, `.001`) is not handled. The tokenizer needs to check if `.` is followed by a digit and call `readNumber()` in that case.

### Known gaps still unexplored (updated):
- **Empty statements inside oneof bodies** — C++ protoc also rejects these, so NOT a valid test (tested and discarded in Run 29)
- **Proto2 default values** — proto2 fields now parse, but `[default = ...]` for enum-typed fields may not work
- **Map field with enum value type** — tested in Run 29 prep, passes (type resolution works correctly)
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug as negative integers
- **Other missing file options** — `java_generate_equals_and_hash` (20, deprecated), any other standard options not in the switch
- **Missing message options** — `message_set_wire_format` (field 1), `no_standard_descriptor_accessor` (field 2), `map_entry` (field 7) — only `deprecated` handled
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]` — does it resolve correctly?
- **`extend` inside oneof** — proto2 allows group/extend inside oneof, same issues
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145` — span computation even more wrong
- **String default with multiple escapes** — each escape adds 1 char discrepancy, accumulating error
- **Edition features** — `edition = "2023"` with feature overrides on fields/messages/enums
- **Enum options beyond allow_alias** — `deprecated` on enum (field 3 of EnumOptions) — check if handled
- **Field option `unverified_lazy`** (field 15), `debug_redact` (field 16) — not in parseFieldOptions switch
- **Option validation** — Go silently accepts ANY option name without validation (tested in Run 31). Try completely bogus option names on messages/enums/fields/services/methods — Go will accept, C++ will reject
- **Float literals starting with `.`** — TESTED in Run 32 (38_float_literal_dot), confirmed broken (tokenizer can't handle `.5` as float)
- **`inf`/`nan` as default values** — TESTED in Run 33 (39_inf_nan_default), confirmed broken (Go normalizes to `+Inf`/`-Inf`/`NaN`, C++ stores `inf`/`-inf`/`nan`)
- **Exponent-only float** (`1e5`) — tokenizer handles `e`/`E` inside readNumber, should work but untested

### Run 33 — inf/nan default value normalization (FAILED: 5/5 profiles)
- **Test:** `39_inf_nan_default` — proto2 message with `optional double pos_inf = 1 [default = inf];`, `[default = -inf]`, `[default = nan]`, plus float variants
- **Bug:** `parseFieldOptions()` at lines 1942-1948 normalizes float/double defaults via `strconv.ParseFloat` + `strconv.FormatFloat`. For `inf`, Go produces `"+Inf"` (with leading `+` and capital `I`). For `-inf`, Go produces `"-Inf"` (capital `I`). For `nan`, Go produces `"NaN"` (capital `N` and `N`). C++ protoc stores these as `"inf"`, `"-inf"`, `"nan"` (all lowercase, no `+` prefix).
- **Root cause:** `parser.go:1942-1948` — `strconv.FormatFloat(v, 'g', -1, 64)` uses Go's default formatting for special float values: `+Inf`, `-Inf`, `NaN`. These don't match C++ protoc's `SimpleDtoa`/`SimpleFtoa` output which produces `inf`, `-inf`, `nan`. The normalization should special-case infinity and NaN to match C++ output.

### Run 34 — Map field options discarded (FAILED: 5/5 profiles)
- **Test:** `40_map_field_options` — proto3 message with `map<string, string> metadata = 1 [deprecated = true];` and `map<int32, string> labels = 2;`
- **Bug:** `parseMapField()` at line 1696-1698 uses `skipBracketedOptions()` to discard map field options, while `parseField()` at line 793-796 uses `parseFieldOptions()` to parse and store them. C++ protoc stores `FieldOptions.deprecated = true` on the map field. Go silently discards it. Result: 15 vs 13 SourceCodeInfo locations (missing options container and deprecated spans), descriptor set 283 vs 279 bytes (missing FieldOptions on the map field).
- **Root cause:** `parser.go:1696-1698` — `parseMapField` calls `p.skipBracketedOptions()` instead of `p.parseFieldOptions(field, fieldPath)`. The same options parsing logic used for regular fields should be used for map fields, but the map field code path has a completely separate (broken) handling.

### Run 35 — Proto3 explicit default values (FAILED: 5/5 profiles)
- **Test:** `41_proto3_default` — proto3 message with `int32 max_retries = 1 [default = 3];` and `string prefix = 2 [default = "test"];`
- **Bug:** Go protoc-go silently accepts `[default = ...]` on proto3 fields and stores the default value in the descriptor. C++ protoc rejects it with error: "Explicit default values are not allowed in proto3." The Go parser has zero proto3-specific validation — it never checks whether default values, required labels, or other proto2-only features are used inappropriately in proto3 files.
- **Root cause:** No validation layer exists in the Go implementation. C++ protoc validates proto3 constraints in `descriptor.cc` (the descriptor pool), but the Go `descriptor/pool.go` is an empty stub. The parser at `parseFieldOptions` (line 1942-1962) stores default values regardless of syntax version.

### Run 36 — Proto3 enum first value != 0 (FAILED: 5/5 profiles)
- **Test:** `42_proto3_enum_zero` — proto3 enum `Priority` with first value `HIGH = 1` (not 0), followed by `MEDIUM = 2` and `LOW = 3`, used in a message field
- **Bug:** Go protoc-go accepts the file and produces a valid descriptor (exit 0). C++ protoc rejects it with error: `test.proto:6:10: The first enum value must be zero for open enums.` (exit 1). The test harness detects exit code mismatch (C++ exit=1, Go exit=0).
- **Root cause:** No validation layer in Go implementation. C++ protoc validates proto3 constraints in `descriptor.cc` — specifically that the first enum value must be 0 for open enums (proto3 enums are open by default). The Go `descriptor/pool.go` is an empty stub with no validation. The parser accepts any enum value numbers without checking proto3 rules.

### Run 37 — Proto3 required fields (FAILED: 5/5 profiles)
- **Test:** `43_proto3_required` — proto3 message with `required string name = 1;` and `required int32 id = 2;`
- **Bug:** Go protoc-go silently accepts `required` in proto3 and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:12: Required fields are not allowed in proto3.` and `test.proto:7:12: Required fields are not allowed in proto3.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates proto3 constraints in `descriptor.cc` — `required` labels are prohibited in proto3 syntax. The Go `descriptor/pool.go` is an empty stub. The parser at `parseField` (line 730-734) accepts `required` regardless of syntax version.

### Run 38 — Reserved field number range 19000–19999 (FAILED: 5/5 profiles)
- **Test:** `44_reserved_field_number` — proto3 message with `string internal = 19000;` (field number in reserved range)
- **Bug:** Go protoc-go silently accepts field number 19000 and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto: Field numbers 19000 through 19999 are reserved for the protocol buffer library implementation.` (exit 1). The test harness detects exit code mismatch (C++ exit=1, Go exit=0).
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that field numbers 19000–19999 are reserved for the protobuf library implementation (used internally for extensions in descriptor.proto). The Go `descriptor/pool.go` is an empty stub with no field number validation. The parser accepts any int32 field number without checking reserved ranges.

### Known gaps still unexplored (updated):
- **Map field options source code info** — even if options are stored, the location ordering may differ from C++ protoc (map fields emit type/name/number in different positions)
- **Proto2 default values** — proto2 fields now parse, but `[default = ...]` for enum-typed fields may not work
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug as negative integers
- **Missing message options** — `message_set_wire_format` (field 1), `no_standard_descriptor_accessor` (field 2), `map_entry` (field 7) — only `deprecated` handled
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]` — does it resolve correctly?
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145` — span computation even more wrong
- **String default with multiple escapes** — each escape adds 1 char discrepancy, accumulating error
- **Edition features** — `edition = "2023"` with feature overrides on fields/messages/enums
- **Field option `unverified_lazy`** (field 15), `debug_redact` (field 16) — not in parseFieldOptions switch
- **Option validation** — Go silently accepts ANY option name without validation
- **Exponent-only float** (`1e5`) — tokenizer handles `e`/`E` inside readNumber, should work but untested
- **Oneof field options** — fields inside oneof parsed via `parseField`, so options should work, but untested
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];` — parser doesn't handle options after ranges
- **Proto3 validation gaps** — proto3 with `required` label TESTED in Run 37, reserved field numbers TESTED in Run 38. Proto3 with groups — likely also accepted by Go but rejected by C++.
- **Type name source code info with spaces** — `Outer . Inner` (spaces around dots) — Go computes span from concatenated string length, C++ uses actual token positions
- **Duplicate field numbers** — TESTED in Run 39 (45_duplicate_field_number), confirmed broken (Go accepts, C++ rejects)
- **Field number 0** — Go likely accepts, C++ rejects (field numbers must be positive)
- **Field number > 2^29-1** — TESTED in Run 41 (47_field_number_max), confirmed broken (Go accepts, C++ rejects)

### Run 39 — Duplicate field numbers (FAILED: 5/5 profiles)
- **Test:** `45_duplicate_field_number` — proto3 message with two fields both using field number 1 (`string name = 1;` and `int32 id = 1;`)
- **Bug:** Go protoc-go silently accepts duplicate field numbers and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:14: Field number 1 has already been used in "dupfield.User" by field "name".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that each field number is unique within a message. The Go `descriptor/pool.go` is an empty stub with no duplicate field number checking. The parser stores all fields regardless of number conflicts.

### Run 40 — Field number zero (FAILED: 5/5 profiles)
- **Test:** `46_field_number_zero` — proto3 message with `string name = 0;` (field number 0)
- **Bug:** Go protoc-go silently accepts field number 0 and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:17: Field numbers must be positive integers.` and `Suggested field numbers for zerof.Config: 2` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that field numbers must be positive (>= 1). The Go `descriptor/pool.go` is an empty stub with no field number range validation. The parser accepts any integer as a field number without checking validity.

### Run 41 — Field number exceeds max (FAILED: 5/5 profiles)
- **Test:** `47_field_number_max` — proto3 message with `string name = 536870912;` (field number 2^29, exceeds max of 536870911)
- **Bug:** Go protoc-go silently accepts field number 536870912 and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:17: Field numbers cannot be greater than 536870911.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that field numbers must be <= 536870911 (2^29-1). The Go `descriptor/pool.go` is an empty stub with no field number upper bound validation. The parser accepts any integer as a field number without range checking.

### Run 42 — Duplicate enum value numbers without allow_alias (FAILED: 5/5 profiles)
- **Test:** `48_duplicate_enum_number` — proto3 enum with `ACTIVE = 1` and `ENABLED = 1` (same number, no `allow_alias`)
- **Bug:** Go protoc-go silently accepts duplicate enum value numbers and produces a valid descriptor (exit 0). C++ protoc rejects with: `"dupenum.ENABLED" uses the same enum value as "dupenum.ACTIVE". If this is intended, set 'option allow_alias = true;' to the enum definition.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that enum values sharing the same number require `option allow_alias = true`. The Go `descriptor/pool.go` is an empty stub with no duplicate enum value checking. The parser stores all enum values regardless of number conflicts.

### Run 43 — Duplicate message names (FAILED: 5/5 profiles)
- **Test:** `49_duplicate_message_name` — proto3 file with two `message User` declarations (different fields)
- **Bug:** Go protoc-go silently accepts duplicate message names and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:9:9: "User" is already defined in "dupname".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that each message name is unique within a package/scope. The Go `descriptor/pool.go` is an empty stub with no duplicate name checking. The parser stores all message declarations regardless of name conflicts.

### Known gaps still unexplored (updated):
- **Proto3 with groups** — `repeated group Foo = 1 { }` in proto3 — Go likely accepts, C++ rejects with "Group syntax is not supported in proto3."
- **Map field options source code info** — even if options are stored, the location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug as negative integers
- **Missing message options** — `message_set_wire_format` (field 1), `no_standard_descriptor_accessor` (field 2), `map_entry` (field 7)
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]` — does it resolve correctly?
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145` — span computation even more wrong
- **Edition features** — `edition = "2023"` with feature overrides
- **Field option `unverified_lazy`/`debug_redact`** — not in parseFieldOptions switch
- **Option validation** — Go silently accepts ANY option name without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Duplicate enum value numbers** — TESTED in Run 42 (48_duplicate_enum_number), confirmed broken (no allow_alias validation)
- **Duplicate message/enum names** — TESTED in Run 43 (49_duplicate_message_name), confirmed broken (no duplicate name checking)
- **Self-referencing message** — `message Foo { Foo child = 1; }` — should work but type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Duplicate enum names** — same as message names, Go likely accepts duplicate enum declarations
- **Duplicate field names** — TESTED (both C++ and Go reject identically — NOT a gap)
- **Proto2 fields without explicit labels** — TESTED in Run 44 (50_proto2_no_label), confirmed broken (Go accepts, C++ rejects)
- **Map fields inside oneofs** — C++ rejects, Go likely accepts (no validation)
- **Self-import / circular import** — cycle detection at importer level, may differ
- **Proto file with no syntax statement** — C++ defaults to proto2 with warning, Go defaults to empty syntax

### Run 44 — Proto2 fields without explicit labels (FAILED: 5/5 profiles)
- **Test:** `50_proto2_no_label` — proto2 message with `string name = 1;` and `int32 count = 2;` (no `required`/`optional`/`repeated` label)
- **Bug:** Go protoc-go silently accepts fields without labels in proto2 and defaults to `LABEL_OPTIONAL` (exit 0). C++ protoc rejects with: `Expected "required", "optional", or "repeated".` for each field (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:729-751` — `parseField` label switch defaults to `LABEL_OPTIONAL` when no label keyword is found, regardless of syntax version. No proto2 validation requires explicit labels. C++ protoc's parser requires explicit labels in proto2 (`ParseMessageField` checks for label keywords and errors if missing).

### Run 45 — Map fields inside oneofs (FAILED: 5/5 profiles)
- **Test:** `51_map_in_oneof` — proto3 message with `oneof payload { string text = 1; map<string, string> metadata = 2; }`
- **Bug:** Go protoc-go silently accepts map fields inside oneofs and produces a valid descriptor (exit 0). C++ protoc rejects with an error about map fields not being allowed in oneofs (exit 1). The `parseOneof` function at line 1624 doesn't check for `"map"` keyword — it falls through to `parseField` which treats `map` as a message type reference name and somehow parses the rest.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that map fields are not allowed inside oneofs. The Go `descriptor/pool.go` is an empty stub with no such validation. The parser doesn't distinguish map fields from regular fields inside oneof bodies.

### Known gaps still unexplored (updated):
- **Proto3 with groups** — `repeated group Foo = 1 { }` in proto3 — Go likely accepts, C++ rejects
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `message_set_wire_format`, `no_standard_descriptor_accessor`, `map_entry`
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]`
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Field option `unverified_lazy`/`debug_redact`** — not in parseFieldOptions switch
- **Option validation** — Go silently accepts ANY option name without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Duplicate enum names** — Go likely accepts duplicate enum declarations
- **Self-import / circular import** — cycle detection may differ
- **Proto file with no syntax statement** — C++ defaults to proto2 with warning, Go may differ
- **Map fields inside oneofs** — TESTED in Run 45 (51_map_in_oneof), confirmed broken (Go accepts, C++ rejects)
- **Duplicate service method names** — TESTED locally, Go now validates (cli.go:922-926), both reject identically — NOT a gap

### Run 46 — Labeled fields inside oneofs (FAILED: 5/5 profiles)
- **Test:** `52_oneof_label` — proto3 message with `oneof payload { repeated string tags = 1; int32 count = 2; }` (repeated label on field inside oneof)
- **Bug:** Go protoc-go silently accepts labeled fields inside oneofs and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:5: Fields in oneofs must not have labels (required / optional / repeated).` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parseOneof()` at line 1636 calls `parseField()` which accepts all labels (required/optional/repeated) without checking if the field is inside a oneof. No validation exists in `compiler/cli/cli.go` for this constraint. C++ protoc validates in `descriptor.cc` that fields within oneofs cannot have explicit labels.

### Run 47 — No syntax statement (FAILED: 5/5 profiles)
- **Test:** `53_no_syntax` — file with no `syntax` statement, just `package nosyntax;` and a message with unlabeled fields (`string name = 1;`)
- **Bug:** Go protoc-go silently accepts the file and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:4:3: Expected "required", "optional", or "repeated".` for each unlabeled field (exit 1). C++ treats files without a syntax statement as proto2, which requires explicit labels on all fields.
- **Root cause:** `parser.go` — when no `syntax` statement is present, `p.syntax` is `""` (empty string). The proto2 label validation at line 762 (`if p.syntax == "proto2"`) doesn't fire because `"" != "proto2"`. Go treats no-syntax files as proto3-like (no labels required), while C++ correctly defaults to proto2 semantics. The parser should default `p.syntax = "proto2"` when no syntax statement is encountered.

### Known gaps still unexplored (updated):
- **Proto3 with groups** — `repeated group Foo = 1 { }` in proto3 — Go likely accepts, C++ rejects
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `message_set_wire_format`, `no_standard_descriptor_accessor`, `map_entry`
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]`
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Field option `unverified_lazy`/`debug_redact`** — not in parseFieldOptions switch
- **Option validation** — Go silently accepts ANY option name without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Self-import / circular import** — cycle detection may differ
- **No syntax statement** — TESTED in Run 47 (53_no_syntax), confirmed broken (Go accepts, C++ rejects)
- **Oneof with optional label** — `optional string name = 1;` inside oneof — C++ rejects, Go likely accepts
- **Reserved field name conflicts** — TESTED in Run 48 (54_reserved_name_conflict), confirmed broken (Go accepts, C++ rejects)
- **Extension number out of range** — extension using number outside declared range — C++ validates, Go likely doesn't
- **Reserved field number conflicts** — TESTED in Run 49 (55_reserved_number_conflict), confirmed broken (Go accepts, C++ rejects)
- **Proto3 with groups** — `repeated group Foo = 1 { }` in proto3 — Go likely accepts, C++ rejects

### Run 48 — Reserved field name conflicts (FAILED: 5/5 profiles)
- **Test:** `54_reserved_name_conflict` — proto3 message with `reserved "email", "phone";` and a field `string email = 2;` that uses a reserved name
- **Bug:** Go protoc-go silently accepts a field whose name is declared as reserved and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:8:10: Field name "email" is reserved.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that field names don't conflict with reserved names declared in the same message. The Go `descriptor/pool.go` is an empty stub with no reserved name checking. The parser stores both the reserved names and the conflicting field without any cross-validation.

### Run 49 — Reserved field number conflicts (FAILED: 5/5 profiles)
- **Test:** `55_reserved_number_conflict` — proto3 message with `reserved 3, 5 to 10;` and a field `int32 count = 3;` that uses a reserved number
- **Bug:** Go protoc-go silently accepts a field whose number is declared as reserved and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:12: Field "count" uses reserved number 3.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that field numbers don't conflict with reserved ranges declared in the same message. The Go `descriptor/pool.go` is an empty stub with no reserved number checking. The parser stores both the reserved ranges and the conflicting field without any cross-validation.

### Run 50 — Map field with invalid key type (FAILED: 5/5 profiles)
- **Test:** `56_map_invalid_key` — proto3 message with `map<double, string> scores = 1;` (double as map key type)
- **Bug:** Go protoc-go silently accepts `double` as a map key type and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:3: Key in map fields cannot be float/double, bytes or message types.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1672-1676` — `parseMapField` checks if the key type is in `builtinTypes` (which includes all 15 scalar types including `double`, `float`, and `bytes`), but never validates that the key type is actually allowed for map fields. C++ protoc validates in `descriptor.cc` that map keys can only be integral types (int32/int64/uint32/uint64/sint32/sint64/fixed32/fixed64/sfixed32/sfixed64), bool, or string — NOT double, float, or bytes. The Go parser accepts any builtin type as a map key without checking the restriction.

### Run 51 — Enum value scope conflict missing note (FAILED: 5/5 profiles)
- **Test:** `57_enum_scope_conflict` — proto3 file with two enums `Color` and `Priority` both defining `UNKNOWN = 0;` in the same package scope
- **Bug:** Go protoc-go correctly detects the conflict and errors with `"UNKNOWN" is already defined in "enumscope".` (exit 1). However, C++ protoc emits TWO error lines: the same message PLUS a second line: `Note that enum values use C++ scoping rules, meaning that enum values are siblings of their type, not children of it. Therefore, "UNKNOWN" must be unique within "enumscope", not just within "Priority".` Go is missing this explanatory note. The test harness detects error message mismatch.
- **Root cause:** `compiler/cli/cli.go` — the duplicate symbol validation emits only one error line. C++ protoc's `descriptor.cc` emits an additional explanatory note about C++ scoping rules for enum values. The Go implementation is missing this second diagnostic message.

### Run 52 — Empty enum (FAILED: 5/5 profiles)
- **Test:** `58_empty_enum` — proto3 file with `enum Status {}` (no enum values)
- **Bug:** Go protoc-go silently accepts an empty enum and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:5:6: Enums must contain at least one value.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that enums must have at least one value. The Go `descriptor/pool.go` is an empty stub with no enum value count validation. The parser accepts empty enum bodies without checking.

### Run 53 — Proto3 with groups (FAILED: 5/5 profiles)
- **Test:** `59_proto3_group` — proto3 message with `repeated group Result = 1 { string url = 1; string title = 2; }`
- **Bug:** Go protoc-go silently accepts groups in proto3 and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:12: Groups are not supported in proto3 syntax.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that groups are not allowed in proto3. The Go parser has group parsing support (`isGroupField` + `parseGroupField`) but never checks the syntax version. The `parseGroupField` function works identically for proto2 and proto3. The Go `descriptor/pool.go` is an empty stub with no proto3 constraint validation.

### Run 54 — Empty oneof (FAILED: 5/5 profiles)
- **Test:** `60_empty_oneof` — proto3 message with `oneof payload {}` (empty oneof body, no fields)
- **Bug:** Go protoc-go silently accepts an empty oneof and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:8:3: Expected type name.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parseOneof()` — the oneof body parsing loop terminates cleanly when `}` is immediately encountered. No validation checks that at least one field exists inside the oneof. C++ protoc's parser expects at least one type name token inside a oneof body. The Go `descriptor/pool.go` is an empty stub with no oneof validation.

### Known gaps still unexplored (updated):
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `message_set_wire_format`, `no_standard_descriptor_accessor`, `map_entry`
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]`
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Field option `unverified_lazy`/`debug_redact`** — not in parseFieldOptions switch
- **Option validation** — Go silently accepts ANY option name without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Self-import / circular import** — cycle detection may differ
- **Oneof with optional label** — `optional string name = 1;` inside oneof — C++ rejects, Go likely accepts
- **Extension number out of range** — extension using number outside declared range — C++ validates, Go likely doesn't
- **Map key type `bytes`** — same issue as double, `map<bytes, string>` accepted by Go, rejected by C++
- **Map key type `float`** — same issue
- **Duplicate field names across message and enum** — enum value `FOO` + field `FOO` in same scope may conflict differently
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **Empty oneof** — TESTED in Run 54 (60_empty_oneof), confirmed broken (Go accepts, C++ rejects)
- **Duplicate syntax statements** — TESTED in Run 55 (61_duplicate_syntax), confirmed broken (Go accepts, C++ rejects)
- **Duplicate package statements** — `package foo; package bar;` — C++ likely rejects, Go likely accepts
- **Oneof with optional label** — `optional string name = 1;` inside oneof — C++ rejects, Go likely accepts

### Run 55 — Duplicate syntax statements (FAILED: 5/5 profiles)
- **Test:** `61_duplicate_syntax` — proto3 file with two `syntax = "proto3";` statements followed by a package and message
- **Bug:** Go protoc-go silently accepts duplicate syntax statements and produces a valid descriptor (exit 0). C++ protoc rejects the second `syntax` with: `test.proto:2:1: Expected top-level statement (e.g. "message").` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:54-57` — the `case "syntax"` in the file-level parser switch calls `parseSyntax()` every time, which just overwrites `p.syntax` and `fd.Syntax`. No flag tracks whether syntax has already been set. C++ protoc only allows `syntax` as the very first statement in a file — after it's been parsed, the parser no longer accepts it as a valid top-level statement.

### Run 56 — Duplicate package statements (FAILED: 5/5 profiles)
- **Test:** `62_duplicate_package` — proto3 file with `package dupkg;` followed by `package dupkg2;` then a message
- **Bug:** Go protoc-go silently accepts duplicate package statements and produces a valid descriptor (exit 0). C++ protoc rejects the second `package` with: `test.proto:5:1: Expected top-level statement (e.g. "message").` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:69-72` — the `case "package"` in the file-level parser switch calls `parsePackage()` every time, which just overwrites `fd.Package` at line 209. No flag tracks whether package has already been set. C++ protoc only allows `package` before any definitions — after it and `syntax` are parsed, the parser no longer accepts them as valid top-level statements. Same pattern as the duplicate syntax bug (Run 55).

### Run 57 — Late syntax statement (FAILED: 5/5 profiles)
- **Test:** `63_late_syntax` — file with `package latesyntax;` BEFORE `syntax = "proto3";`, followed by a message with unlabeled fields
- **Bug:** Go protoc-go silently accepts `syntax` after `package` and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:2:1: Expected top-level statement (e.g. "message").` plus `Expected "required", "optional", or "repeated".` for each unlabeled field (exit 1). C++ only allows `syntax` as the very first statement — it defaults to proto2 when syntax isn't first, then `syntax` is not a valid top-level keyword, then unlabeled fields are invalid in proto2.
- **Root cause:** `parser.go:52-112` — the file-level switch handles `syntax` at any position (line 56-62). The only guard is `if p.syntaxParsed` (line 57), which prevents duplicate syntax but not late syntax. C++ protoc handles `syntax` separately before the main loop — `ParseSyntaxIdentifier` is called once at the start, then the main `ParseTopLevelStatement` loop doesn't include `syntax` as a valid keyword. The Go parser should only allow `syntax`/`edition` as the very first statement.

### Run 58 — Octal integer default values (FAILED: 5/5 profiles)
- **Test:** `64_octal_default` — proto2 message with `optional int32 mode = 1 [default = 0755];`, `[default = 0644]`, `[default = 0777]`
- **Bug:** Go protoc-go stores default values as the raw token text: `"0755"`, `"0644"`, `"0777"`. C++ protoc parses octal literals and stores the decimal representation: `"493"`, `"420"`, `"511"`. Binary CodeGeneratorRequest payloads differ (59 vs 40 bytes for the default value strings), and descriptor set sizes differ (122 vs 119 bytes).
- **Root cause:** `parser.go:2008-2028` — `case "default"` stores `valTok.Value` (the raw token text) directly as `field.DefaultValue`. C++ protoc parses the integer literal (respecting `0x` hex and `0` octal prefixes) and formats it as a decimal string via `SimpleItoa`. The Go parser should use `strconv.ParseInt(valTok.Value, 0, 64)` to parse the integer and then `strconv.FormatInt` to produce the decimal string. Same bug would affect hex default values like `[default = 0x1F]` → Go stores `"0x1F"`, C++ stores `"31"`.
- **Also tried:** late import (import after message definition) — both C++ and Go accept it, NOT a gap.

### Known gaps still unexplored (updated):
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Deeply nested messages (5+ levels)** — source code info path correctness at depth
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `message_set_wire_format`, `no_standard_descriptor_accessor`, `map_entry`
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]`
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Field option `unverified_lazy`/`debug_redact`** — not in parseFieldOptions switch
- **Option validation** — Go silently accepts ANY option name without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Self-import / circular import** — cycle detection may differ
- **Import after definitions** — TESTED, both accept it — NOT a gap
- **Map key type `bytes`/`float`** — accepted by Go, rejected by C++
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **Proto2 enum default values** — `[default = SOME_ENUM_VALUE]` — does it resolve correctly?
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug as negative integers
- **Hex default values** — `[default = 0x1F]` — same bug as octal defaults (raw text vs decimal)
- **Octal default values** — TESTED in Run 58 (64_octal_default), confirmed broken (raw text vs decimal)

### Run 59 — String concatenation in default values (FAILED: 5/5 profiles)
- **Test:** `65_string_concat_default` — proto2 message with `optional string greeting = 1 [default = "hello" " world"];` and `optional string farewell = 2 [default = "goodbye"];`
- **Bug:** `parseFieldOptions()` at line 2001 reads `valTok = p.tok.Next()` — a single token `"hello"`. The next token `" world"` is not consumed/concatenated. The parser then expects `;` or `,` or `]` but sees `" world"`, causing error: `expected ";", got "]"` (cascading parse failure). C++ protoc concatenates adjacent string literals into a single value per the protobuf language spec.
- **Root cause:** `parser.go:2001` — `parseFieldOptions` reads only one token for the option value. The string concatenation fix from commit 6fd286e was only applied to `parseFileOption` (file-level options), NOT to `parseFieldOptions` (field-level options). Same bug exists for import paths (though imports typically use single strings), and enum value options. The fix pattern — `for p.tok.Peek().Type == tokenizer.TokenString { ... }` — needs to be applied everywhere string values are read.
- **Also tried:** map entry name with digits (`items2get`) — BOTH compilers produce `Items2getEntry`, NOT a gap (C++ toCamelCase matches Go).

### Run 60 — Message option no_standard_descriptor_accessor (FAILED: 5/5 profiles)
- **Test:** `66_message_option_accessor` — proto3 message with `option no_standard_descriptor_accessor = true;`
- **Bug:** `parseMessageOption()` switch at lines 748-753 only handles `deprecated` (field 3). The `default` case at line 752-753 does `return nil`, silently discarding `no_standard_descriptor_accessor` (field 2 of MessageOptions). But at line 743-745, `msg.Options` is set to `&descriptorpb.MessageOptions{}` BEFORE the switch — so the message gets an empty non-nil MessageOptions. C++ protoc stores `MessageOptions{no_standard_descriptor_accessor: true}`. Binary descriptor set: 86 bytes (C++) vs 84 bytes (Go). SourceCodeInfo locations: 15 (C++) vs 13 (Go) — missing the option statement locations.
- **Root cause:** `parser.go:748-753` — `parseMessageOption` switch only handles `deprecated`. Standard options `message_set_wire_format` (field 1), `no_standard_descriptor_accessor` (field 2), and `map_entry` (field 7) all hit the `default` case and are silently dropped. Additionally, `msg.Options` is unconditionally initialized to an empty MessageOptions before the switch, leaving a spurious empty options object even when the option value is discarded.

### Run 61 — Duplicate oneof names (FAILED: 5/5 profiles)
- **Test:** `67_duplicate_oneof` — proto3 message with two `oneof payload { ... }` blocks (same name, different fields)
- **Bug:** Both C++ and Go reject the duplicate oneof, but the error message format differs. C++ protoc: `test.proto: "payload" is already defined in "duponeof.Request".` (no line/column). Go protoc-go: `test.proto:9:9: "payload" is already defined in "duponeof.Request".` (with line:column). The test harness detects error message mismatch.
- **Root cause:** Go's duplicate name detection (likely in `compiler/cli/cli.go`) includes line and column numbers in the error, while C++ protoc's `descriptor.cc` omits position info for duplicate symbol errors. The error text itself matches, but the position prefix format differs.

### Run 62 — Type name source code info with spaces around dots (FAILED: 4/5 profiles)
- **Test:** `68_type_name_spaces` — proto3 message with `spacetype .  Inner ref = 1;` (spaces around dots in type reference)
- **Bug:** `parseField()` at line 875 computes `typeNameEnd = typeStartCol + len(field.GetTypeName())`. For `spacetype .  Inner`, `field.GetTypeName()` is `"spacetype.Inner"` (15 chars), but the actual source text spans more columns due to spaces around the dot (20 chars). C++ protoc records the span from the first token's start to the last token's end, correctly covering the wider range. Go computes end as `typeStartCol + 15 = 17`, C++ computes end as `20`. Binary diff: byte `0x14` (20) in C++ vs `0x11` (17) in Go at the type_name span.
- **Root cause:** `parser.go:875` — `typeNameEnd` is computed from `len(field.GetTypeName())` which is the concatenated identifier string (no spaces), not the actual source text span. The parser consumes `.` and subsequent identifier tokens in the loop at lines 819-823 but doesn't track the position of the last consumed token for span computation. Fix: save the last token's end position (e.g., `part.Column + len(part.Value)`) and use it as `typeNameEnd`.

### Run 63 — Self-import / circular import (FAILED: 5/5 profiles)
- **Test:** `69_self_import` — proto3 file with `import "test.proto";` importing itself
- **Bug:** Go protoc-go silently accepts the self-import and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:3:1: File recursively imports itself: test.proto -> test.proto` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parseRecursive()` in `compiler/cli/cli.go:326-355` checks if a file is already in the `parsed` map (line 327) and returns nil if so. For self-import, the file adds itself to `parsed` at line 344 before processing dependencies at line 347. When the self-import dependency is encountered, it's already in `parsed`, so it returns nil — no error. C++ protoc's `Importer` tracks "currently being imported" files separately from "already imported" files, detecting cycles in the import chain.

### Run 64 — Circular import (two files) (FAILED: 5/5 profiles)
- **Test:** `70_circular_import` — two proto3 files: `a.proto` imports `b.proto`, `b.proto` imports `a.proto` (mutual circular import)
- **Bug:** Both C++ and Go detect the cycle and reject with exit code 1, but error messages differ significantly. C++ protoc produces 5 error lines (cycle detection + "not found or had errors" + unresolved types for both files). Go protoc-go produces only 1 error line (just the cycle detection for `b.proto`). C++ reports the cycle on `a.proto:5:1`, Go reports it on `b.proto:5:1`. C++ continues to report cascading errors (unresolved imports/types), Go stops after the first cycle error.
- **Root cause:** `compiler/cli/cli.go:326-355` — `parseRecursive` detects the cycle correctly but returns a single error and stops. C++ protoc's import resolution continues processing after cycle detection, generating additional error messages for unresolved imports and undefined types. The Go implementation short-circuits on the first error rather than continuing to collect all errors.

### Run 65 — Float default value normalization (FAILED: 5/5 profiles)
- **Test:** `71_float_precision` — proto2 message with `optional double ratio = 1 [default = 1e10];`, `[default = 1e-6]`, `[default = 0.333333333333333]`
- **Bug:** Go's `strconv.FormatFloat(v, 'g', -1, 64)` formats `1e10` as `"1e+10"` (scientific notation with `+` sign, 5 chars). C++ protoc's `SimpleDtoa` formats it as `"10000000000"` (fully expanded decimal, 11 chars). Binary CodeGeneratorRequest payloads differ because the default_value strings have different representations.
- **Root cause:** `parser.go:2048-2049` — `strconv.FormatFloat(v, 'g', -1, 64)` uses Go's default '%g' formatting which differs from C++ `SimpleDtoa`. Go's `'g'` format uses scientific notation for large exponents (e.g., `1e+10`), while C++ `SimpleDtoa` uses `DoubleToBuffer` which expands to full decimal notation for values that fit within 15 significant digits. The fix would need to replicate C++ `SimpleDtoa` behavior, which avoids scientific notation when the expanded form has fewer than ~15 digits.
- **Also tried:** Hex default values (`[default = 0x1F]`) — passes now (already fixed in commit f6c5378). Diamond imports (A→B,C→D) — passes (file ordering matches). Deeply nested messages (6 levels) — passes. Enum default values (`[default = HIGH]`) — passes. Map key type `bytes` — passes (already fixed in commit 8c68c03).

### Run 66 — Proto3 extension ranges (FAILED: 5/5 profiles)
- **Test:** `72_proto3_extensions` — proto3 message with `extensions 100 to 200;` (extension ranges not allowed in proto3)
- **Bug:** Go protoc-go silently accepts extension ranges in proto3 and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:14: Extension ranges are not allowed in proto3.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `collectProto3MessageErrors()` in `compiler/cli/cli.go:1152-1165` validates groups, required fields, default values, and enum zero values, but does NOT check for extension ranges. C++ protoc validates in `descriptor.cc` that extension ranges are prohibited in proto3. The Go parser at `parseExtensionRange` (line 522) accepts extension ranges regardless of syntax version, and no post-parse validation catches this.

### Run 67 — Proto3 extend blocks (FAILED: 5/5 profiles)
- **Test:** `73_proto3_extend` — proto3 file with `extend Extendable { string tag = 100; }` where Extendable is defined in a proto2 import
- **Bug:** Go protoc-go silently accepts extend blocks in proto3 files and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:8: Extensions in proto3 are only allowed for defining options.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `validateProto3()` in `compiler/cli/cli.go:1046-1061` checks messages and enums for proto3 constraints but never checks `fd.GetExtension()` for file-level extend blocks. C++ protoc validates in `descriptor.cc` that extensions in proto3 files are only allowed for defining options (custom options that extend `google.protobuf.*Options`). The Go parser handles `extend` blocks syntactically but no post-parse validation catches proto3 extend usage.

### Run 68 — Missing file option php_generic_services (FAILED: 5/5 profiles)
- **Test:** `74_php_generic_services` — proto3 file with `option php_generic_services = true;` and a service with one RPC method
- **Bug:** `parseFileOption()` switch at lines 1886-1949 doesn't have a case for `php_generic_services` (field 42 of FileOptions). The `default` case at line 1950-1952 does `return nil`, silently discarding the option. C++ protoc populates `FileOptions.php_generic_services = true`. Binary descriptor sizes differ (68 vs 40 bytes for plugin). SourceCodeInfo locations also differ — missing the option statement locations at paths `[8]` and `[8, 42]`.
- **Root cause:** `parser.go:1886-1949` — `parseFileOption` switch handles 17 standard options but is missing `php_generic_services` (field 42). The default case silently drops any unrecognized option. Same pattern as Run 26 (`java_string_check_utf8`). Other potentially missing standard options: `java_generate_equals_and_hash` (field 20, deprecated).

### Run 69 — Message option message_set_wire_format (FAILED: 5/5 profiles)
- **Test:** `75_message_set_wire_format` — proto2 message with `option message_set_wire_format = true;` and `extensions 4 to max;`
- **Bug:** `parseMessageOption()` switch at lines 754-762 only handles `deprecated` (field 3) and `no_standard_descriptor_accessor` (field 2). The `default` case at line 761-762 does `return nil`, silently discarding `message_set_wire_format` (field 1 of MessageOptions). C++ protoc stores `MessageOptions{message_set_wire_format: true}`. Go produces 16 SourceCodeInfo locations vs C++ protoc's 18 — missing the option statement locations.
- **Root cause:** `parser.go:754-762` — `parseMessageOption` switch is missing `message_set_wire_format` (field 1) and `map_entry` (field 7). Same pattern as all other missing option bugs.

### Run 70 — Field option debug_redact (FAILED: 5/5 profiles)
- **Test:** `76_field_debug_redact` — proto3 message with `string email = 2 [debug_redact = true];`
- **Bug:** `parseFieldOptions()` switch at lines 2028-2108 has no case for `debug_redact` (field 16 of FieldOptions). The option value token is consumed but not stored on `FieldOptions`. C++ protoc populates `FieldOptions.debug_redact = true`. Descriptor set size differs (112 vs 107 bytes). SourceCodeInfo locations differ (19 vs 18) — missing the option-specific location at path `[fieldPath, 8, 16]`.
- **Root cause:** `parser.go:2028-2108` — `parseFieldOptions` switch handles `default`, `json_name`, `deprecated`, `packed`, `lazy`, `jstype`, `ctype` but is missing `debug_redact` (field 16) and `unverified_lazy` (field 15). Unknown option names fall through without matching any case, silently dropped without error.

### Run 71 — Duplicate file-level options (FAILED: 5/5 profiles)
- **Test:** `77_duplicate_file_option` — proto3 file with `option java_package = "com.example.first";` followed by `option java_package = "com.example.second";`
- **Bug:** Go protoc-go silently accepts the duplicate option and overwrites the value, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:8: Option "java_package" was already set.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1894-1895` — `parseFileOption` unconditionally sets `fd.Options.JavaPackage = proto.String(valTok.Value)` without checking if the field was already set. No duplicate option tracking exists. C++ protoc tracks which options have been set and rejects duplicates. Same bug applies to ALL file-level options (go_package, optimize_for, etc.), all message options, all field options, etc.

### Run 72 — Proto3 optional + real oneof ordering (FAILED: 5/5 profiles)
- **Test:** `78_oneof_ordering` — proto3 message with `optional string name = 1;` (synthetic oneof) BEFORE `oneof payload { string text = 2; int32 number = 3; }` (real oneof), plus `optional int32 age = 4;` (another synthetic oneof)
- **Bug:** Go places `OneofDecl` entries in declaration order: `[_name, payload, _age]`. C++ protoc places real oneofs first, then synthetic oneofs: `[payload, _name, _age]`. This causes `OneofIndex` values on all fields to differ: Go sets `name.OneofIndex=0, text/number.OneofIndex=1, age.OneofIndex=2`. C++ sets `text/number.OneofIndex=0, name.OneofIndex=1, age.OneofIndex=2`. Binary descriptors differ accordingly.
- **Root cause:** `parser.go:389-396` — when a proto3 optional field is encountered, the synthetic oneof is immediately appended to `msg.OneofDecl` and `oneofIdx` is incremented. C++ protoc's `DescriptorBuilder` processes all real oneofs first, then creates synthetic oneofs for proto3_optional fields at the end. The Go parser should defer synthetic oneof creation until after all real oneofs are processed, or reorder `OneofDecl` entries before emitting the descriptor.
- **Also tried:** `json_name` trailing underscore (`field_name_` → both produce `fieldName`) — NOT a gap.

### Run 73 — Duplicate message-level options (FAILED: 5/5 profiles)
- **Test:** `79_duplicate_message_option` — proto3 message with `option deprecated = true;` followed by `option deprecated = false;`
- **Bug:** Go protoc-go silently accepts duplicate message options and overwrites the value, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:10: Option "deprecated" was already set.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:772-784` — `parseMessageOption` unconditionally sets `msg.Options.Deprecated` without checking if it was already set. No duplicate option tracking exists. Same pattern as duplicate file-level options (Run 71). Applies to all message options (`deprecated`, `no_standard_descriptor_accessor`, `message_set_wire_format`).

### Known gaps still unexplored (updated):
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `map_entry` (field 7) — `message_set_wire_format` TESTED in Run 69
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Field option `unverified_lazy`** (field 15) — TESTED, already fixed (added to switch)
- **Option validation** — Go silently accepts ANY option name without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **String concatenation in service/method/enum option values** — same single-token bug as field defaults
- **Missing service options** — only `deprecated` handled
- **Error message format consistency** — many C++ protoc errors omit line:col but Go includes them (or vice versa)
- **Type name spaces in map value types** — `map<string, pkg . Msg>` — same span bug
- **Type name spaces in method input/output** — `rpc Foo(pkg . Req) returns (pkg . Resp)` — same span bug
- **Duplicate file-level options** — TESTED in Run 71 (77_duplicate_file_option), confirmed broken
- **Duplicate message options** — TESTED in Run 73 (79_duplicate_message_option), confirmed broken
- **Duplicate field/enum/service options** — same pattern, Go likely overwrites all
- **Duplicate `option optimize_for`** — same issue
- **Synthetic oneof ordering** — TESTED in Run 72 (78_oneof_ordering), confirmed broken
- **Synthetic oneof source code info paths** — the SourceCodeInfo paths for synthetic oneofs may also differ due to index mismatch
- **Proto3 optional inside nested messages** — same ordering bug would apply recursively

### Run 74 — Duplicate field-level options (FAILED: 5/5 profiles)
- **Test:** `80_duplicate_field_option` — proto3 message with `string phone = 3 [deprecated = true, deprecated = false];` (same option specified twice in bracket list)
- **Bug:** Go protoc-go silently accepts duplicate field options and overwrites the value, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:8:40: Option "deprecated" was already set.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go` — `parseFieldOptions` processes each option in the `[...]` list without checking if it was already set. Same pattern as duplicate file-level options (Run 71) and duplicate message options (Run 73). No duplicate option tracking exists for any option level. Applies to all field options (`deprecated`, `packed`, `json_name`, `lazy`, `jstype`, `ctype`, `debug_redact`).

### Run 75 — Duplicate enum-level options (FAILED: 5/5 profiles)
- **Test:** `81_duplicate_enum_option` — proto3 enum with `option deprecated = true;` followed by `option deprecated = false;`
- **Bug:** Go protoc-go silently accepts duplicate enum options and overwrites the value, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:10: Option "deprecated" was already set.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1238` — `parseEnumOption` has no duplicate option tracking. Unlike `parseFileOption` (which has `seenFileOptions`) and `parseMessageOption` (which receives a `seenOptions` map), `parseEnumOption` unconditionally sets the option value without checking if it was already set. Same pattern applies to `parseServiceOption` and `parseMethodOption` — neither has duplicate tracking.

### Run 76 — Duplicate service options (FAILED: 5/5 profiles)
- **Test:** `82_duplicate_service_option` — proto3 service with `option deprecated = true;` followed by `option deprecated = false;`
- **Bug:** Go protoc-go silently accepts duplicate service options and overwrites the value, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:10:10: Option "deprecated" was already set.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1475-1481` — `parseServiceOption` unconditionally sets `svc.Options.Deprecated` without checking if it was already set. No duplicate option tracking exists (no `seenOptions` map passed in). Same pattern as duplicate file-level options (Run 71), duplicate message options (Run 73), duplicate field options (Run 74), and duplicate enum options (Run 75).

### Run 77 — Duplicate method options (FAILED: 5/5 profiles)
- **Test:** `83_duplicate_method_option` — proto3 service with a method containing `option deprecated = true;` followed by `option deprecated = false;`
- **Bug:** Go protoc-go silently accepts duplicate method options and overwrites the value, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:16:12: Option "deprecated" was already set.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1503-1550` — `parseMethodOption` unconditionally sets `method.Options.Deprecated` without checking if it was already set. No duplicate option tracking exists (no `seenOptions` map passed in). Same pattern as duplicate file-level options (Run 71), duplicate message options (Run 73), duplicate field options (Run 74), duplicate enum options (Run 75), and duplicate service options (Run 76).

### Run 78 — Duplicate enum value options (FAILED: 5/5 profiles)
- **Test:** `84_duplicate_enum_value_option` — proto3 enum with `HIGH = 1 [deprecated = true, deprecated = false];` (same option twice in bracket list)
- **Bug:** Go protoc-go silently accepts duplicate enum value options and overwrites the value, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:32: Option "deprecated" was already set.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1136-1173` — enum value option parsing loop has no duplicate tracking. Unlike `parseFieldOptions` (which has `seenFieldOpts` map at line 2053), `parseMessageOption` (which receives `seenOptions`), and all other option parsers, the enum value option loop at line 1136 processes each option without checking if it was already set. The switch at line 1153-1157 unconditionally sets `enumValOpts.Deprecated` on each iteration. Same pattern as all other duplicate option bugs (Runs 71-77), but this one is at the enum value level (inside `[...]` brackets on enum value declarations).

### Run 79 — Invalid syntax value (FAILED: 5/5 profiles)
- **Test:** `85_invalid_syntax` — file with `syntax = "proto4";` (unrecognized syntax identifier)
- **Bug:** Go protoc-go silently accepts `"proto4"` as a syntax value and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:1:10: Unrecognized syntax identifier "proto4".  This parser only recognizes "proto2" and "proto3".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:151-178` — `parseSyntax` stores whatever string is provided as the syntax value without validating it. Line 167: `if valTok.Value != "proto2"` sets `fd.Syntax`. Line 170: `p.syntax = valTok.Value` stores it for later. No check that the value is `"proto2"` or `"proto3"`. Since `p.syntax` is `"proto4"`, proto2 validation (`p.syntax == "proto2"`) doesn't fire and proto3 validation (`fd.GetSyntax() != "proto3"`) skips it. The parser treats `"proto4"` like proto3 (no label requirements) without any error.

### Known gaps still unexplored (updated):
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `map_entry` (field 7) — only `deprecated`, `no_standard_descriptor_accessor`, `message_set_wire_format` handled
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation (default returns nil)
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **Error message format consistency** — many C++ protoc errors omit line:col but Go includes them (or vice versa)
- **Type name spaces in method input/output** — `rpc Foo(pkg . Req) returns (pkg . Resp)` — same span bug
- **Extension number out of range** — extension using number outside declared range
- **Proto3 optional inside nested messages** — synthetic oneof ordering bug would apply recursively
- **Duplicate idempotency_level** — `option idempotency_level = IDEMPOTENT; option idempotency_level = NO_SIDE_EFFECTS;` — same pattern
- **Duplicate map field options** — `map<string,string> m = 1 [deprecated = true, deprecated = false];` — likely same bug
- **Invalid syntax value** — TESTED in Run 79 (85_invalid_syntax), confirmed broken (no validation of syntax string)
- **Invalid edition value** — `edition = "2025";` or `edition = "9999";` — Go has editionMap check but C++ might differ on unrecognized editions
- **Boolean option with integer value** — TESTED in Run 80 (86_bool_option_int), confirmed broken (Go accepts, C++ rejects)

### Run 80 — Boolean option with integer value (FAILED: 5/5 profiles)
- **Test:** `86_bool_option_int` — proto3 file with `option java_multiple_files = 1;` (integer instead of boolean literal)
- **Bug:** Go protoc-go silently accepts integer `1` for boolean option `java_multiple_files` and stores `false` (since `"1" != "true"`), producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:5:30: Value must be identifier for boolean option "google.protobuf.FileOptions.java_multiple_files".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1962` — `fd.Options.JavaMultipleFiles = proto.Bool(valTok.Value == "true")` accepts any token value and does a string comparison. No validation that the token is actually a boolean identifier (`true`/`false`). Integer tokens, string tokens, or any other token type are silently accepted and treated as `false`. C++ protoc's option parser validates that boolean options receive identifier tokens with value `true` or `false`. Same bug applies to ALL boolean file options (`cc_generic_services`, `java_generic_services`, `py_generic_services`, `deprecated`, `cc_enable_arenas`, `java_string_check_utf8`, etc.) and boolean message/field/enum/service/method options.

### Run 81 — Extension number out of range (FAILED: 5/5 profiles)
- **Test:** `87_extension_out_of_range` — proto2 file with `message Base { extensions 100 to 200; }` and `extend Base { optional string nickname = 300; }` (field number 300 outside declared range 100-200)
- **Bug:** Go protoc-go silently accepts the extension with field number 300 and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:11:30: "extrange.Base" does not declare 300 as an extension number.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that extension field numbers must fall within a declared `extensions` range of the extended message. The Go `descriptor/pool.go` is an empty stub with no extension range validation. The parser stores extension fields without checking if their numbers are within the declared extension ranges of the target message.

### Run 82 — Proto2 oneof fields unparseable (FAILED: 5/5 profiles)
- **Test:** `88_oneof_default` — proto2 message with `oneof payload { string name = 1 [default = "hello"]; int32 count = 2; }`
- **Bug:** Go protoc-go rejects valid proto2 oneof fields with: `Expected "required", "optional", or "repeated".` (exit 1). C++ protoc accepts the file and produces a valid descriptor (exit 0). Proto2 oneof fields must NOT have labels, but the Go parser requires labels for all proto2 fields — creating a dead-end where `parseOneof` rejects labels (line 1751-1753) but `parseField` requires them (line 762).
- **Root cause:** `parser.go:756-762` — `parseField` checks `if p.syntax == "proto2"` and requires explicit labels. But oneof fields in proto2 are an exception — they must NOT have labels. When `parseOneof` calls `parseField` (line 1756), the field has no label, so `parseField` errors. The fix should skip the proto2 label requirement when parsing inside a oneof. Secondary bug: if the label issue is fixed, C++ protoc still accepts `[default = "hello"]` on oneof fields, but Go would need to handle it correctly too.

### Run 83 — Duplicate imports (FAILED: 5/5 profiles)
- **Test:** `89_duplicate_import` — proto3 file with `import "base.proto";` listed twice, referencing a base.proto with a Timestamp message
- **Bug:** Go protoc-go silently accepts duplicate imports and stores `"base.proto"` twice in `fd.Dependency`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:1: Import "base.proto" was listed twice.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:275-276` — `parseImport` unconditionally appends `pathTok.Value` to `fd.Dependency` without checking if the import path already exists in the dependency list. No deduplication or duplicate detection. C++ protoc tracks imported files and rejects duplicates in `descriptor.cc`. Same issue applies to `import public` and `import weak` — importing the same file twice with different modifiers would also be silently accepted.

### Run 84 — String literal for boolean option (FAILED: 5/5 profiles)
- **Test:** `90_string_bool_option` — proto3 file with `option java_multiple_files = "true";` (string literal `"true"` instead of identifier `true`)
- **Bug:** Go protoc-go silently accepts a string literal for a boolean option and correctly sets `java_multiple_files = true`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:5:30: Value must be identifier for boolean option "google.protobuf.FileOptions.java_multiple_files".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1962-1966` — `validateBool` checks `valTok.Value != "true" && valTok.Value != "false"` but does NOT check `valTok.Type`. A TokenString with Value `"true"` (decoded content without quotes) passes the check. C++ protoc's parser uses `ConsumeIdentifier` for boolean values, which rejects string literal tokens. Same bug applies to ALL boolean options at every level (file, message, field, enum, service, method) — any quoted `"true"` or `"false"` would be accepted by Go but rejected by C++.

### Run 85 — String literal for enum option (FAILED: 5/5 profiles)
- **Test:** `91_string_enum_option` — proto3 file with `option optimize_for = "SPEED";` (string literal `"SPEED"` instead of identifier `SPEED`)
- **Bug:** Go protoc-go silently accepts a string literal for the enum option `optimize_for` and correctly sets `OptimizeFor = SPEED`, producing a valid descriptor (exit 0). C++ protoc rejects with an error about expecting an identifier for enum-type options (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1987-1998` — the `optimize_for` case does `switch valTok.Value` which checks the decoded string content. A TokenString `"SPEED"` has `valTok.Value = "SPEED"` (decoded without quotes), so it matches the `case "SPEED"`. No check on `valTok.Type` to ensure it's an identifier token. C++ protoc uses `ConsumeIdentifier` for enum-typed options, rejecting string literal tokens. Same category as Run 84 (string for boolean), but here it affects enum-typed options.

### Known gaps still unexplored (updated):
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `map_entry` (field 7) — only `deprecated`, `no_standard_descriptor_accessor`, `message_set_wire_format` handled
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **Error message format consistency** — many C++ protoc errors omit line:col but Go includes them
- **Type name spaces in method input/output** — `rpc Foo(pkg . Req) returns (pkg . Resp)` — same span bug
- **Proto3 optional inside nested messages** — synthetic oneof ordering bug would apply recursively
- **Duplicate idempotency_level** — same duplicate option pattern
- **Duplicate map field options** — likely same bug
- **Invalid edition value** — `edition = "2025"` — Go has editionMap check but C++ might differ
- **Proto2 oneof fields** — TESTED in Run 82 (88_oneof_default), confirmed broken
- **Duplicate imports** — TESTED in Run 83 (89_duplicate_import), confirmed broken
- **String literal for boolean option** — TESTED in Run 84 (90_string_bool_option), confirmed broken
- **String literal for enum option** — TESTED in Run 85 (91_string_enum_option), confirmed broken
- **String literal for integer option** — `option optimize_for = "1";` or numeric options with string values
- **Integer value for enum option** — `option optimize_for = 1;` — Go may accept, C++ rejects
- **Duplicate `import public`** — same file imported as both `import` and `import public`

### Run 86 — JSON name conflict (FAILED: 5/5 profiles)
- **Test:** `92_json_name_conflict` — proto3 message with `string foo_bar = 1;` and `string fooBar = 2;` (both generate JSON name `"fooBar"`)
- **Bug:** Go protoc-go silently accepts the conflicting JSON names and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:10: The default JSON name of field "fooBar" ("fooBar") conflicts with the default JSON name of field "foo_bar".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that auto-generated JSON field names (`ToJsonName`) are unique within a message. The Go `descriptor/pool.go` is an empty stub with no JSON name conflict validation. The parser stores both fields with the same `json_name` without any cross-field uniqueness check.

### Run 87 — Integer value for string option (FAILED: 5/5 profiles)
- **Test:** `93_int_string_option` — proto3 file with `option java_package = 42;` (integer literal instead of quoted string)
- **Bug:** Go protoc-go silently accepts integer `42` for string option `java_package` and stores `java_package = "42"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:5:23: Value must be quoted string for string option "google.protobuf.FileOptions.java_package".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1972-1973` — `fd.Options.JavaPackage = proto.String(valTok.Value)` accepts any token type and converts its Value to a string. No validation that `valTok.Type == tokenizer.TokenString`. C++ protoc's `ConsumeString()` requires a string literal token. Same bug applies to ALL string-typed file options (`java_outer_classname`, `go_package`, `php_namespace`, `php_class_prefix`, `php_metadata_namespace`, `ruby_package`, `objc_class_prefix`, `csharp_namespace`, `swift_prefix`) — none validate that the value token is a string literal.

### Run 88 — Integer value for string field option json_name (FAILED: 5/5 profiles)
- **Test:** `94_int_json_name` — proto3 message with `string name = 1 [json_name = 42];` (integer literal instead of quoted string for json_name)
- **Bug:** Go protoc-go silently accepts integer `42` for string field option `json_name` and stores `json_name = "42"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:32: Expected string for JSON name.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2214` — `field.JsonName = proto.String(valTok.Value)` accepts any token type and converts its Value to a string. No validation that `valTok.Type == tokenizer.TokenString`. C++ protoc's parser uses `ConsumeString()` for json_name, which requires a string literal token. Same category as Run 87 (integer for file-level string option), but at the field option level.

### Known gaps still unexplored (updated):
- **JSON name conflict with explicit json_name** — `string a = 1 [json_name = "x"]; string b = 2 [json_name = "x"];` — same issue
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Negative float default span** — `[default = -1.5]` likely has same column offset bug
- **Missing message options** — `map_entry` (field 7)
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **Integer value for enum option** — `option optimize_for = 1;` — Go rejects (type check added), NOT a gap
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Integer for string option** — TESTED in Run 87 (93_int_string_option), confirmed broken
- **Identifier for string option** — `option java_package = foo;` — same bug, identifier instead of string
- **Integer for string field option** — TESTED in Run 88 (94_int_json_name), confirmed broken
- **Identifier for json_name** — `[json_name = foo]` — same pattern, identifier instead of string
- **Identifier for string option** — TESTED locally, Go now rejects identically to C++ — NOT a gap

### Run 89 — Overlapping extension ranges (FAILED: 5/5 profiles)
- **Test:** `95_extension_range_overlap` — proto2 message with `extensions 100 to 200;` and `extensions 150 to 300;` (overlapping ranges)
- **Bug:** Go protoc-go silently accepts overlapping extension ranges and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:14: Extension range 150 to 300 overlaps with already-defined range 100 to 200.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that extension ranges within a message must not overlap. The Go `descriptor/pool.go` is an empty stub with no extension range overlap checking. The parser stores all extension ranges without any cross-range validation.

### Known gaps still unexplored (updated):
- **JSON name conflict with explicit json_name** — `string a = 1 [json_name = "x"]; string b = 2 [json_name = "x"];`
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Missing message options** — `map_entry` (field 7)
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Edition features** — `edition = "2023"` with feature overrides
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Overlapping reserved ranges** — `reserved 1 to 10; reserved 5 to 15;` — same overlap validation gap
- **Extension range overlap with field numbers** — `int32 x = 100;` with `extensions 100 to 200;` — C++ validates, Go likely doesn't
- **Reserved range overlap with extension range** — reserved and extensions in same message overlapping

### Run 90 — Extension range overlaps with field number (FAILED: 5/5 profiles)
- **Test:** `96_extension_field_conflict` — proto2 message with `optional int32 value = 100;` AND `extensions 100 to 200;` (field number falls within extension range)
- **Bug:** Go protoc-go silently accepts a field whose number falls within a declared extension range and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:8:14: Extension range 100 to 200 includes field "value" (100).` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that no field number may overlap with a declared extension range within the same message. The Go `descriptor/pool.go` is an empty stub with no extension range vs field number validation. The parser stores both the field and the extension range without any cross-validation.

### Run 91 — Overlapping reserved ranges (FAILED: 5/5 profiles)
- **Test:** `97_reserved_range_overlap` — proto3 message with `reserved 1 to 10;` and `reserved 5 to 15;` (overlapping reserved ranges)
- **Bug:** Go protoc-go silently accepts overlapping reserved ranges and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:12: Reserved range 5 to 15 overlaps with already-defined range 1 to 10.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that reserved ranges within a message must not overlap. The Go `descriptor/pool.go` is an empty stub with no reserved range overlap checking. The parser stores all reserved ranges without any cross-range validation. Same pattern as extension range overlap (Run 89).

### Known gaps still unexplored (updated):
- **Reserved range overlap with field numbers** — field number within a reserved range (different from reserved name conflict, Run 48)
- **JSON name conflict with explicit json_name** — `string a = 1 [json_name = "x"]; string b = 2 [json_name = "x"];`
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Missing message options** — `map_entry` (field 7)
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Overlapping reserved ranges** — TESTED in Run 91 (97_reserved_range_overlap), confirmed broken
- **Overlapping enum reserved ranges** — TESTED in Run 92 (98_enum_reserved_overlap), confirmed broken

### Run 92 — Overlapping enum reserved ranges (FAILED: 5/5 profiles)
- **Test:** `98_enum_reserved_overlap` — proto3 enum with `reserved 20 to 30;` and `reserved 25 to 40;` (overlapping reserved ranges inside an enum)
- **Bug:** Go protoc-go silently accepts overlapping enum reserved ranges and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:10:12: Reserved range 25 to 40 overlaps with already-defined range 20 to 30.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that enum reserved ranges must not overlap, same as message reserved ranges. The Go `descriptor/pool.go` is an empty stub with no enum reserved range overlap checking. The parser stores all enum reserved ranges without any cross-range validation. Same pattern as message reserved range overlap (Run 91).

### Run 93 — Enum value number within reserved range (FAILED: 5/5 profiles)
- **Test:** `99_enum_reserved_value_conflict` — proto3 enum with `reserved 5 to 10;` and `MEDIUM = 7;` (enum value number 7 falls within reserved range 5-10)
- **Bug:** Go protoc-go silently accepts an enum value whose number is in a reserved range and produces a valid descriptor (exit 0). C++ protoc rejects with an error about enum value using a reserved number (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that enum value numbers must not fall within declared reserved ranges. The Go `descriptor/pool.go` is an empty stub with no enum reserved range vs enum value validation. The parser stores both the reserved ranges and the conflicting enum value without any cross-validation. Same pattern as message reserved number conflicts (Run 49).

### Known gaps still unexplored (updated):
- **Reserved range overlap with field numbers** — field number within a reserved range
- **JSON name conflict with explicit json_name** — `string a = 1 [json_name = "x"]; string b = 2 [json_name = "x"];`
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Missing message options** — `map_entry` (field 7)
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Enum reserved value conflict** — TESTED in Run 93 (99_enum_reserved_value_conflict), confirmed broken
- **Overlapping enum reserved names** — `reserved "A", "B"; reserved "B", "C";` — duplicate reserved names
- **Enum reserved name conflict** — TESTED in Run 94 (100_enum_reserved_name_conflict), confirmed broken

### Run 94 — Enum reserved name conflict (FAILED: 5/5 profiles)
- **Test:** `100_enum_reserved_name_conflict` — proto3 enum with `reserved "DELETED", "ARCHIVED";` and `DELETED = 3;` (enum value name matches reserved name)
- **Bug:** Go protoc-go silently accepts an enum value whose name is in the reserved name list and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:9:3: Enum value "DELETED" is reserved.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that enum value names must not match any reserved name declared in the same enum. The Go `descriptor/pool.go` is an empty stub with no enum reserved name vs enum value name validation. The parser stores both the reserved names and the conflicting enum value without any cross-validation. Same pattern as message reserved name conflicts (Run 48) and enum reserved value number conflicts (Run 93).

### Run 95 — Explicit map_entry option (FAILED: 5/5 profiles)
- **Test:** `101_map_entry_explicit` — proto3 message with `option map_entry = true;` explicitly set on a user-defined message (with key/value fields)
- **Bug:** Go protoc-go silently discards the `map_entry` option (default case in `parseMessageOption` returns nil) and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:10: map_entry should not be set explicitly. Use map<KeyType, ValueType> instead.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:789-800` — `parseMessageOption` switch handles `deprecated` (field 3), `no_standard_descriptor_accessor` (field 2), and `message_set_wire_format` (field 1) but NOT `map_entry` (field 7). The `default` case at line 800 does `return nil`, silently discarding the option. Even if `map_entry` were added to the switch, C++ protoc explicitly rejects it in `descriptor.cc` validation — `map_entry` can only be set by the compiler on synthetic map entry messages, not by users. The Go implementation lacks both: (1) the option storage, and (2) the validation that rejects explicit usage.

### Known gaps still unexplored (updated):
- **JSON name conflict with explicit json_name** — `string a = 1 [json_name = "x"]; string b = 2 [json_name = "x"];`
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145`
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — `message FOO` + enum value `FOO` in same scope
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Overlapping enum reserved names** — `reserved "A", "B"; reserved "B", "C";` — duplicate reserved names
- **Explicit map_entry option** — TESTED in Run 95 (101_map_entry_explicit), confirmed broken
- **Oneof inside oneof** — nested oneof — C++ rejects, Go may accept
- **Negative field numbers** — `string name = -1;` — C++ rejects, Go may accept
- **Map field with message key type** — `map<MyMsg, string>` — C++ rejects, Go likely accepts

### Run 96 — Enum value name collides with message name (FAILED: 5/5 profiles)
- **Test:** `102_enum_msg_name_conflict` — proto3 file with `message Foo { ... }` and `enum Bar { Foo = 0; }` (enum value name "Foo" collides with message name "Foo" at package scope)
- **Bug:** Both C++ and Go detect the duplicate name and reject with exit code 1. Both emit: `test.proto:10:3: "Foo" is already defined in "enumconflict".` However, C++ protoc also emits a second explanatory line: `test.proto:10:3: Note that enum values use C++ scoping rules, meaning that enum values are siblings of their type, not children of it.  Therefore, "Foo" must be unique within "enumconflict", not just within "Bar".` Go is missing this explanatory note. The test harness detects error message mismatch.
- **Root cause:** Go's duplicate symbol validation (likely in `compiler/cli/cli.go`) emits only the base error. C++ protoc's `descriptor.cc` emits an additional note about C++ scoping rules for enum values when an enum value name conflicts with another symbol. Same issue as Run 61 (duplicate oneof names) — Go error output is missing supplementary diagnostic messages that C++ includes.

### Run 97 — Reserved range overlaps with extension range (FAILED: 5/5 profiles)
- **Test:** `103_reserved_extension_overlap` — proto2 message with `reserved 100 to 200;` and `extensions 150 to 300;` (reserved range overlaps with extension range)
- **Bug:** Go protoc-go silently accepts the overlap and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:8:14: Extension range 150 to 300 overlaps with reserved range 100 to 200.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No cross-validation between reserved ranges and extension ranges in Go implementation. C++ protoc validates in `descriptor.cc` that extension ranges must not overlap with reserved ranges within the same message. The Go `compiler/cli/cli.go` validates reserved-reserved overlaps (line 1478) and extension-extension overlaps (line 1654), but never checks reserved vs extension cross-overlap. The parser stores both ranges without any cross-range validation.

### Run 98 — Enum value number overflow (FAILED: 5/5 profiles)
- **Test:** `104_enum_value_overflow` — proto3 enum with `TOO_BIG = 2147483648;` (exceeds int32 max of 2147483647)
- **Bug:** Go protoc-go silently accepts the out-of-range enum value and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:7:13: Integer out of range.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1208` — `num, _ := strconv.ParseInt(valNumTok.Value, 0, 32)` silently ignores the `ErrRange` error. When `ParseInt` overflows int32, it returns the clamped value (2147483647) with an error, but the error is discarded via `_`. The enum value is stored as 2147483647 instead of being rejected. C++ protoc's tokenizer validates integer range during parsing and errors immediately. The fix: check the error from `strconv.ParseInt` and return an error if it fails. Same issue does NOT affect field numbers (line 892-895) because field number parsing properly checks the error.

### Run 99 — Reserved range number overflow (FAILED: 5/5 profiles)
- **Test:** `105_reserved_number_overflow` — proto3 message with `reserved 2147483648;` (exceeds int32 max of 2147483647)
- **Bug:** Go protoc-go silently accepts the out-of-range reserved number and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:12: Integer out of range.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:483` — `startNum, _ := strconv.ParseInt(numTok.Value, 0, 32)` silently ignores the `ErrRange` error. When `ParseInt` overflows int32, it returns the clamped value (2147483647) with an error, but the error is discarded via `_`. Same issue at line 503 for reserved range end values, lines 559/583 for extension range start/end values, and line 1855 for map field numbers. All use `_, _ := strconv.ParseInt(..., 0, 32)` pattern where the error is silently discarded.

### Run 100 — Map field number overflow (FAILED: 5/5 profiles)
- **Test:** `106_map_field_number_overflow` — proto3 message with `map<string, string> metadata = 2147483648;` (field number exceeds int32 max)
- **Bug:** Go protoc-go parses the overflowed integer (silently truncated to 2147483647 at line 1873 via `num, _ := strconv.ParseInt(numTok.Value, 0, 32)`), then the downstream field number validation catches 2147483647 > 536870911 and reports: `Field numbers cannot be greater than 536870911.` plus a suggestion line. C++ protoc catches it earlier at parse time as `Integer out of range.` (exit 1 from both, but different error messages). The test harness detects error message mismatch.
- **Root cause:** `parser.go:1873` — `num, _ := strconv.ParseInt(numTok.Value, 0, 32)` silently discards the overflow error. The value is truncated to max int32 (2147483647), then a different validation catches an unrelated constraint (field number > max allowed). C++ protoc's tokenizer validates integer range during parsing and errors immediately with "Integer out of range." The fix: check the error from `strconv.ParseInt` and return an integer overflow error before field number validation runs.

### Run 101 — Explicit json_name conflict says "default" instead of "custom" (FAILED: 5/5 profiles)
- **Test:** `107_json_name_explicit` — proto3 message with `string first_name = 1 [json_name = "name"];` and `string last_name = 2 [json_name = "name"];` (both fields explicitly set the same json_name)
- **Bug:** Both C++ and Go detect the JSON name conflict and reject with exit code 1, but the error message wording differs. C++ protoc: `The custom JSON name of field "last_name" ("name") conflicts with the custom JSON name of field "first_name".` Go protoc-go: `The default JSON name of field "last_name" ("name") conflicts with the default JSON name of field "first_name".` Go uses "default" instead of "custom" because it doesn't distinguish between auto-generated and explicitly set json_names.
- **Root cause:** Go's JSON name conflict validation (likely in `compiler/cli/cli.go`) always uses "default JSON name" in the error message. C++ protoc's `descriptor.cc` checks whether the json_name was explicitly set by the user (`has_json_name()`) and uses "custom JSON name" when it was, "default JSON name" when it's auto-generated. The Go implementation lacks `has_json_name()` tracking — it doesn't know if json_name was set by the user or auto-generated.

### Known gaps still unexplored (updated):
- **JSON name conflict with explicit json_name** — TESTED in Run 101 (107_json_name_explicit), confirmed broken (error message says "default" instead of "custom")
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Hex/octal escape in strings** — `\x48\x65` or `\110\145` — tokenizer now handles these (fixed)
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Extension range options** — `extensions 100 to 199 [(verification) = UNVERIFIED];`
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Enum value name collision with message name** — TESTED in Run 96, confirmed error message difference
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Overlapping enum reserved names** — `reserved "A", "B"; reserved "B", "C";` — duplicate reserved names
- **Oneof inside oneof** — nested oneof — C++ rejects, Go may accept
- **Negative field numbers** — `string name = -1;` — C++ rejects, Go may accept
- **Negative enum value overflow** — `FOO = -2147483649;` — same silent truncation bug
- **Extension range start/end overflow** — overflow checks now added (fixed)
- **Enum reserved range overflow** — overflow checks now added (fixed)
- **`stream` as a type name in RPC** — TESTED in Run 102 (108_stream_type_name), confirmed broken (different error messages)

### Run 102 — Message named "stream" used as RPC type (FAILED: 5/5 profiles)
- **Test:** `108_stream_type_name` — proto3 file with `message stream { ... }` and `rpc Process(stream) returns (stream);` where `stream` is used as a type name, not the streaming keyword
- **Bug:** Both C++ and Go reject the file (both treat `stream` as the streaming keyword when it appears after `(` in an RPC declaration), but with different error messages. C++ protoc: `test.proto:10:21: Expected type name.` Go protoc-go: `test.proto:line 10:23: expected ")", got "returns"`. C++ correctly identifies the missing type name after the `stream` keyword. Go has a cascading parse error: it consumes `)` as the type name, then fails expecting another `)`.
- **Root cause:** `parser.go:1639-1642` — when `stream` is followed by `)`, the Go parser still consumes `stream` as the keyword. Then `p.tok.Next()` at line 1643 gets `)` as `inputTok` (setting `inputType = ")"`). Then `p.tok.Expect(")")` at line 1659 sees `returns` instead of `)` → error at column 23. C++ protoc also consumes `stream` as the keyword, but immediately detects the missing type name at column 21 (the `)` position) before trying to consume the closing paren. The error messages differ in both content and column position.

### Run 103 — Map field with enum key type (FAILED: 5/5 profiles)
- **Test:** `109_map_enum_key` — proto3 file with `map<Priority, string> task_names = 2;` where `Priority` is an enum defined in the same file
- **Bug:** `parseMapField()` at line 1840-1842 checks if the key type is in `builtinTypes`. Since `Priority` is not a builtin type, Go rejects with `"invalid map key type: Priority"` at parse time. C++ protoc also rejects (enum keys are not valid per the protobuf spec), but with a different error message: `"Key in map fields cannot be enum types."` at validation time. Both exit 1, but stderr differs.
- **Root cause:** `parser.go:1840-1842` — Go rejects non-builtin key types at parse time with a generic error. C++ protoc accepts the type during parsing, resolves it during linking, then validates in `descriptor.cc` with a specific error mentioning "enum types". The error message format also differs: Go has no line:column prefix (`test.proto:invalid map key type: Priority`), C++ has line:column (`test.proto:14:3: Key in map fields cannot be enum types.`). Additionally, Go's approach of rejecting non-builtins at parse time is overly restrictive — if protobuf ever allowed enum keys, Go would need parser changes while C++ would only need a validation change.

### Run 104 — Extension range options (FAILED: 5/5 profiles)
- **Test:** `110_extension_range_options` — proto2 message with `extensions 100 to 199 [verification = UNVERIFIED];` (extension range with options)
- **Bug:** Go protoc-go rejects the `[` token after the range with: `expected ";", got "["` (exit 1). C++ protoc accepts it and produces a valid descriptor with `ExtensionRangeOptions` containing `verification = UNVERIFIED` (exit 0). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:623-630` — after parsing extension range numbers, the parser checks for `,` (another range) or breaks to expect `;`. There is no handling for `[` to parse `ExtensionRangeOptions`. C++ protoc's parser checks for `[` after ranges and calls `ParseExtensionRangeOptions` to read key-value options into the `ExtensionRange.options` field. The Go parser needs to add `[...]` option parsing between the range loop exit (line 628) and the `;` expectation (line 630).

### Known gaps still unexplored (updated):
- **Option validation** — Go silently accepts ANY option name on service/method/enum without validation
- **Self-referencing message** — type resolution may differ
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Overlapping enum reserved names** — `reserved "A", "B"; reserved "B", "C";` — duplicate reserved names
- **Oneof inside oneof** — nested oneof — C++ rejects, Go may accept
- **Negative field numbers** — `string name = -1;` — C++ rejects, Go may accept
- **Negative enum value overflow** — `FOO = -2147483649;` — may be fixed now (int64 parsing)
- **Proto2 default values** — `[default = ...]` for enum-typed fields may not work
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **Missing message options** — `map_entry` (field 7)
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **Integer value for enum option** — `option optimize_for = 1;`
- **Duplicate `import public`** — same file imported via `import` and `import public`
- **Map field with message key type** — `map<MyMsg, string>` — Go rejects at parse time, C++ at validation with different error
- **message_set_wire_format + extensions to max** — Go uses INT32_MAX (2147483647), C++ uses 536870912 for `max` sentinel
- **Extension range options** — TESTED in Run 104 (110_extension_range_options), confirmed broken (Go rejects `[...]` after ranges)

### Run 105 — Enum used as RPC input type (FAILED: 5/5 profiles)
- **Test:** `111_enum_rpc_type` — proto3 file with `enum Status { ... }`, `message Response { ... }`, and `rpc GetStatus(Status) returns (Response);` where the RPC input type is an enum instead of a message
- **Bug:** Go protoc-go silently accepts an enum as an RPC input type and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:16:17: "Status" is not a message type.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that RPC method input and output types must be message types, not enums. The Go `descriptor/pool.go` is an empty stub with no method type validation. The parser stores the type name string without checking whether it resolves to a message or an enum.

### Run 106 — Negative field numbers (FAILED: 5/5 profiles)
- **Test:** `112_negative_field_number` — proto3 message with `string name = -1;` (negative field number)
- **Bug:** Both C++ and Go reject the file (exit 1), but with different error messages. C++ protoc: `test.proto:6:17: Expected field number.` Go protoc-go: `test.proto:line 6:17: expected integer, got "-"`. C++ treats the `-` as an unexpected token and reports "Expected field number." Go's `ExpectInt()` at line 892 fails because `-` is not an integer token.
- **Root cause:** `parser.go:892` — `ExpectInt()` encounters `-` (a symbol token) and produces a generic "expected integer" error. C++ protoc's parser produces a domain-specific "Expected field number" error. Both correctly reject negative field numbers, but the error message format and content differ. The test harness detects error message mismatch.

### Run 107 — `map` as a message type name (FAILED: 5/5 profiles)
- **Test:** `113_map_as_type` — proto3 file with `message map { ... }` and `message Container { map data = 1; }` where `map` is used as a type name (not the map field keyword)
- **Bug:** Go parser's message body switch at line 372 has `case "map":` which unconditionally calls `parseMapField()`. `parseMapField` expects `<` after `map`, so `map data = 1;` fails with `expected "<", got "data"`. C++ protoc only treats `map` as the map keyword when followed by `<`; otherwise it treats `map` as a type name (message reference). C++ produces a valid descriptor (exit 0), Go rejects (exit 1).
- **Root cause:** `parser.go:372` — `case "map":` doesn't check if the next token is `<` before committing to map field parsing. C++ protoc's parser peeks at the token after `map` and only enters map parsing if it's `<`. When `map` is followed by an identifier (field name), C++ treats it as a regular field with `map` as the type name. The Go parser should check `p.tok.PeekAt(1).Value == "<"` (similar to how `isGroupField` checks for `group` at line 401) and fall through to `parseField` if `<` doesn't follow.

### Run 108 — Integer default value on string field (FAILED: 5/5 profiles)
- **Test:** `114_int_default_string` — proto2 message with `optional string name = 1 [default = 42];` (integer literal instead of string literal for string field default)
- **Bug:** Go protoc-go silently accepts integer `42` as a default value for a string field and stores `default_value = "42"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:39: Expected string for field default value.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2332-2362` — `case "default"` stores `valTok.Value` as the default value without validating that the token type matches the field type. For string/bytes fields, the value must be a string literal (`TokenString`). For integer fields, it must be an integer literal. For float fields, a float literal. C++ protoc's `ParseDefaultAssignment` dispatches based on field type, calling `ConsumeString` for string fields, `ConsumeSignedInteger` for integer fields, etc. The Go parser has zero default value type validation — any token type is accepted for any field type.

### Known gaps still unexplored (updated):
- **RPC type referencing non-existent message** — C++ rejects, Go likely accepts (no type resolution validation)
- **Overlapping enum reserved names** — `reserved "A", "B"; reserved "B", "C";` — duplicate reserved names
- **Oneof inside oneof** — nested oneof — C++ rejects, Go may accept
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **Map field with message key type** — `map<MyMsg, string>` — Go rejects at parse time, C++ at validation with different error
- **`option` as type name** — `message option { } message Foo { option x = 1; }` — Go treats `option` as keyword, same pattern
- **`reserved` as type name** — same pattern, Go switch matches keyword before checking context
- **`extensions` as type name** — same pattern
- **String default for integer field** — `optional int32 x = 1 [default = "42"];` — Go likely accepts, C++ rejects
- **Boolean default for string field** — `optional string x = 1 [default = true];` — Go likely accepts, C++ rejects
- **Float default for integer field** — `optional int32 x = 1 [default = 1.5];` — Go likely accepts, C++ rejects
- **Default value type validation** — all type mismatches between default value token type and field type

### Run 109 — String default value on integer field (FAILED: 5/5 profiles)
- **Test:** `115_string_default_int` — proto2 message with `optional int32 count = 1 [default = "42"];` (string literal instead of integer for int32 field default)
- **Bug:** Go protoc-go silently accepts a string literal `"42"` as a default value for an int32 field and stores `default_value = "42"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:39: Expected integer for field default value.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go` — `case "default"` stores `valTok.Value` as the default value without validating that the token type matches the field type. For int32/int64/uint32/uint64/sint32/sint64/fixed32/fixed64/sfixed32/sfixed64 fields, the value must be an integer literal (`TokenInt`). C++ protoc's `ParseDefaultAssignment` dispatches based on field type, calling `ConsumeSignedInteger` for integer fields. The Go parser has zero default value type validation — any token type is accepted for any field type. Same category as Run 108 (integer for string field), but reversed direction.

### Known gaps still unexplored (updated):
- **Boolean default for string field** — `optional string x = 1 [default = true];` — Go likely accepts, C++ rejects
- **Float default for integer field** — `optional int32 x = 1 [default = 1.5];` — Go likely accepts, C++ rejects
- **Enum default for wrong enum type** — `optional OtherEnum x = 1 [default = WRONG_VALUE];` — C++ validates enum membership
- **Oneof inside oneof** — nested oneof — C++ rejects, Go may accept
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **RPC type referencing non-existent message** — C++ rejects, Go likely accepts

### Run 110 — Float default value on integer field (FAILED: 5/5 profiles)
- **Test:** `116_float_default_int` — proto2 message with `optional int32 threshold = 1 [default = 1.5];` and `optional int64 big_value = 2 [default = 3.14];` (float literals instead of integers for integer field defaults)
- **Bug:** Go protoc-go silently accepts float literals `1.5` and `3.14` as default values for int32/int64 fields and stores `default_value = "1.5"` / `"3.14"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:43: Expected integer for field default value.` and `test.proto:7:43: Expected integer for field default value.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go` — `case "default"` stores `valTok.Value` as the default value without validating that the token type matches the field type. For integer fields (int32/int64/uint32/uint64/etc.), the value must be an integer literal (`TokenInt`), not a float literal (`TokenFloat`). C++ protoc's `ParseDefaultAssignment` dispatches based on field type, calling `ConsumeSignedInteger` for integer fields which rejects float tokens. The Go parser has zero default value type validation — any token type is accepted for any field type. Same category as Runs 108 (integer for string) and 109 (string for integer).

### Known gaps still unexplored (updated):
- **Boolean default for string field** — `optional string x = 1 [default = true];` — Go likely accepts, C++ rejects
- **Enum default for wrong enum type** — `optional OtherEnum x = 1 [default = WRONG_VALUE];` — C++ validates enum membership
- **Oneof inside oneof** — nested oneof — C++ rejects, Go may accept
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **RPC type referencing non-existent message** — C++ rejects, Go likely accepts
- **Float default for integer field** — TESTED in Run 110 (116_float_default_int), confirmed broken
- **Boolean default for string field** — TESTED locally, Go now rejects identically to C++ — NOT a gap

### Run 111 — Nested oneof (FAILED: 5/5 profiles)
- **Test:** `117_nested_oneof` — proto3 message with `oneof outer { string text = 2; oneof inner { int32 count = 3; bool flag = 4; } }` (oneof nested inside another oneof)
- **Bug:** Both C++ and Go reject the file (exit 1), but with different error messages. C++ protoc: `test.proto:9:17: Missing field number.` Go protoc-go: `test.proto:line 9:17: expected "=", got "{"`. C++ treats `inner` as a field name and expects a `=` + field number. Go's `parseField` treats `oneof` as a type name, `inner` as the field name, and then expects `=` but gets `{`. The error messages differ in both content and format.
- **Root cause:** `parseOneof()` body parsing loop falls through to `parseField()` for any non-`option`/`";"` token. When `oneof` appears inside a oneof body, `parseField` treats `oneof` as a type name (message reference) and `inner` as the field name. C++ protoc's parser handles `oneof` differently — it recognizes `inner` as a potential field name but then expects `=` and a field number, producing "Missing field number." Both reject correctly, but error messages differ.

### Run 112 — Multiline string literal (FAILED: 5/5 profiles)
- **Test:** `118_multiline_string` — proto3 file with `option java_package = "hello\nworld";` where the string contains a literal newline character (not `\n` escape, but an actual line break between `hello` and `world`)
- **Bug:** Go protoc-go silently accepts the multiline string and produces a valid descriptor with `java_package = "hello\nworld"` (exit 0). C++ protoc rejects with: `test.proto:5:29: Multiline strings are not allowed. Did you miss a "?.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `tokenizer.go:261` — `readString()` loop condition is `t.input[t.pos] != quote`. It only stops at the matching quote character or end of input. There is no check for `\n` (newline). C++ protoc's `Tokenizer::ConsumeString()` in `tokenizer.cc` stops at `\n` and reports "Multiline strings are not allowed." The Go tokenizer needs to add `&& t.input[t.pos] != '\n'` to the loop condition (or check inside the loop and return an error).

### Run 113 — Undefined RPC type (FAILED: 5/5 profiles)
- **Test:** `119_undefined_rpc_type` — proto3 file with `rpc Process(NonExistent) returns (Response);` where `NonExistent` is never defined as a message
- **Bug:** Go protoc-go silently accepts a reference to an undefined message type and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:10:15: "NonExistent" is not defined.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that all type references (including RPC input/output types) resolve to defined types. The Go `descriptor/pool.go` is an empty stub with no undefined type validation. The parser stores the type name string without checking whether it resolves to any defined type. Same category as Run 105 (enum as RPC type) — Go performs zero type resolution validation for RPC methods.

### Run 114 — Required label on extension fields (FAILED: 5/5 profiles)
- **Test:** `120_required_extension` — proto2 file with `extend Base { required string nickname = 100; }` (required label on extension field)
- **Bug:** Go protoc-go silently accepts `required` on extension fields and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:10:12: The extension reqext.nickname cannot be required.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that extension fields cannot have `required` label — only `optional` or `repeated` are allowed. The Go `descriptor/pool.go` is an empty stub with no extension label validation. The `parseExtend` function calls `parseField` which accepts all labels without checking the extension context.

### Run 115 — Duplicate extension field numbers (FAILED: 5/5 profiles)
- **Test:** `121_duplicate_extension_number` — proto2 file with two `extend Base` blocks both defining extensions with field number 100
- **Bug:** Go protoc-go silently accepts duplicate extension field numbers and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:15:26: Extension number 100 has already been used in "extdup.Base" by extension "extdup.name".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that extension field numbers must be unique across all extend blocks targeting the same message. The Go `descriptor/pool.go` is an empty stub with no duplicate extension number validation. The parser stores all extension fields without checking for number conflicts across extend blocks.

### Run 116 — Map field inside extend block (FAILED: 5/5 profiles)
- **Test:** `122_map_in_extend` — proto2 file with `message Base { extensions 100 to 200; }` and `extend Base { map<string, string> metadata = 100; }` (map field inside an extend block)
- **Bug:** Both C++ and Go reject the file (exit 1), but with different error messages. C++ protoc: `test.proto:11:6: Map fields are not allowed to be extensions.` Go protoc-go: `test.proto:11:6: Expected identifier.` C++ parses the map field syntactically (via `ParseMessageField` which handles `map<...>`), then rejects it during validation. Go's `parseExtend` calls `parseField` which doesn't handle `map<...>` syntax — it reads `map` as a type name, then `<` is not a valid field name identifier.
- **Root cause:** `parser.go:840-865` — `parseExtend` only calls `parseField()`, which doesn't handle `map<K,V>` syntax. Map fields are handled separately via `parseMapField()` which is only called from `parseMessage`'s `case "map":` switch. C++ protoc's `ParseExtend` calls `ParseMessageField` which handles all field syntaxes including maps. The error message content and context differ: C++ gives a domain-specific validation error, Go gives a generic parse error.

### Known gaps still unexplored (updated):
- **Enum default for wrong enum type** — `optional OtherEnum x = 1 [default = WRONG_VALUE];` — C++ validates enum membership
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **Undefined field type** — `message Foo { NonExistent x = 1; }` — Go may or may not handle (has resolveMessageFieldsWithErrors)
- **Extension with default value** — `extend Base { optional string tag = 100 [default = "hello"]; }` — may differ
- **`oneof` inside extend block** — C++ rejects differently than Go
- **Extension field name conflict with base message fields** — C++ validates, Go likely doesn't
- **Map in extend** — TESTED in Run 116 (122_map_in_extend), confirmed broken (different error messages)
- **Group inside extend** — `extend Base { group Foo = 100 { ... } }` — Go might handle differently
- **Bool default on integer field** — TESTED in Run 117 (123_bool_default_int), confirmed broken (Go accepts, C++ rejects)

### Run 117 — Boolean default value on integer field (FAILED: 5/5 profiles)
- **Test:** `123_bool_default_int` — proto2 message with `optional int32 enabled = 1 [default = true];` and `optional int64 flags = 2 [default = false];` (boolean identifiers instead of integer literals for integer field defaults)
- **Bug:** Go protoc-go silently accepts boolean identifiers `true`/`false` as default values for integer fields and stores `default_value = "true"` / `"false"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:41: Expected integer for field default value.` and `test.proto:7:39: Expected integer for field default value.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go` — `case "default"` stores `valTok.Value` as the default value without validating that the token type matches the field type. For integer fields (int32/int64/uint32/uint64/etc.), the value must be an integer literal (`TokenInt`), not a boolean identifier. C++ protoc's `ParseDefaultAssignment` dispatches based on field type, calling `ConsumeSignedInteger` for integer fields which rejects non-integer tokens. Same category as Runs 108-110 (default value type validation).

### Run 118 — String literal for bool default value (FAILED: 5/5 profiles)
- **Test:** `124_string_default_bool` — proto2 message with `optional bool verbose = 1 [default = "true"];` and `optional bool debug = 2 [default = "false"];` (string literals instead of identifiers for bool field defaults)
- **Bug:** Go protoc-go silently accepts string literals `"true"`/`"false"` as default values for bool fields and stores `default_value = "true"` / `"false"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:40: Expected "true" or "false".` and `test.proto:7:38: Expected "true" or "false".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2431-2474` — `case "default"` validates string/bytes fields (require TokenString) and integer fields (reject TokenString/TokenFloat/TokenIdent), but has NO validation for bool fields. Bool fields accept any token type — a TokenString with decoded value `"true"` passes through and is stored as `default_value = "true"`. C++ protoc's `ParseDefaultAssignment` dispatches based on field type, calling `ConsumeIdentifier` for bool fields which only accepts identifier tokens (`true`/`false`), not string literal tokens. Same category as Runs 108-110, 117 (default value type validation).

### Known gaps still unexplored (updated):
- **String literal for float default** — `optional float ratio = 1 [default = "1.5"];` — Go likely accepts, C++ rejects
- **Enum default for wrong enum type** — `optional OtherEnum x = 1 [default = WRONG_VALUE];` — C++ validates enum membership
- **Oneof inside oneof** — nested oneof — C++ rejects differently than Go (tested Run 111, error messages differ)
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **RPC type referencing non-existent message** — TESTED in Run 113 (119_undefined_rpc_type), confirmed broken
- **Missing message options** — `map_entry` (field 7)
- **Extension range options** — TESTED in Run 104 (110_extension_range_options), confirmed broken

### Run 119 — String literal for float default value (FAILED: 5/5 profiles)
- **Test:** `125_string_default_float` — proto2 message with `optional float ratio = 1 [default = "1.5"];` and `optional double scale = 2 [default = "3.14"];` (string literals instead of float literals for float/double field defaults)
- **Bug:** Go protoc-go silently accepts string literals `"1.5"`/`"3.14"` as default values for float/double fields and stores `default_value = "1.5"` / `"3.14"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:40: Expected number for field default value.` and `test.proto:7:41: Expected number for field default value.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go` — `case "default"` stores `valTok.Value` as the default value without validating that the token type matches the field type. For float/double fields, the value must be a numeric literal (`TokenInt` or `TokenFloat`) or special identifiers (`inf`, `nan`), not a string literal (`TokenString`). C++ protoc's `ParseDefaultAssignment` dispatches based on field type, calling `ConsumeNumber` for float fields which rejects string literal tokens. Same category as Runs 108-110, 117-118 (default value type validation).

### Run 120 — Enum default value with nonexistent enum member (FAILED: 5/5 profiles)
- **Test:** `126_enum_default_invalid` — proto2 message with `optional Priority level = 1 [default = NONEXISTENT];` where `NONEXISTENT` is not a member of the `Priority` enum
- **Bug:** Go protoc-go silently accepts a default value that names a nonexistent enum member and stores `default_value = "NONEXISTENT"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:12:42: Enum type "enumdefault.Priority" has no value named "NONEXISTENT".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` during linking that enum default values must name a valid member of the field's enum type. The Go `descriptor/pool.go` is an empty stub with no enum default value validation. The parser stores the raw identifier string as `default_value` without checking if it resolves to a valid enum value in the enum type.

### Known gaps still unexplored (updated):
- **Enum default for wrong enum type** — TESTED in Run 120 (126_enum_default_invalid), confirmed broken (no enum value validation)
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **Missing message options** — `map_entry` (field 7)
- **String literal for float default** — TESTED in Run 119 (125_string_default_float), confirmed broken
- **Syntax string concatenation** — TESTED in Run 121 (127_syntax_concat), confirmed broken (Go rejects, C++ accepts)
- **Enum default from wrong enum** — `optional EnumA x = 1 [default = ENUM_B_VALUE];` — C++ validates membership
- **Import path string concatenation** — `import "base" ".proto";` — same single-token bug
- **Package name string concatenation** — probably not valid since package uses identifier not string

### Run 121 — Syntax string concatenation (FAILED: 5/5 profiles)
- **Test:** `127_syntax_concat` — file with `syntax = "proto" "3";` (adjacent string literals for syntax value)
- **Bug:** Go protoc-go reads only the first string token `"proto"` as the syntax value, then expects `;` but gets the second string token `"3"`. Error: `test.proto:1:18: Expected ";".` (exit 1). C++ protoc concatenates adjacent string literals per the protobuf language spec, producing `"proto3"`, and successfully parses the file (exit 0). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:249` — `p.tok.ExpectString()` reads a single string token. No loop to check for and concatenate subsequent adjacent string tokens. C++ protoc's `ConsumeString()` loops over adjacent string literals and concatenates them. Same root cause as Run 25 (file option string concatenation) and Run 59 (field default string concatenation) — the string concatenation pattern is missing throughout the parser. This specific instance affects the syntax declaration, which is critical for determining how the rest of the file is parsed.

### Run 122 — Import path string concatenation (FAILED: 5/5 profiles)
- **Test:** `128_import_concat` — proto3 file with `import "base" ".proto";` (adjacent string literals for import path)
- **Bug:** `parseImport()` at line 368 uses `p.tok.ExpectString()` which reads a single string token `"base"`. Then line 372 expects `;` but gets the second string token `".proto"`. Error: Go rejects the file (exit 1). C++ protoc concatenates adjacent string literals per the protobuf language spec, producing `"base.proto"`, and successfully parses the file (exit 0). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:368` — `p.tok.ExpectString()` reads a single string token. No loop to check for and concatenate subsequent adjacent string tokens. C++ protoc's `ConsumeString()` loops over adjacent string literals and concatenates them. Same root cause as Run 25 (file option string concatenation), Run 59 (field default string concatenation), and Run 121 (syntax string concatenation) — the string concatenation pattern is missing throughout the parser. This instance affects import path resolution, breaking multi-part import declarations.

### Run 123 — Packed option on non-repeated field (FAILED: 5/5 profiles)
- **Test:** `129_packed_nonrepeated` — proto3 message with `int32 count = 1 [packed = true];` (packed on a non-repeated field)
- **Bug:** Go protoc-go silently accepts `[packed = true]` on a non-repeated field and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:3: [packed = true] can only be specified for repeated primitive fields.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` via `is_packable()` which requires `is_repeated()` to be true and the field to be a numeric primitive type. The Go parser stores `FieldOptions.Packed = true` without checking whether the field is repeated or a packable type. Same validation gap pattern as all other missing descriptor pool validations.

### Run 124 — Packed option on repeated string/bytes (FAILED: 5/5 profiles)
- **Test:** `130_packed_string` — proto3 message with `repeated string tags = 1 [packed = true];` and `repeated bytes data = 2 [packed = true];` (packed on non-numeric repeated fields)
- **Bug:** Both C++ and Go reject the file with the same error message `[packed = true] can only be specified for repeated primitive fields.`, but the column numbers differ. C++ protoc reports column 12 (pointing to the field name token). Go protoc-go reports column 3 (pointing to the start of the `repeated` keyword). Both exit 1, but stderr differs due to column positions.
- **Root cause:** Go's packed validation (likely in `compiler/cli/cli.go`) reports the error position as the start of the field declaration (column 3, at `repeated`). C++ protoc's `descriptor.cc` reports the error position as the field name column (column 12). The validation logic correctly identifies non-packable types, but the error location metadata points to a different token. The `repeated int32 ids = 3 [packed = true]` field is correctly accepted by both since int32 is a packable type.

### Known gaps still unexplored (updated):
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **Missing message options** — `map_entry` (field 7)
- **Enum default from wrong enum** — `optional EnumA x = 1 [default = ENUM_B_VALUE];` — C++ validates membership
- **Oneof field with packed option** — same validation gap
- **`lazy` option on non-message field** — TESTED in Run 125 (131_lazy_nonmessage), confirmed broken (Go accepts, C++ rejects)
- **Error column positions** — many Go validation errors report wrong column (start of line vs specific token)

### Run 125 — Lazy option on non-message field (FAILED: 5/5 profiles)
- **Test:** `131_lazy_nonmessage` — proto3 message with `string name = 1 [lazy = true];` (lazy on a string field)
- **Bug:** Go protoc-go silently accepts `[lazy = true]` on a non-message field and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:3: [lazy = true] can only be specified for submessage fields.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that `lazy` and `unverified_lazy` can only be specified for singular embedded message fields (not repeated, not scalar types). The Go parser stores `FieldOptions.Lazy = true` without checking whether the field type is a message. Same validation gap pattern as all other missing descriptor pool validations.

### Run 126 — Extending undefined message type (FAILED: 5/5 profiles)
- **Test:** `132_extend_undefined` — proto2 file with `extend NonExistent { optional string tag = 100; }` where `NonExistent` is never defined as a message
- **Bug:** Both C++ and Go reject the file (exit 1), but with completely different error messages. C++ protoc: `test.proto:9:8: "NonExistent" is not defined.` — catches the undefined type at the `extend` declaration. Go protoc-go: `test.proto:10:25: "NonExistent" does not declare 100 as an extension number.` — doesn't check if the extendee exists, but a downstream extension range validation produces a different error. Error messages differ in content, line number, and column.
- **Root cause:** `CheckUnresolvedTypes` in `parser.go:3080-3148` checks message field types (line 3107-3108) and RPC input/output types (lines 3111-3144), but does NOT check extendee types in `fd.GetExtension()`. The extendee name `NonExistent` is never validated as a defined type. Instead, the extension range validation in `cli.go` fires later because `NonExistent` (as an undefined type) has no declared extension ranges, producing a semantically different error. C++ protoc catches the undefined type first during linking in `descriptor.cc`.

### Run 127 — jstype on non-int64 field (FAILED: 5/5 profiles)
- **Test:** `133_jstype_nonint64` — proto3 message with `int32 count = 1 [jstype = JS_STRING];` and `string name = 2 [jstype = JS_NUMBER];` (jstype on non-int64 fields)
- **Bug:** Go protoc-go silently accepts `jstype` on non-int64 fields and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:3: jstype is only allowed on int64, uint64, sint64, fixed64 or sfixed64 fields.` (exit 1 for each field). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2529-2540` — `case "jstype"` stores the jstype option on `FieldOptions` without checking the field's type. No validation in `compiler/cli/cli.go` either. C++ protoc validates in `descriptor.cc` that `jstype` can only be used on 64-bit integral fields (int64/uint64/sint64/fixed64/sfixed64). Same gap applies to `ctype` — Go likely accepts `ctype = CORD` on non-string fields without validation.

### Known gaps still unexplored (updated):
- **Package conflict** — two files with different packages imported together
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **Missing message options** — `map_entry` (field 7)
- **Enum default from wrong enum** — `optional EnumA x = 1 [default = ENUM_B_VALUE];` — C++ validates membership
- **Oneof field with packed option** — same validation gap
- **Error column positions** — many Go validation errors report wrong column (start of line vs specific token)
- **Undefined extension field type** — `extend Base { optional NonExistent foo = 100; }` — checkMsgUnresolved doesn't check extension field types
- **Negative enum value overflow** — `FOO = -2147483649;` — silent truncation of absolute value
- **Minimum int32 enum value** — `FOO = -2147483648;` — ParseInt overflow on absolute value even though -2^31 is valid
- **ctype on non-string field** — `int32 x = 1 [ctype = CORD];` — tested, NOT a gap (C++ also accepts)
- **jstype on non-int64 field** — TESTED in Run 127 (133_jstype_nonint64), confirmed broken
- **Undefined extension field type** — TESTED in Run 128 (134_ext_field_undefined), confirmed broken (Go accepts, C++ rejects)

### Run 128 — Undefined extension field type (FAILED: 5/5 profiles)
- **Test:** `134_ext_field_undefined` — proto2 file with `message Base { extensions 100 to 200; }` and `extend Base { optional NonExistent payload = 100; }` where `NonExistent` is never defined as a message or enum
- **Bug:** Go protoc-go silently accepts an extension field with an undefined type and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:11:12: "NonExistent" is not defined.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2919-2925` — `resolveAndSetTypes` resolves extension field types via `resolveTypeName` and looks them up in the `types` map. If the type is found, it sets `ext.Type`. But if the type is NOT found, it silently continues — no error is appended. Compare to the extendee check at lines 2912-2917 which DOES report errors for undefined extendees, and `checkMsgUnresolved` at lines 3173-3183 which reports errors for undefined message field types. The extension field type validation is simply missing — the `if tp, ok := types[resolved]; ok` check at line 2922 has no corresponding `else` branch to report the undefined type.

### Run 130 — Unicode escape sequences (FAILED: 5/5 profiles)
- **Test:** `136_unicode_escape` — proto3 file with `option java_package = "\u0048ello";` where `\u0048` is a Unicode escape for 'H' (U+0048)
- **Bug:** Go tokenizer's `readString()` at lines 278-325 has no handling for `\u` (4-digit Unicode escape) or `\U` (8-digit Unicode escape). The `\u` falls to the `default` case at line 323, which writes literal `u`. Subsequent `0048ello` is read as normal characters. Result: Go produces `java_package = "u0048ello"` (10 chars), C++ protoc produces `java_package = "Hello"` (5 chars, since `\u0048` → 'H'). Binary descriptor payloads differ.
- **Root cause:** `tokenizer.go:278-325` — the escape sequence switch handles `n`, `t`, `r`, `a`, `b`, `f`, `v`, `\\`, `'`, `"`, `?`, `x`/`X` (hex), `0-7` (octal), but NOT `u` (Unicode 4-digit) or `U` (Unicode 8-digit). C++ protoc's `ConsumeStringAppend` in `tokenizer.cc` handles `\u` by reading 4 hex digits and converting to a UTF-8 encoded codepoint, and `\U` by reading 8 hex digits. The fix: add `case 'u':` and `case 'U':` branches that read 4/8 hex digits, convert to a Unicode codepoint, and encode as UTF-8.

### Run 129 — Hex escape reads too many digits (FAILED: 5/5 profiles)
- **Test:** `135_hex_escape_digits` — proto3 file with `option java_package = "com.example.\x4Eelson";` where `\x4E` is a hex escape followed by `e` (also a hex digit) and `lson`
- **Bug:** Go tokenizer's `readString()` hex escape handler at lines 301-310 reads ALL hex digits greedily (unlimited loop: `for t.pos < len(t.input) && isHexDigit(t.input[t.pos])`). C++ protoc's `ConsumeStringAppend` reads at most 2 hex digits. For `\x4Eelson`: Go reads `4Ee` (3 hex digits) → `byte(0x4Ee) = byte(0xEE)`, leaving `lson`. C++ reads `4E` (2 hex digits) → `byte(0x4E) = 'N'`, leaving `elson`. String values differ: Go produces `"com.example.\xEElson"` (invalid UTF-8), C++ produces `"com.example.Nelson"`. Binary descriptor sizes differ (101 vs 100 bytes for descriptor_set). Plugin profiles fail because Go's invalid UTF-8 causes JSON marshaling error in protoc-gen-dump.
- **Root cause:** `tokenizer.go:305-307` — the hex escape loop reads hex digits without a 2-digit limit. C++ protoc uses `TryConsumeOne` twice (max 2 digits). The fix: add a counter to limit hex digit consumption to 2, matching C++ behavior. This affects any string containing `\xHH` followed by additional hex-digit characters (0-9, a-f, A-F).

### Run 131 — Numeric package name (FAILED: 5/5 profiles)
- **Test:** `137_numeric_package` — proto3 file with `package 123;` (integer literal instead of identifier for package name)
- **Bug:** Go protoc-go silently accepts a numeric package name and produces a valid descriptor with `package = "123"` (exit 0). C++ protoc rejects with: `test.proto:3:9: Expected identifier.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:332` — `nameTok := p.tok.Next()` reads the next token without any type check. An integer token `TokenInt("123")` is accepted as the package name. C++ protoc's `ParsePackage` calls `ConsumeIdentifier` which requires `TYPE_IDENTIFIER`, rejecting integer tokens. The Go parser should use `p.tok.ExpectIdent()` instead of `p.tok.Next()` to validate the package name is an identifier.

### Run 132 — Empty extend block (FAILED: 5/5 profiles)
- **Test:** `138_empty_extend` — proto2 file with `message Base { extensions 100 to 200; }` and `extend Base { }` (empty extend block, no fields inside)
- **Bug:** Go protoc-go silently accepts an empty extend block and produces a valid descriptor (exit 0). C++ protoc rejects with an error expecting at least one field inside the extend block (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:864` — `parseExtend` uses a `for p.tok.Peek().Value != "}"` loop which exits immediately when the extend block is empty. C++ protoc's `ParseExtend` in `parser.cc` uses a `do { ... } while (...)` loop that requires at least one field to be parsed. The Go parser should either check that at least one field was parsed inside the extend block, or use a do-while pattern.

### Known gaps still unexplored (updated):
- **Package name with leading dot** — `package .foo;` — C++ may reject, Go may accept
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context
- **`reserved` as type name** — same pattern
- **`extensions` as type name** — same pattern
- **Missing message options** — `map_entry` (field 7)
- **Enum default from wrong enum** — `optional EnumA x = 1 [default = ENUM_B_VALUE];` — C++ validates membership
- **Error column positions** — many Go validation errors report wrong column
- **String literal as package name** — `package "foo";` — Go likely accepts, C++ rejects (same missing type check)
- **Numeric message/enum/service name** — `message 123 {}` — Go uses ExpectIdent, probably rejects
- **Integer syntax value** — `syntax = 3;` — Go's parseSyntax uses ExpectString, probably rejects
- **Empty extend block** — TESTED in Run 132 (138_empty_extend), confirmed broken (Go accepts, C++ rejects)
- **Empty nested extend block** — `message Foo { extend Base { } }` — same issue in `parseNestedExtend`

### Run 133 — Group inside extend block (FAILED: 5/5 profiles)
- **Test:** `139_group_in_extend` — proto2 file with `message Base { extensions 100 to 200; }` and `extend Base { optional group MyGroup = 100 { optional string name = 1; } }`
- **Bug:** Go protoc-go rejects the file with: `group_extend.proto:9:32: Expected ";".` (exit 1). C++ protoc accepts it and produces a valid descriptor with a TYPE_GROUP extension field and a nested DescriptorProto for `MyGroup` (exit 0). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:840-865` — `parseExtend` calls `parseField()` for each field inside the extend block. `parseField` reads `optional` as label, then `group` as the type name (treated as a message reference, not the group keyword), then `MyGroup` as the field name, then `=` and `100`, then expects `;` but gets `{`. The `isGroupField` check (which handles the `group` keyword) only exists in the message body's `default` case (line 522), not in `parseExtend`. C++ protoc's `ParseExtend` calls `ParseMessageField` which handles both regular fields and group fields. The fix: add group detection in `parseExtend` similar to the message body parser.

### Known gaps still unexplored (updated):
- **Package name with leading dot** — `package .foo;` — C++ may reject, Go may accept
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **`option` as type name** — Go switch matches keyword before checking context (but C++ also matches)
- **Missing message options** — `map_entry` (field 7)
- **Enum default from wrong enum** — `optional EnumA x = 1 [default = ENUM_B_VALUE];` — C++ validates membership
- **Error column positions** — many Go validation errors report wrong column
- **String literal as package name** — `package "foo";` — Go rejects (fixed: type check added at line 333)
- **Empty nested extend block** — `message Foo { extend Base { } }` — same issue in `parseNestedExtend`
- **Group inside nested extend** — `message Foo { extend Base { optional group G = 100 { } } }` — same issue
- **Labeled map field (optional/repeated)** — both reject but different error messages
- **`service` as a message name** — valid in C++, should work in Go too (not a keyword in message body switch)
- **Nested group in oneof** — TESTED in Run 134 (140_group_in_oneof), confirmed broken (Go rejects, C++ accepts)

### Run 134 — Group inside oneof (FAILED: 5/5 profiles)
- **Test:** `140_group_in_oneof` — proto2 message with `oneof choice { group MyGroup = 1 { optional string name = 1; } string text = 2; }` (group field inside a oneof body)
- **Bug:** Go protoc-go rejects the file with: `test.proto:7:23: Expected ";".` (exit 1). C++ protoc accepts it and produces a valid descriptor with a TYPE_GROUP field inside the oneof and a nested DescriptorProto for `MyGroup` (exit 0). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2154-2179` — `parseOneof` body loop calls `parseField()` for each field. When `group` appears, `parseField` reads `group` as a type name (message reference), `MyGroup` as the field name, `=` and `1` as field number, then expects `;` but gets `{`. The `isGroupField` check (which handles the `group` keyword) only exists in the message body parser's `default` case, not in `parseOneof`. C++ protoc's `ParseMessageField` handles group fields in all contexts (message body, oneof, extend). The Go parser needs group detection in `parseOneof` similar to the message body parser.

### Run 135 — Default value on repeated field (FAILED: 5/5 profiles)
- **Test:** `141_repeated_default` — proto2 message with `repeated int32 values = 1 [default = 42];` and `repeated string names = 2 [default = "hello"];` (default values on repeated fields)
- **Bug:** Go protoc-go silently accepts `[default = ...]` on repeated fields and produces a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:40: Repeated fields can't have default values.` and `test.proto:7:40: Repeated fields can't have default values.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** No validation layer in Go implementation. C++ protoc validates in `descriptor.cc` that repeated fields cannot have default values. The Go `descriptor/pool.go` is an empty stub with no default-on-repeated validation. The parser stores `default_value` on the field regardless of label. Same validation gap pattern as all other missing descriptor pool validations.

### Run 136 — Negative enum reserved ranges (FAILED: 5/5 profiles)
- **Test:** `142_negative_enum_reserved` — proto2 enum with `reserved -20 to -15;` and `reserved -5;` (negative numbers in enum reserved ranges)
- **Bug:** Go protoc-go rejects the file with: `test.proto:10:12: Expected integer.` (exit 1). C++ protoc accepts it and produces a valid descriptor with negative `EnumReservedRange` entries (exit 0). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:1843` — `parseEnumReserved` calls `p.tok.ExpectInt()` which strictly requires `TokenInt`. A `-` token is `TokenSymbol`, so `ExpectInt()` fails immediately. C++ protoc's enum reserved range parser checks for a leading `-` token before consuming the integer, allowing negative reserved ranges. The fix: check for `-` before calling `ExpectInt()`, negate the parsed value when `-` is present. Same pattern as enum value parsing (which already handles `-` at line 1688-1690). This affects both single negative numbers (`reserved -5;`) and negative ranges (`reserved -20 to -15;`).

### Run 137 — Default value on message-typed field (FAILED: 5/5 profiles)
- **Test:** `143_message_default` — proto2 message with `optional Inner child = 1 [default = "test"];` where `Inner` is a message type
- **Bug:** Both C++ and Go reject the file (exit 1), but with different error messages. C++ protoc: `test.proto:10:39: Messages can't have default values.` Go protoc-go: `test.proto:10:39: Expected number.` C++ correctly identifies that message-typed fields cannot have default values. Go doesn't recognize the field as message-typed at parse time and produces a generic type mismatch error ("Expected number" because unresolved named types fall through to the numeric default parsing path).
- **Root cause:** `parser.go` — `case "default"` in `parseFieldOptions` doesn't have special handling for message-typed fields. When the field type is an unresolved reference (a named type like `Inner`), the parser doesn't know if it's a message or enum. C++ protoc's `ParseDefaultAssignment` in `parser.cc` handles this by checking if the type is a message reference and immediately rejecting with "Messages can't have default values." The Go parser falls through to a generic number parsing path, producing the wrong diagnostic.

### Run 138 — Negative default on unsigned integer fields (FAILED: 5/5 profiles)
- **Test:** `144_negative_unsigned_default` — proto2 message with `optional uint32 value = 1 [default = -5];` and `optional uint64 large = 2 [default = -100];` (negative defaults on unsigned integer fields)
- **Bug:** Go protoc-go silently accepts negative default values on unsigned integer fields and stores `default_value = "-5"` / `"-100"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:41: Unsigned field can't have negative default value.` and `test.proto:7:41: Unsigned field can't have negative default value.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2803-2805` — when `negative == true`, the parser prepends `"-"` to `defVal` regardless of the field's type. No check for unsigned types (uint32/uint64/fixed32/fixed64). C++ protoc's `ParseDefaultAssignment` in `parser.cc` calls `ConsumeUnsignedInteger` for unsigned fields, which rejects negative values. The Go parser should check if the field type is unsigned and reject negative defaults for those types.

### Known gaps still unexplored (updated):
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **Missing message options** — `map_entry` (field 7)
- **Enum default from wrong enum** — `optional EnumA x = 1 [default = ENUM_B_VALUE];` — C++ validates membership
- **Error column positions** — many Go validation errors report wrong column
- **Empty nested extend block** — `message Foo { extend Base { } }` — same issue in `parseNestedExtend`
- **Negative message reserved ranges** — `reserved -5 to -1;` in a message — C++ rejects negative reserved in messages (field numbers are positive), but may produce different error messages
- **Negative extension range start** — `extensions -1 to 10;` — C++ rejects, Go may also reject but with different error
- **Default on message field** — TESTED in Run 137 (143_message_default), confirmed broken (different error messages)
- **Negative unsigned default** — TESTED in Run 138 (144_negative_unsigned_default), confirmed broken (Go accepts, C++ rejects)
- **Default value overflow** — TESTED in Run 139 (145_default_overflow), confirmed broken (Go accepts, C++ rejects)

### Run 139 — Default value overflow on integer field (FAILED: 5/5 profiles)
- **Test:** `145_default_overflow` — proto2 message with `optional int32 small = 1 [default = 99999999999];` (value exceeds int32 range)
- **Bug:** Go protoc-go silently accepts the overflowed default value and stores `default_value = "99999999999"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:39: Integer out of range.` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2807-2834` — `case "default"` stores `valTok.Value` as the default value string without validating that the integer value fits within the field's type range. For `int32`, values must be within [-2147483648, 2147483647]. The raw string `"99999999999"` is stored directly as `default_value`. C++ protoc's `ParseDefaultAssignment` calls `ConsumeSignedInteger` which validates range. The Go parser should parse the integer and check range based on the field type (int32/int64/uint32/uint64/etc.).

### Run 140 — Integer default value on bool field (FAILED: 5/5 profiles)
- **Test:** `146_int_default_bool` — proto2 message with `optional bool verbose = 1 [default = 1];` and `optional bool debug = 2 [default = 0];` (integer literals instead of identifiers for bool field defaults)
- **Bug:** Go protoc-go silently accepts integer literals `1`/`0` as default values for bool fields and stores `default_value = "1"` / `"0"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:40: Expected "true" or "false".` and `test.proto:7:38: Expected "true" or "false".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2782-2788` — bool field default validation only rejects `TokenString` tokens. Integer tokens (`TokenInt`) with value `1` or `0` pass through all validation checks and are stored as `default_value`. C++ protoc's `ParseDefaultAssignment` uses `ConsumeIdentifier` for bool fields, which only accepts identifier tokens `true`/`false`, rejecting integer and float tokens. Same category as Runs 108-110, 117-119 (default value type validation). The fix: add `|| valTok.Type == tokenizer.TokenInt || valTok.Type == tokenizer.TokenFloat` to the bool field validation check.

### Run 141 — Float default value on bool field (FAILED: 5/5 profiles)
- **Test:** `147_float_default_bool` — proto2 message with `optional bool verbose = 1 [default = 1.0];` and `optional bool debug = 2 [default = 0.0];` (float literals instead of identifiers for bool field defaults)
- **Bug:** Go protoc-go silently accepts float literals `1.0`/`0.0` as default values for bool fields and stores `default_value = "1"` / `"0"`, producing a valid descriptor (exit 0). C++ protoc rejects with: `test.proto:6:40: Expected "true" or "false".` and `test.proto:7:38: Expected "true" or "false".` (exit 1). The test harness detects exit code mismatch.
- **Root cause:** `parser.go:2783-2788` — bool field default validation at line 2784 checks `valTok.Type == tokenizer.TokenString || valTok.Type == tokenizer.TokenInt` but does NOT check `valTok.Type == tokenizer.TokenFloat`. A float token `1.0` passes through the bool validation and is stored as `default_value`. C++ protoc's `ParseDefaultAssignment` uses `ConsumeIdentifier` for bool fields, which only accepts identifier tokens `true`/`false`, rejecting float tokens. Same category as Run 140 (integer default on bool). The fix: add `|| valTok.Type == tokenizer.TokenFloat` to line 2784.

### Run 142 — Integer default value on enum field (FAILED: 5/5 profiles)
- **Test:** `148_int_default_enum` — proto2 message with `optional Priority level = 1 [default = 0];` and `optional Priority urgency = 2 [default = 2];` (integer literals instead of enum value names for enum field defaults)
- **Bug:** Go protoc-go tries to look up the integer as an enum value name, producing: `Enum type "intenumd.Priority" has no value named "0".` C++ protoc catches it earlier: `Default value for an enum field must be an identifier.` Both reject (exit 1), but error messages differ. C++ validates the token type (must be identifier), Go validates the value name (tries to find "0" in enum members).
- **Root cause:** `parser.go:2763-2770` — when `field.Type == nil` (unresolved named type), the code accepts any token value and stores it. After type resolution, the enum validation in `cli.go` checks if the value name exists in the enum type. But it doesn't check if the token was an integer vs identifier. C++ protoc's `ParseDefaultAssignment` dispatches based on field type at parse time and calls `ConsumeIdentifier` for enum fields, rejecting integer tokens immediately. The fix: after type resolution reveals the field is an enum type, check that the default value token was an identifier, not an integer literal.

### Run 143 — Labeled map fields (FAILED: 5/5 profiles)
- **Test:** `149_labeled_map` — proto3 message with `repeated map<string, string> tags = 1;` (label on a map field)
- **Bug:** Both C++ and Go reject the file (exit 1), but with different error messages. C++ protoc: `test.proto:6:15: Field labels (required/optional/repeated) are not allowed on map fields.` Go protoc-go: `test.proto:6:15: Expected identifier.` C++ recognizes `map<` after the label, parses the map field, then rejects the label with a domain-specific error. Go's message body switch sees `repeated` as the first token (not `map`), falls to the default case, calls `parseField`, which reads `map` as a type name (message reference), then expects a field name identifier but gets `<`.
- **Root cause:** `parser.go:372` — `case "map":` only fires when `map` is the first token in the message body. When a label like `repeated` precedes it, the `default` case calls `parseField`. `parseField` reads `map` as a type name, then `ExpectIdent()` fails on `<`. C++ protoc's `ParseMessageField` reads the label first, then checks for `map<` as a type, handling both labeled and unlabeled map fields. The Go parser needs to check for `map<` inside `parseField` after consuming a label.

### Known gaps still unexplored (updated):
- **Duplicate `import public`** — same file imported as both `import` and `import public`
- **Type shadowing** — same nested type name in different parent messages
- **Map field options source code info** — location ordering may differ from C++ protoc
- **String concatenation in enum/service/method option values** — same single-token bug as field defaults
- **Missing message options** — `map_entry` (field 7)
- **Enum default from wrong enum** — `optional EnumA x = 1 [default = ENUM_B_VALUE];` — C++ validates membership
- **Error column positions** — many Go validation errors report wrong column
- **Empty nested extend block** — `message Foo { extend Base { } }` — same issue in `parseNestedExtend`
- **Negative message reserved ranges** — `reserved -5 to -1;` in a message — C++ rejects negative reserved in messages
- **Negative extension range start** — `extensions -1 to 10;` — C++ rejects, Go may also reject differently
- **Labeled map fields** — TESTED in Run 143 (149_labeled_map), confirmed broken (Go gives "Expected identifier", C++ gives domain-specific label error)
- **`optional map<...>` in proto3** — same issue, `optional` label + map<... gets wrong error
- **`required map<...>` in proto2** — same issue
- **Group name validation** — C++ requires group names start with uppercase, Go likely doesn't validate
