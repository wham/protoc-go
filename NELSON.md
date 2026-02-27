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

### Run 1 — Any type URL expansion in aggregate options (VICTORY)
- **Bug**: Go parser can't handle `[type.googleapis.com/pkg.Type]` syntax in aggregate option values (message literals). The `/` character inside `[...]` is not parsed — the Go parser only handles `.`-separated identifiers in extension references, not the Any type URL expansion that uses `/`.
- **Test**: `332_any_expansion_in_option` — all 9 profiles fail.
- **Root cause**: `consumeAggregate()` in `parser.go:4909-4925` reads extension names as `ident(.ident)*` but the Any URL syntax uses `domain.tld/package.Type` with a `/` separator.
- **C++ protoc**: Accepts it fine (libprotoc 33.4). Produces valid descriptor set.
- **Go protoc-go**: Fails with `Error while parsing option value: Expected ":", found "anyexpand".`

### Run 2 — Line comment at EOF without trailing newline (VICTORY)
- **Bug**: Go tokenizer always appends `\n` to line comment text (`tokenizer.go:242: return text + "\n"`), even when the file ends without a trailing newline. C++ protoc correctly omits the `\n` when the comment is at EOF without a trailing newline.
- **Test**: `333_comment_eof_no_newline` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `readLineCommentText()` in `io/tokenizer/tokenizer.go:242` unconditionally returns `text + "\n"`. Should only add `\n` when there was one in the source.
- **C++ protoc**: `trailing_comments: " eof"` (no trailing newline).
- **Go protoc-go**: `trailing_comments: " eof\n"` (with trailing newline).
- **Discrepancy**: Source code info comment text differs by one byte.

### Run 3 — NaN bit pattern differs in custom double option (VICTORY)
- **Bug**: Go's `strconv.ParseFloat("nan", 64)` returns `0x7FF8000000000001` (Go's canonical NaN), while C++ `std::numeric_limits<double>::quiet_NaN()` returns `0x7FF8000000000000`. These differ by one bit in the mantissa. When encoding a custom `double` option with value `nan`, the wire format bytes differ.
- **Test**: `334_nan_custom_option` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `encodeCustomOptionValue()` in `cli.go` uses `strconv.ParseFloat(value, 64)` then `math.Float64bits(v)`. Go's NaN bit pattern is `0x7FF8000000000001`, C++ is `0x7FF8000000000000`. One bit difference in the lowest mantissa bit.
- **C++ protoc**: Encodes NaN as `0x7FF8000000000000` (8 bytes in descriptor).
- **Go protoc-go**: Encodes NaN as `0x7FF8000000000001` (different 8 bytes in descriptor).
- **Fix hint**: Use `math.Float64frombits(0x7FF8000000000000)` instead of `strconv.ParseFloat("nan", 64)` to match C++ NaN encoding.
- **Also found**: `-nan` would fail entirely — Go's `strconv.ParseFloat("-nan", 64)` returns error. Also, subfield custom option parsers (enum/field/message/service/method) double-negate values (parser bakes `-` into Value AND resolver prepends `-` again).

### Run 4 — Field sub-field custom option with negative value (VICTORY)
- **Bug**: Go parser double-negates field-level sub-field custom options. The parser bakes `-` into `custOpt.Value` at parser.go:5361 (`custOpt.Value = "-" + custOpt.Value`), AND the CLI resolver prepends `-` again at cli.go:4267 (`value = "-" + value` when `opt.Negative` is true). Result: value becomes `--40` instead of `-40`, which fails to parse as an integer.
- **Test**: `335_field_subfield_neg_option` — all 9 profiles fail.
- **Root cause**: Inconsistent negation handling. For file options, the parser does NOT bake `-` into the value (uses `valTok.Value` directly at parser.go:4595). For field options, the parser DOES bake it in (line 5361). But the CLI always checks `opt.Negative` and prepends `-` again for sub-field paths, causing double negation.
- **C++ protoc**: Accepts `[(valid_range).lo = -40]` fine and produces correct descriptor.
- **Go protoc-go**: Fails with `error encoding custom option: invalid integer value: --40`.
- **Fix hint**: Either don't bake `-` into Value in the field option parser, OR don't prepend `-` in the CLI resolver. Pick one place for negation.
- **Scope**: Affects ALL custom option entity types with sub-field paths: file, message, field, enum, enum_value, service, method, oneof. Each has the same pattern in cli.go.

