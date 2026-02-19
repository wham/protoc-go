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