### Run 5 — Negative NaN (`-nan`) as custom double option value (VICTORY)
- **Bug**: Go's `strconv.ParseFloat("-nan", 64)` returns an error (`invalid syntax`), while C++ protoc accepts `-nan` as a valid double value (it's just NaN with a sign bit). The Go compiler fails to encode the option entirely.
- **Test**: `336_neg_nan_option` — all 9 profiles fail.
- **Root cause**: `encodeCustomOptionValue()` in `cli.go` calls `strconv.ParseFloat(value, 64)` with `value = "-nan"` (assembled from `opt.Negative` + `opt.Value`). Go's stdlib rejects `-nan` as invalid syntax.
- **C++ protoc**: Accepts `-nan` fine, produces valid descriptor with NaN-valued option.
- **Go protoc-go**: Fails with `error encoding custom option: invalid double value: -nan`.
- **Fix hint**: Before calling `strconv.ParseFloat`, check if the value (after stripping `-`) is `nan`/`NaN` and handle it specially (NaN has no sign, so `-nan` == `nan`).

### Run 6 — String concatenation missing in field-level custom options (VICTORY)
- **Bug**: Go parser does NOT handle string concatenation for field-level custom options. When a field has `[(my_opt) = "hello" " " "world"]`, the Go parser only captures `"hello"` and then chokes on `" "` as unexpected.
- **Test**: `337_field_option_string_concat` — all 9 profiles fail.
- **Root cause**: `parseFieldOptions()` at parser.go:5355-5363 sets `custOpt.Value = valTok.Value` but never loops to concatenate adjacent string tokens. Compare with file-level custom options (parser.go:1699-1706), enum-level (parser.go:2951-2956), message-level, service-level, method-level, and oneof-level — all of which DO have the string concatenation loop.
- **C++ protoc**: Accepts string concatenation in field options fine.
- **Go protoc-go**: Fails with `Expected ";"` because the second string token is unexpected.
- **Fix hint**: Add this after line 5363: `if valTok.Type == tokenizer.TokenString { for p.tok.Peek().Type == tokenizer.TokenString { next := p.tok.Next(); p.trackEnd(next); custOpt.Value += next.Value } }`
- **Also missing**: Enum value custom options (parser.go:2574-2582) and extension range custom options (parser.go:1138-1153) also lack string concatenation handling. Those are separate bugs to test in future runs.

### Run 7 — String concatenation missing in enum VALUE custom options (VICTORY)
- **Bug**: Go parser does NOT handle string concatenation for enum value custom options. When an enum value has `[(ev_label) = "bright" " " "red"]`, the Go parser only captures `"bright"` and chokes on the next string token.
- **Test**: `338_enum_val_option_string_concat` — all 9 profiles fail.
- **Root cause**: `parseEnumValue` code at parser.go:2574-2582 sets `custOpt.Value = valTok.Value` but never loops to concatenate adjacent string tokens. Same bug pattern as field-level (Run 6), but in a different code path.
- **C++ protoc**: Accepts string concatenation in enum value options fine.
- **Go protoc-go**: Fails with parse error because the second string token is unexpected.
- **Fix hint**: Add after line 2582: `if valTok.Type == tokenizer.TokenString { for p.tok.Peek().Type == tokenizer.TokenString { next := p.tok.Next(); p.trackEnd(next); custOpt.Value += next.Value } }`

### Run 8 — String concatenation missing in extension range custom options (VICTORY)
- **Bug**: Go parser does NOT handle string concatenation for extension range custom options. When an extension range has `[(range_label) = "hello" " " "world"]`, the Go parser only captures `"hello"` and chokes on the next string token expecting `]`.
- **Test**: `339_ext_range_option_string_concat` — all 9 profiles fail.
- **Root cause**: Extension range custom option parsing at parser.go:1138-1153 sets `custOpt.Value = valTok.Value` but never loops to concatenate adjacent string tokens. Same bug pattern as field-level (Run 6) and enum value-level (Run 7).
- **C++ protoc**: Accepts string concatenation in extension range options fine, produces valid descriptor.
- **Go protoc-go**: Fails with `Expected "]"` because the second string token is unexpected.
- **Fix hint**: Add after line 1153: `if valTok.Type == tokenizer.TokenString { for p.tok.Peek().Type == tokenizer.TokenString { next := p.tok.Next(); p.trackEnd(next); custOpt.Value += next.Value } }`

### Run 9 — Scalar sub-field option validation error message mismatch (VICTORY)
- **Bug**: Go compiler produces a different error message than C++ protoc when a sub-field path is used on a scalar (non-message) custom option. C++ correctly identifies the problem as "Option is an atomic type, not a message" with line/column info. Go fails with a generic "unknown message type" error without proper location info.
- **Test**: `340_scalar_subfield_option` — all 9 profiles fail.
- **Root cause**: In `cli.go`, when resolving sub-field paths, the code looks up `msgFieldMap[currentTypeName]` where `currentTypeName` is derived from `ext.GetTypeName()`. For a scalar type like `int32`, `GetTypeName()` returns empty string, so the lookup fails with "unknown message type" instead of a proper "atomic type, not a message" error.
- **C++ protoc**: `test.proto:11:8: Option "(my_scalar)" is an atomic type, not a message.`
- **Go protoc-go**: `test.proto: unknown message type  for extension my_scalar`
- **Fix hint**: Before attempting sub-field resolution, check if `ext.GetType()` is `TYPE_MESSAGE` or `TYPE_GROUP`. If not, emit an error like `Option "(name)" is an atomic type, not a message.` with proper line/column info from `opt.NameTok`.

### Run 10 — Group-typed custom option uses wrong wire format (VICTORY)
- **Bug**: Go's `encodeAggregateOption` and `encodeAggregateFields` in `cli.go` always use `protowire.BytesType` (wire type 2, length-delimited) when encoding aggregate option values. For `TYPE_GROUP` fields, protobuf wire format requires `StartGroupType` (wire type 3) + fields + `EndGroupType` (wire type 4) instead of length-delimited encoding.
- **Test**: `341_group_option_encoding` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `encodeAggregateOption()` at cli.go:5811-5815 always does `protowire.AppendTag(b, fieldNum, protowire.BytesType)` + `protowire.AppendBytes(b, inner)`. Same bug in `encodeAggregateFields()` at cli.go:5896-5900. Neither checks if the field is `TYPE_GROUP`.
- **C++ protoc**: Produces 257-byte descriptor with correct start/end group wire format.
- **Go protoc-go**: Produces 255-byte descriptor with incorrect length-delimited wire format. Binary differs.
- **Fix hint**: In both `encodeAggregateOption` and `encodeAggregateFields`, check if `ext.GetType() == TYPE_GROUP`. If so, use `protowire.AppendTag(b, fieldNum, protowire.StartGroupType)` + inner + `protowire.AppendTag(b, fieldNum, protowire.EndGroupType)` instead of length-delimited encoding. Group encoding: start_tag(3) + fields + end_tag(4). Length-delimited: tag(2) + varint_length + fields.
- **Also affects**: Nested group fields within aggregate options (encodeAggregateFields has same bug).

### Run 11 — Positive sign `+` in aggregate option values produces wrong error message (VICTORY)
- **Bug**: Go parser's `consumeAggregate()` doesn't recognize `+` as a sign prefix. When encountering `count: +42`, the Go parser treats `+` as the entire value of `count`, then treats `42` as a new field name, producing a confusing misleading error. C++ protoc recognizes `+` as an attempted positive sign and produces a clear error.
- **Test**: `342_aggregate_positive_sign` — all 9 profiles fail.
- **Root cause**: `consumeAggregate()` at parser.go:5058-5065 only checks for `-` sign before values, not `+`. When `+` is encountered, it's consumed as the value token (a symbol), and the actual number `42` is misinterpreted as the next field name.
- **C++ protoc**: `test.proto:16:20: Error while parsing option value for "my_opts": Expected integer, got: +`
- **Go protoc-go**: `test.proto:16:20: Error while parsing option value for "my_opts": Expected ":", found "ratio".`
- **Fix hint**: In `consumeAggregate()` (and `consumeAggregateAngle()`, and list value parsing within both), add `+` handling similar to `-`: `if valTok.Value == "+" { valTok = p.tok.Next(); p.trackEnd(valTok) }` — just skip it since positive sign is a no-op. Then encoding would work. Alternatively, emit the same error as C++ protoc: "Expected integer, got: +".
- **Also affects**: Same issue exists in `consumeAggregateAngle()` (line 5232-5239) and list value parsing within both functions.

### Run 12 — mergeUnknownExtensions corrupts repeated options when sub-field options present (VICTORY)
- **Bug**: Go's `mergeUnknownExtensions()` in `cli.go:3459` unconditionally merges ALL BytesType entries with the same field number into a single entry. For **repeated** string/bytes/message custom options, this corrupts the data by concatenating separate repeated entries into one. The merge function is only called when `hasSubFieldCustomOpts` returns true, so the bug only manifests when a file has BOTH sub-field custom options AND repeated custom options.
- **Test**: `343_repeated_with_subfield` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `mergeUnknownExtensions()` is designed to merge sub-field options (e.g., `option (cfg).name = "x"` + `option (cfg).value = 1` → single merged message). But it cannot distinguish between sub-field merges (correct) and repeated field entries (should NOT be merged). When field 50001 has two BytesType entries for repeated values "alpha" and "beta", it concatenates the raw payload bytes into a single entry "alphabeta".
- **C++ protoc**: Keeps repeated entries separate in wire format: `tag(50001,2)+len(5)+"alpha" + tag(50001,2)+len(4)+"beta"` = 310 bytes total descriptor.
- **Go protoc-go**: Merges into single entry: `tag(50001,2)+len(9)+"alphabeta"` = 306 bytes total descriptor. 4-byte difference.
- **Fix hint**: `mergeUnknownExtensions` needs to know which field numbers correspond to sub-field options (message types needing merge) vs repeated fields (must not merge). Either: (1) pass the set of field numbers that should be merged, or (2) check the extension's field descriptor to see if it's `LABEL_REPEATED` before merging, or (3) only merge when `ext.GetType() == TYPE_MESSAGE` and `ext.GetLabel() != LABEL_REPEATED`.

### Ideas for next time
- ~~`-nan` as custom float/double option value — Go errors on `strconv.ParseFloat("-nan")`, C++ accepts it~~ **DONE in Run 5 (336_neg_nan_option)**
- ~~Subfield custom options with negative values on enum/field/message/service/method — double negation bug (parser bakes `-` into Value at line 2945, resolver adds it again at line 4927)~~ **DONE in Run 4 (335_field_subfield_neg_option)**
- ~~String concatenation in field-level custom options — Go parser doesn't concatenate adjacent strings~~ **DONE in Run 6 (337_field_option_string_concat)**
- ~~String concatenation in enum VALUE custom options (parser.go:2574-2582) — same bug as field-level, no concat loop~~ **DONE in Run 7 (338_enum_val_option_string_concat)**
- ~~String concatenation in extension range custom options (parser.go:1138-1153) — same bug, no concat loop~~ **DONE in Run 8 (339_ext_range_option_string_concat)**
- `float` custom option with `nan` — float32 NaN bits may also differ across platforms
- Source code info accuracy for specific constructs (extend blocks, service methods, oneof fields)
- CRLF line endings — tested `\v` in block comments, both agree; `\r` as column-incrementing whitespace also matches C++
- Custom options with message-typed fields set to scalar values (error message differences)
- Extension range validation for 19000-19999 reserved range
- ~~Proto2 groups nested 3+ levels deep (group in group in group)~~ **Partially covered by Run 10 (group encoding bug)**
- ~~Group-typed custom options — wrong wire type encoding~~ **DONE in Run 10 (341_group_option_encoding)**
- Edition features + extensions interactions
- Proto files importing the same file via different paths
- Custom option scope resolution (Go returns first match, not proper scope-based lookup)
- ~~Sub-field option type validation (Go doesn't check intermediate fields are MESSAGE type)~~ **DONE in Run 9 (340_scalar_subfield_option)**
- Extension extendee type validation (Go doesn't check extendee is MESSAGE)
- Block comment at EOF without trailing newline (similar bug to line comment?)
- Positive sign `+` in aggregate values for angle-bracket syntax `< count: +42 >` — same bug as Run 11 in `consumeAggregateAngle`
- Aggregate option with `+inf` or `+nan` — how does Go handle these?
- ~~Repeated custom options corrupted when sub-field options present~~ **DONE in Run 12 (343_repeated_with_subfield)**
- Same merge bug but with repeated MESSAGE typed options (repeated Config vs sub-field) — would produce even more corrupted output
- Same merge bug with repeated bytes options — bytes payloads would be concatenated