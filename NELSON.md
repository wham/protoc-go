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

### Run 13 — Field-level repeated options corrupted by mergeFieldOptionsInMessages (VICTORY)
- **Bug**: Ralph fixed the file-level merge corruption (Run 12) by adding `mergeableFileFields` parameter to `mergeUnknownExtensions`. But `mergeFieldOptionsInMessages` (line 3445-3458) still passes `nil` for `mergeableFields`, meaning ALL BytesType entries in FieldOptions are merged — including repeated string/bytes options that should stay separate.
- **Test**: `344_field_repeated_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `mergeFieldOptionsInMessages` at cli.go:3449 calls `mergeUnknownExtensions(field.Options.ProtoReflect(), nil)`. Passing `nil` means `mergeableFields == nil` is true in the condition at line 3522, so ALL BytesType entries are merged. When field_a has sub-field options `[(cfg).name = "primary", (cfg).value = 10]`, `hasSubFieldCustomOpts` returns true, triggering merge for ALL fields in the file — including field_b's repeated `[(field_tags) = "alpha", (field_tags) = "beta"]`.
- **C++ protoc**: Keeps repeated entries separate: 362-byte descriptor.
- **Go protoc-go**: Merges repeated entries: 358-byte descriptor. 4-byte difference (same corruption pattern as Run 12).
- **Fix hint**: Either (1) compute `mergeableFields` for FieldOptions similar to how `subFieldFileOptNums` is computed for FileOptions, and pass it instead of `nil`, or (2) check the extension's label and skip merge for `LABEL_REPEATED` fields, or (3) only merge field numbers that actually have sub-field options on that specific field.
- **Also affects**: Extension field options (line 3453: `mergeUnknownExtensions(ext.Options.ProtoReflect(), nil)`) have the same bug.

### Run 14 — Message sub-field options not merged by cloneWithMergedExtUnknowns (VICTORY)
- **Bug**: Go's `cloneWithMergedExtUnknowns` only merges unknown fields in `FileOptions` and `FieldOptions`. It completely ignores `MessageOptions`, `EnumOptions`, `ServiceOptions`, `MethodOptions`, `OneofOptions`, and `EnumValueOptions`. When a message has two sub-field option assignments (`option (msg_cfg).name = "hello"; option (msg_cfg).value = 42;`), C++ protoc merges them into a single wire entry for field 50001, but Go leaves them as two separate entries.
- **Test**: `345_msg_subfield_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `cloneWithMergedExtUnknowns()` at cli.go:3434 only calls `mergeUnknownExtensions` for `fdCopy.Options` (FileOptions) and delegates to `mergeFieldOptionsInMessages` for FieldOptions. No code merges MessageOptions or any other option type. Additionally, `hasSubFieldCustomOpts()` at cli.go:3416 only checks `CustomFileOptions` and `CustomFieldOptions` — it doesn't check `CustomMessageOptions` or other option types.
- **C++ protoc**: Produces 233-byte descriptor with merged Config entry in MessageOptions.
- **Go protoc-go**: Produces 237-byte descriptor with two separate Config entries — 4 bytes larger (extra tag+length overhead).
- **Fix hint**: (1) Add checks for `CustomMessageOptions`, `CustomEnumOptions`, `CustomServiceOptions`, etc. in `hasSubFieldCustomOpts`. (2) Add merge functions for MessageOptions, EnumOptions, ServiceOptions, MethodOptions, OneofOptions, EnumValueOptions in `cloneWithMergedExtUnknowns` or a new helper. (3) Compute mergeable field sets for each option type (like `subFieldFileOptNums`/`subFieldFieldOptNums`).
- **Also affects**: Same bug exists for EnumOptions, ServiceOptions, MethodOptions, OneofOptions, EnumValueOptions, ExtensionRangeOptions — any option type with sub-field custom options will not be merged.

### Run 15 — Enum sub-field options not merged by cloneWithMergedExtUnknowns (VICTORY)
- **Bug**: Go's `cloneWithMergedExtUnknowns` merges unknown extensions in FileOptions, FieldOptions, and MessageOptions — but completely ignores EnumOptions. When an enum has two sub-field option assignments (`option (enum_cfg).label = "tracker"; option (enum_cfg).priority = 10;`), C++ protoc merges them into a single wire entry for field 50001, but Go leaves them as two separate entries.
- **Test**: `346_enum_subfield_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `cloneWithMergedExtUnknowns()` at cli.go:3440 only merges FileOptions, FieldOptions, and MessageOptions. No code iterates enums to merge EnumOptions. Additionally, `hasSubFieldCustomOpts()` does not check `CustomEnumOptions`, so even if merge code existed, it wouldn't be triggered for files that only have enum sub-field options (it works here because the merge IS triggered — by the fact that sub-field options exist — but the actual enum merge step is missing).
- **C++ protoc**: Produces 322-byte descriptor with merged EnumMeta entry in EnumOptions.
- **Go protoc-go**: Produces 326-byte descriptor with two separate EnumMeta entries — 4 bytes larger (extra tag+length overhead).
- **Fix hint**: (1) Add `hasSubFieldCustomOpts` check for `CustomEnumOptions`. (2) Have `resolveCustomEnumOptions` return a `map[string]map[int32]bool` of sub-field nums (like `resolveCustomMessageOptions` does). (3) Add a `mergeEnumOptionsInMessages` function that recursively walks all enums and calls `mergeUnknownExtensions` on each `EnumOptions`. (4) Call it from `cloneWithMergedExtUnknowns`.
- **Also affects**: Same bug exists for ServiceOptions, MethodOptions, OneofOptions, EnumValueOptions, ExtensionRangeOptions.

### Run 16 — Service sub-field options not merged by cloneWithMergedExtUnknowns (VICTORY)
- **Bug**: Go's `cloneWithMergedExtUnknowns` merges unknown extensions in FileOptions, FieldOptions, MessageOptions, and EnumOptions — but completely ignores ServiceOptions. When a service has two sub-field option assignments (`option (svc_cfg).label = "search"; option (svc_cfg).priority = 5;`), C++ protoc merges them into a single wire entry for field 50001, but Go leaves them as two separate entries.
- **Test**: `347_svc_subfield_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `cloneWithMergedExtUnknowns()` at cli.go:3446 handles FileOptions, FieldOptions, MessageOptions, and EnumOptions but has no code to iterate services and merge ServiceOptions. `hasSubFieldCustomOpts()` also doesn't check `CustomServiceOptions`, but the merge is still triggered because the file has sub-field options that match other checked types (the function returns true for any sub-field opt).
- **C++ protoc**: Produces merged ServiceMeta entry in ServiceOptions (0xd3 size prefix).
- **Go protoc-go**: Produces two separate ServiceMeta entries — 4 bytes larger (0xd7 size prefix, extra tag+length overhead).
- **Fix hint**: (1) Add `CustomServiceOptions` check in `hasSubFieldCustomOpts`. (2) Add `mergeServiceOptions` function that iterates `fd.GetService()` and calls `mergeUnknownExtensions` on each service's Options. (3) Add `mergeableServiceOptFields` parameter to `cloneWithMergedExtUnknowns`. (4) Have `resolveCustomServiceOptions` return sub-field nums map.
- **Also affects**: Same bug exists for MethodOptions, OneofOptions, EnumValueOptions, ExtensionRangeOptions.

### Run 17 — Method sub-field options not merged by cloneWithMergedExtUnknowns (VICTORY)
- **Bug**: Go's `cloneWithMergedExtUnknowns` merges unknown extensions in FileOptions, FieldOptions, MessageOptions, EnumOptions, and ServiceOptions — but completely ignores MethodOptions. When a method has two sub-field option assignments (`option (method_cfg).label = "search"; option (method_cfg).priority = 5;`), C++ protoc merges them into a single wire entry for field 50001, but Go leaves them as two separate entries.
- **Test**: `348_method_subfield_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `cloneWithMergedExtUnknowns()` calls `mergeServiceOptions()` which only merges `ServiceOptions` on each service, but never iterates `svc.GetMethod()` to merge `MethodOptions`. No `mergeMethodOptions` function exists.
- **C++ protoc**: Produces 397-byte descriptor with merged MethodMeta entry in MethodOptions.
- **Go protoc-go**: Produces 401-byte descriptor with two separate MethodMeta entries — 4 bytes larger (extra tag+length overhead).
- **Fix hint**: (1) Add a `mergeMethodOptions` function that iterates `svc.GetMethod()` and calls `mergeUnknownExtensions` on each method's Options. (2) Call it from `mergeServiceOptions` or from `cloneWithMergedExtUnknowns`. (3) Add `mergeableMethodOptFields` parameter. (4) Have `resolveCustomMethodOptions` return sub-field nums map.
- **Also affects**: Same bug exists for OneofOptions, EnumValueOptions, ExtensionRangeOptions.

### Run 18 — Oneof sub-field options not merged by cloneWithMergedExtUnknowns (VICTORY)
- **Bug**: Go's `cloneWithMergedExtUnknowns` merges unknown extensions in FileOptions, FieldOptions, MessageOptions, EnumOptions, ServiceOptions, and MethodOptions — but completely ignores OneofOptions. When a oneof has two sub-field option assignments (`option (oneof_cfg).label = "primary"; option (oneof_cfg).priority = 7;`), C++ protoc merges them into a single wire entry for field 50001, but Go leaves them as two separate entries.
- **Test**: `349_oneof_subfield_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `cloneWithMergedExtUnknowns()` handles FileOptions, FieldOptions, MessageOptions, EnumOptions, ServiceOptions, and MethodOptions but has no code to iterate oneofs within messages and merge OneofOptions. No `mergeOneofOptions` function exists. `hasSubFieldCustomOpts` also doesn't check `CustomOneofOptions`, but the merge is still triggered because the function checks other option types.
- **C++ protoc**: Produces 358-byte descriptor with merged OneofMeta entry in OneofOptions.
- **Go protoc-go**: Produces 362-byte descriptor with two separate OneofMeta entries — 4 bytes larger (extra tag+length overhead).
- **Fix hint**: (1) Add `CustomOneofOptions` check in `hasSubFieldCustomOpts`. (2) Add a `mergeOneofOptionsInMessages` function that recursively iterates messages, then for each message iterates `msg.GetOneofDecl()` and calls `mergeUnknownExtensions` on each oneof's Options. (3) Have `resolveCustomOneofOptions` return sub-field nums map. (4) Add `mergeableOneofOptFields` parameter to `cloneWithMergedExtUnknowns`.
- **Also affects**: Same bug exists for EnumValueOptions and ExtensionRangeOptions.

### Run 19 — EnumValue sub-field options not merged by cloneWithMergedExtUnknowns (VICTORY)
- **Bug**: Go's `cloneWithMergedExtUnknowns` merges unknown extensions in FileOptions, FieldOptions, MessageOptions, EnumOptions, ServiceOptions, MethodOptions, and OneofOptions — but completely ignores EnumValueOptions. When an enum value has two sub-field option assignments (`[(val_cfg).label = "low priority", (val_cfg).weight = 1]`), C++ protoc merges them into a single wire entry for field 50001, but Go leaves them as two separate entries.
- **Test**: `350_enumval_subfield_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `cloneWithMergedExtUnknowns()` at cli.go:3455 handles FileOptions, FieldOptions, MessageOptions, EnumOptions, ServiceOptions, MethodOptions, and OneofOptions but has no code to iterate enum values and merge EnumValueOptions. No `mergeEnumValueOptions` function exists. `hasSubFieldCustomOpts()` also doesn't check `CustomEnumValueOptions`.
- **C++ protoc**: Produces 348-byte descriptor with merged ValueMeta entries in EnumValueOptions.
- **Go protoc-go**: Produces 356-byte descriptor with two separate ValueMeta entries per enum value — 8 bytes larger (4 bytes × 2 enum values, extra tag+length overhead).
- **Fix hint**: (1) Add `CustomEnumValueOptions` check in `hasSubFieldCustomOpts`. (2) Add a `mergeEnumValueOptionsInEnums` function that iterates all enums (top-level + nested in messages), then for each enum iterates `enum.GetValue()` and calls `mergeUnknownExtensions` on each value's Options. (3) Have `resolveCustomEnumValueOptions` return sub-field nums map. (4) Add `mergeableEnumValOptFields` parameter to `cloneWithMergedExtUnknowns`.
- **Also affects**: Same bug exists for ExtensionRangeOptions.

### Run 20 — ExtensionRange sub-field options fail to parse (VICTORY)
- **Bug**: Go's parser cannot parse sub-field path syntax in extension range custom options. `extensions 100 to 199 [(range_cfg).label = "primary", (range_cfg).priority = 5]` fails with `Expected "="` at the `.label` sub-field access. The parser doesn't support `(opt).subfield = value` syntax for extension range options — only `(opt) = value` (flat value, no sub-field path).
- **Test**: `351_extrange_subfield_merge` — all 9 profiles fail.
- **Root cause**: Extension range option parsing (parser.go around line 1138-1153) handles `(opt_name) = value` syntax but doesn't parse the `.subfield` sub-field path that follows the option name. When it encounters `.label` after `(range_cfg)`, it expects `=` but finds `.`. This is a parser-level bug, not just a merge issue.
- **C++ protoc**: Accepts sub-field extension range options fine, produces valid descriptor with merged RangeMeta entry.
- **Go protoc-go**: Fails with `test.proto:26:37: Expected "=".` — can't even parse the sub-field syntax.
- **Fix hint**: In the extension range option parser, after consuming the parenthesized option name `(range_cfg)`, add sub-field path parsing (consume `.identifier` segments and build `SubFieldPath` array) before expecting `=`. Same pattern used by file-level, field-level, message-level, etc. option parsers.
- **Also affects**: `hasSubFieldCustomOpts` doesn't check `CustomExtRangeOptions`, and `cloneWithMergedExtUnknowns` has no merge code for ExtensionRangeOptions. Even after the parser is fixed, the merge step will be missing (same pattern as Runs 14-19).

### Run 21 — Custom option unknown fields not sorted by field number (VICTORY)
- **Bug**: Go's custom option resolution appends encoded unknown fields (extension options) in proto file declaration order, but C++ protoc sorts them by field number. When options are declared in non-ascending field number order (e.g., field 50003 first, then 50001, then 50002), the wire format bytes differ.
- **Test**: `352_option_order` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: In `resolveCustomFileOptions()` (and all other `resolveCustom*Options` functions), options are processed in declaration order via `for _, opt := range result.CustomFileOptions`. Each option's encoded bytes are appended to unknown fields via `fd.Options.ProtoReflect().SetUnknown(append(fd.Options.ProtoReflect().GetUnknown(), rawBytes...))`. This preserves declaration order. C++ protoc sorts unknown fields by field number during serialization.
- **C++ protoc**: Encodes unknown fields as field 50001 (varint 42), field 50002 (varint 1), field 50003 (string "hello") — ascending order.
- **Go protoc-go**: Encodes as field 50003 (string "hello"), field 50001 (varint 42), field 50002 (varint 1) — declaration order.
- **Fix hint**: After all custom options are resolved for a given options proto, sort the unknown fields by field number. Can parse the raw unknown bytes, group by tag number, sort by tag number, and reassemble. Or sort the options list by resolved field number before encoding. This affects ALL option types (FileOptions, FieldOptions, MessageOptions, EnumOptions, ServiceOptions, MethodOptions, OneofOptions, EnumValueOptions, ExtensionRangeOptions).

### Run 22 — Top-level extension field sub-field options not merged (VICTORY)
- **Bug**: Go's `cloneWithMergedExtUnknowns` calls `mergeFieldOptionsInMessages(fdCopy.GetMessageType(), ...)` which only processes fields and extensions **inside messages**. It never processes top-level extension declarations (`fdCopy.GetExtension()`). When a top-level extension field has sub-field custom options (e.g., `[(ext_cfg).label = "marker", (ext_cfg).priority = 10]`), the two sub-field entries are not merged into a single wire entry.
- **Test**: `353_toplevel_ext_field_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `cloneWithMergedExtUnknowns()` at cli.go:3480 calls `mergeFieldOptionsInMessages(fdCopy.GetMessageType(), mergeableFieldOptFields)`. This function iterates `msg.GetField()` and `msg.GetExtension()` for each message, but `fdCopy.GetExtension()` (top-level file-scope extensions) is never iterated. The sorting code (`sortFDOptionsUnknownFields` at line 6434) does correctly handle top-level extensions, so sorting is fine — only merging is missing.
- **C++ protoc**: Produces 315-byte descriptor with merged ExtFieldCfg entry in the extension field's FieldOptions.
- **Go protoc-go**: Produces 319-byte descriptor with two separate ExtFieldCfg entries — 4 bytes larger (extra tag+length overhead).
- **Fix hint**: Add a loop in `cloneWithMergedExtUnknowns` after line 3480 that directly iterates `fdCopy.GetExtension()` and calls `mergeUnknownExtensions(ext.Options.ProtoReflect(), mergeableFieldOptFields)` for each top-level extension field. Example: `for _, ext := range fdCopy.GetExtension() { if ext.Options != nil { mergeUnknownExtensions(ext.Options.ProtoReflect(), mergeableFieldOptFields) } }`.

### Run 23 — Nested extension bare name scope resolution accepts invalid option (VICTORY)
- **Bug**: Go's `findFileOptionExtension()` has a "bare name match" fallback (step 3) that finds extensions by simple name alone, ignoring the containing message scope. When an extension is defined inside a message (`message Container { extend google.protobuf.FileOptions { ... } }`), C++ protoc requires the qualified name `(Container.nested_opt)` but Go accepts the bare `(nested_opt)`.
- **Test**: `354_nested_ext_scope` — all 9 profiles fail.
- **Root cause**: `findFileOptionExtension()` at cli.go:4355 tries bare name match: `if e.field.GetName() == name { return e.field, ... }`. For extensions nested in messages, `e.pkg` is set to `"pkg.Container"` (the message FQN), so step 2 (current package scope) correctly fails (`e.pkg != currentPkg`). But step 3 ignores `e.pkg` entirely and matches by field name alone.
- **C++ protoc**: `test.proto:15:8: Option "(nested_opt)" unknown.` — requires `(Container.nested_opt)`.
- **Go protoc-go**: Accepts `(nested_opt)` and produces a valid descriptor with the option encoded.
- **Fix hint**: Remove or restrict the bare name match (step 3) in `findFileOptionExtension`. It should only match when `e.pkg == ""` (top-level, no package). Or better: implement proper scope-based resolution that walks up from the current scope. The same bug likely affects `findFieldOptionExtension`, `findMessageOptionExtension`, etc.

### Run 24 — Trailing empty statement shifts file-level SCI span end column (VICTORY)
- **Bug**: Go's source code info for the file-level span (path `[]`) reports the wrong end column when the file ends with a trailing empty statement (`;` after a closing brace `}`). C++ protoc includes the trailing `;` in the file span, but Go does not.
- **Test**: `355_trailing_empty_stmt` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: When a top-level `};` ends the file, the `;` is an empty statement. C++ protoc's file-level span end column is 2 (past the `;`), but Go's is 1 (at the `}`). The Go parser doesn't advance the file-level end position when consuming trailing empty statements (extra semicolons at top level).
- **C++ protoc**: File span `[0, 0, 6, 2]` — end column includes the trailing `;`.
- **Go protoc-go**: File span `[0, 0, 6, 1]` — end column stops at `}`, ignoring the trailing `;`.
- **Fix hint**: In the parser's top-level loop, when consuming a `;` (empty statement), update the file-level SCI end position to include it. The `trackEnd()` call is likely missing for the `;` token of empty statements.

### Run 25 — Go accepts `infinity` as float/double default value, C++ rejects it (VICTORY)
- **Bug**: Go's default value normalization for float/double fields uses `strings.ToLower(defVal)` and then checks for `infinity`/`-infinity` (normalizing to `inf`/`-inf`). C++ protoc only recognizes the exact lowercase tokens `nan`, `inf`, and `-inf` as special float identifiers — it does NOT accept `NaN`, `Inf`, `INF`, `infinity`, `Infinity`, or `-infinity`.
- **Test**: `356_infinity_default` — all 9 profiles fail.
- **Root cause**: `parseFieldOptions` in parser.go:5594-5603 does `lower := strings.ToLower(defVal)` and checks `lower == "infinity"`, normalizing to `inf`. C++ protoc's tokenizer only recognizes `nan` and `inf` (lowercase exact match) as special float values — anything else (`NaN`, `Inf`, `INF`, `infinity`, `Infinity`) is treated as an unknown identifier and rejected with "Expected number."
- **C++ protoc**: `test.proto:4:37: Expected number.` — rejects `infinity` as a default value.
- **Go protoc-go**: Accepts `infinity`, normalizes to `inf`, produces valid descriptor.
- **Fix hint**: Remove the case-insensitive handling in the default value normalization. Only accept exact lowercase `nan`, `inf`, and `-inf` (matching C++ protoc behavior). Remove the `strings.ToLower` call and the `infinity`/`-infinity` normalization. The check should be: `if defVal == "inf" || defVal == "-inf" || defVal == "nan"` — nothing else.
- **Also affected**: `NaN`, `Inf`, `INF`, `Infinity`, `-infinity` are all accepted by Go but rejected by C++. These are all the same bug (case-insensitive + full-word matching).

### Run 26 — Float overflow default value produces `+Inf` instead of `inf` (VICTORY)
- **Bug**: Go's `simpleFtoa` produces `"+Inf"` for positive infinity, while C++ `SimpleFtoa` produces `"inf"`. When a float32 default value overflows (e.g., `3.5e38` exceeds `FLT_MAX` ~3.4028235e38), the value becomes `+Inf` in float32. Go's `strconv.FormatFloat(+Inf, 'g', 6, 64)` returns `"+Inf"` (uppercase, with `+` sign), but C++ `snprintf(buf, "%.6g", INFINITY)` returns `"inf"` (lowercase, no sign).
- **Test**: `357_float_overflow_default` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `simpleFtoa(float32(3.5e38))` calls `strconv.FormatFloat(math.Inf(1), 'g', 6, 64)` which returns `"+Inf"`. The existing normalization guard (`if lower == "inf" || lower == "-inf"`) only catches literal `inf`/`-inf` text — it doesn't catch numeric values that overflow float32 to infinity. The `simpleFtoa` function doesn't have special handling for infinity/NaN results.
- **C++ protoc**: 60-byte descriptor with `default_value: "inf"` (3 bytes).
- **Go protoc-go**: 61-byte descriptor with `default_value: "+Inf"` (4 bytes). 1-byte difference.
- **Fix hint**: In `simpleFtoa`, check if the result is infinity or NaN before returning: `if math.IsInf(float64(v), 1) { return "inf" }; if math.IsInf(float64(v), -1) { return "-inf" }; if math.IsNaN(float64(v)) { return "nan" }`. Same fix needed in `simpleDtoa` for double overflow (e.g., `1e309` for double). Alternatively, post-process the FormatFloat result to normalize `"+Inf"` → `"inf"`, `"-Inf"` → `"-inf"`, `"NaN"` → `"nan"`.
- **Also affected**: `simpleDtoa` has the same issue — `FormatFloat(+Inf, 'g', 15, 64)` returns `"+Inf"` not `"inf"`. Double overflow values (e.g., `1e309`) would produce the same discrepancy, but `ParseFloat("1e309", 64)` returns `err = ErrRange` so the normalization branch is skipped entirely (stores raw `"1e309"` instead of `"inf"`).

### Run 27 — Double overflow default value stores raw string instead of "inf" (VICTORY)
- **Bug**: Go's default value normalization for `double` fields skips normalization when `strconv.ParseFloat` returns `ErrRange`. For `1e309` (exceeds `DBL_MAX` ~1.7976931348623158e308), `ParseFloat("1e309", 64)` returns `(+Inf, ErrRange)`. Since `err != nil`, the `else if` branch at parser.go:5610 is skipped, and the raw string `"1e309"` is stored as-is. C++ protoc's `strtod("1e309")` returns `HUGE_VAL` (Inf) without failing, and `SimpleDtoa(Inf)` returns `"inf"`.
- **Test**: `358_double_overflow_default` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: parser.go:5610 `else if v, err := strconv.ParseFloat(defVal, 64); err == nil` — when value overflows double, Go returns `err = ErrRange` so the normalization branch is skipped. The special-case checks at line 5605 only catch literal strings `"inf"`, `"-inf"`, `"nan"`, `"-nan"` — not numeric literals that overflow to infinity.
- **C++ protoc**: 61-byte descriptor with `default_value: "inf"` (3 bytes).
- **Go protoc-go**: 63-byte descriptor with `default_value: "1e309"` (5 bytes). 2-byte difference.
- **Fix hint**: When `ParseFloat` returns `ErrRange`, the returned value `v` is still `+Inf` or `-Inf`. Change the condition to: `v, err := strconv.ParseFloat(defVal, 64); if err == nil || (err != nil && (math.IsInf(v, 0)))`. Or: check `math.IsInf(v, 0)` even when `err != nil` and call `simpleDtoa(v)` (which already handles Inf). Same issue exists for float32 overflow beyond `FLT_MAX` with `ParseFloat(defVal, 32)` though that code path uses `ParseFloat(defVal, 64)` + `float32()` cast so it may work differently.
- **Also affects**: Negative overflow: `default = -1e309` would store `"-1e309"` instead of `"-inf"`.

### Run 28 — Custom bool option accepts "True" (capital T), C++ rejects it (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` for `TYPE_BOOL` accepts case-variant bool values like `"True"`, `"False"`, `"t"`, `"f"` in addition to `"true"`, `"false"`, `"0"`, `"1"`. C++ protoc's option resolver only accepts exact lowercase `"true"` and `"false"` (and integer `0`/`1`). When a custom bool option uses `True` (capital T), Go accepts it and produces a valid descriptor, but C++ rejects it with an error.
- **Test**: `359_bool_option_case` — all 9 profiles fail.
- **Root cause**: `encodeCustomOptionValue()` at cli.go:5813-5818 has `case "true", "True", "t", "1":` and `case "false", "False", "f", "0":`. C++ protoc's `OptionInterpreter::SetOption` only accepts `identifier_value == "true"` or `identifier_value == "false"` (exact match, case-sensitive). `"True"` doesn't match either.
- **C++ protoc**: `test.proto:13:20: Value must be "true" or "false" for boolean option "boolcase.my_flag".`
- **Go protoc-go**: Accepts `True` and encodes as varint 1 (true). Produces valid descriptor.
- **Fix hint**: Change the bool case in `encodeCustomOptionValue` to only accept exact `"true"` and `"false"` (and `"0"`, `"1"` for integer literals). Remove `"True"`, `"False"`, `"t"`, `"f"` from the switch cases. Match C++ behavior exactly.
- **Also affects**: Same bug for `"False"` (capital F), `"t"`, `"f"` — all accepted by Go, rejected by C++. This applies to ALL custom option types (file, message, field, enum, enum value, service, method, oneof, extension range) since they all use `encodeCustomOptionValue`.
- **Also affects**: Aggregate option bool fields — `encodeAggregateFields` also uses the same switch for bool values.

### Run 29 — Enum shadow in compound type name resolution bypassed by Go (VICTORY)
- **Bug**: Go's `resolveTypeName` skips enum matches when resolving compound type names (like `Direction.Sub`) and continues searching outer scopes. C++ protoc stops at the first match of the first part — even if it's an enum — and reports a shadow error because the full compound doesn't exist within the enum.
- **Test**: `360_enum_shadow_scope` — all 9 profiles fail.
- **Root cause**: `resolveTypeName()` at parser.go:6675 has a comment "Non-aggregate (enum): skip, continue searching outer scopes". When the first part of a compound name (e.g., `Direction` in `Direction.Sub`) matches an enum in the current scope, Go skips it and continues to outer scopes. C++ treats ANY match of the first part as a shadow — it tries the full compound in that scope, fails, and reports: `"Direction.Sub" is resolved to "scopetest.Outer.Container.Direction.Sub", which is not defined. The innermost scope is searched first in name resolution.`
- **C++ protoc**: `test.proto:23:14: "Direction.Sub" is resolved to "scopetest.Outer.Container.Direction.Sub", which is not defined. The innermost scope is searched first in name resolution. Consider using a leading '.'(i.e., ".Direction.Sub") to start from the outermost scope.`
- **Go protoc-go**: Silently accepts the reference, resolves to `.scopetest.Outer.Direction.Sub`, and produces a valid descriptor.
- **Fix hint**: In `resolveTypeName`, when the first part of a compound name matches an enum type, treat it the same as a message match: try the full compound in the current scope, fail (since enums don't have nested types), and return a shadow error. Replace the "skip, continue" logic at line 6675 with: `return "." + name, firstCandidate` (shadow error with the enum's full path).
- **Also affects**: Any compound type reference where the first part matches an enum in an inner scope. This is a scope resolution correctness bug, not just an edge case.

### Run 30 — Fully-qualified option name with leading dot rejected by Go (VICTORY)
- **Bug**: Go's `parseParenthesizedOptionName()` doesn't handle a leading `.` in custom option extension names. When an option is declared as `option (.pkg.my_opt) = "value";`, C++ protoc accepts the fully-qualified name (the leading dot forces absolute scope lookup). Go's parser reads `.` as the first `innerTok`, then the loop looks for another `.` separator, but the next token is `pkg` (an identifier), so the loop exits and `Expect(")")` fails because the next token is `pkg`, not `)`.
- **Test**: `361_fqn_option_name` — all 9 profiles fail.
- **Root cause**: `parseParenthesizedOptionName()` at parser.go:4902-4920 reads the first token and then loops on `.` separators. When the first token IS `.`, the parser stores `fullName = "(."` and then expects either another `.` or `)`. It doesn't handle the case where `.` is a leading qualifier (meaning the next tokens are `ident(.ident)*`).
- **C++ protoc**: Accepts `(.fqnopt.my_label)` — produces valid descriptor with the option value.
- **Go protoc-go**: `test.proto:14:10: Expected ")".` — rejects the syntax.
- **Fix hint**: In `parseParenthesizedOptionName`, after reading `innerTok`, check if `innerTok.Value == "."`. If so, read the next identifier token and prepend `.` to it: `fullName = "(." + nextTok.Value`. Then continue the loop for `.ident` pairs as normal. This mirrors C++ protoc's handling of fully-qualified extension names.
- **Also affects**: This bug affects ALL option entity types: file options, message options, field options, enum options, enum value options, service options, method options, oneof options, extension range options — all call `parseParenthesizedOptionName`.

### Run 31 — Custom float option accepts `Inf` (capital I), C++ rejects it (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` for `TYPE_FLOAT` calls `strconv.ParseFloat(value, 32)` which accepts case-insensitive `Inf`, `INF`, `NaN`, etc. C++ protoc only accepts exact lowercase identifiers `inf` and `nan` for float/double option values. When a custom float option uses `Inf` (capital I), Go accepts it and produces a valid descriptor with infinity, but C++ rejects it with "Value must be number for float option".
- **Test**: `362_float_option_case` — all 9 profiles fail.
- **Root cause**: `encodeCustomOptionValue()` at cli.go:5847 does `switch strings.ToLower(value)` which normalizes `"Inf"` to `"inf"`. This doesn't match `"nan"` or `"-nan"`, so it falls to the `default` case which calls `strconv.ParseFloat("Inf", 32)`. Go's `strconv.ParseFloat` is case-insensitive for special values (`Inf`, `+Inf`, `-Inf`, `NaN`), so it returns `+Inf` successfully. C++ protoc's option interpreter only recognizes exact lowercase `"inf"` and `"nan"` identifiers; `"Inf"` is treated as an unknown identifier and rejected.
- **C++ protoc**: `test.proto:13:21: Value must be number for float option "floatcase.my_float".`
- **Go protoc-go**: Accepts `Inf`, encodes as float32 infinity `0x7F800000`, produces valid descriptor.
- **Fix hint**: In `encodeCustomOptionValue` for TYPE_FLOAT and TYPE_DOUBLE, after the NaN switch, don't rely on `strconv.ParseFloat` for special values. Instead, check for exact lowercase `"inf"` and `"-inf"` before calling `ParseFloat`, and reject anything else that `ParseFloat` would interpret as infinity/NaN. Or: after `ParseFloat`, check if the result is Inf/NaN and reject it (since those should only be accepted via the explicit identifier checks, not via `ParseFloat` interpretation of case variants).
- **Also affects**: TYPE_DOUBLE has the same issue (line 5861-5873). Aggregate option fields (`encodeAggregateFields`) likely have the same bug. Values like `NaN`, `INF`, `-Inf`, `+Inf`, `+inf` would all behave differently between Go and C++.

### Run 32 — int32 custom option accepts hex value above INT32_MAX (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` for `TYPE_INT32` (and `TYPE_INT64`, `TYPE_SINT32`, `TYPE_SINT64`) uses `strconv.ParseInt(value, 0, 64)` — note the 64-bit width. This means any hex value that fits in int64 is accepted, regardless of whether it fits in int32. For `0x80000000 = 2147483648` (which exceeds `INT32_MAX = 2147483647`), Go accepts it and encodes as varint. C++ protoc validates the range and rejects it.
- **Test**: `363_int32_hex_overflow` — all 9 profiles fail (C++ fails, Go succeeds → one-accepts-one-rejects).
- **Root cause**: `encodeCustomOptionValue()` at cli.go:5854 does `strconv.ParseInt(value, 0, 64)`. For int32, Go should use bit width 32 or validate the result is in int32 range `[-2147483648, 2147483647]`. Currently, Go only catches values outside int64 range, not int32 range.
- **C++ protoc**: `test.proto:14:22: Value out of range, -2147483648 to 2147483647, for int32 option "hexoverflow.small_val".`
- **Go protoc-go**: Accepts `0x80000000`, encodes as varint `2147483648`, produces valid descriptor.
- **Fix hint**: For `TYPE_INT32` and `TYPE_SINT32`, after `ParseInt`, check `if v > math.MaxInt32 || v < math.MinInt32` and return a range error. Similarly, for `TYPE_UINT32`, check `v > math.MaxUint32`. The error message should match C++ format: `"Value out of range, MIN to MAX, for TYPE option \"NAME\"."` with proper line:column info.
- **Also affects**: Same bug exists for TYPE_SINT32 (same ParseInt call). TYPE_INT64 has the separate issue that `ParseInt(value, 0, 64)` can't handle values > INT64_MAX (like `0xFFFFFFFFFFFFFFFF`) that C++ rejects with a range error too — Go gives a generic parse error instead of a range error with proper format. TYPE_UINT32 similarly lacks range validation. For aggregate option encoding (`encodeAggregateFields`), the same function is called, so the same bug applies there too.

### Run 33 — Form feed character not treated as whitespace (VICTORY)
- **Bug**: Go tokenizer does NOT treat form feed (`\f`, 0x0C) or vertical tab (`\v`, 0x0B) as whitespace. C++ protoc does. When `\f` appears between tokens in a .proto file, C++ silently skips it as whitespace, but Go emits it as a `TokenSymbol`, causing parse errors.
- **Test**: `364_formfeed_whitespace` — all 9 profiles fail.
- **Root cause**: `collectComments()` in `io/tokenizer/tokenizer.go` at lines 120 and 160 only checks for `' '`, `'\t'`, `'\r'` as whitespace characters. The `\f` (form feed) and `\v` (vertical tab) characters are missing. Ironically, `readBlockCommentText()` at line 262 DOES handle `\v` and `\f` — the inconsistency is within the same file.
- **C++ protoc**: Accepts `\f` between tokens as whitespace, produces valid descriptor (exit 0).
- **Go protoc-go**: Fails with `Expected top-level statement (e.g. "message").` because `\f` is tokenized as a symbol, not skipped.
- **Fix hint**: In `collectComments()`, add `'\f'` and `'\v'` to the whitespace checks at lines 120 and 160. E.g., change `currentChar == ' ' || currentChar == '\t' || currentChar == '\r'` to include `|| currentChar == '\f' || currentChar == '\v'`.
- **Also affects**: `\v` (vertical tab, 0x0B) has the same bug — also not treated as whitespace.

### Run 34 — Message-level int32 custom option missing range validation (VICTORY)
- **Bug**: Go's `resolveCustomMessageOptions` (and all other `resolveCustom*Options` except `resolveCustomFileOptions`) does NOT call `checkIntRangeOption` to validate that int32/uint32 values fit in 32-bit range. When a message-level int32 custom option is set to `0x80000000` (= 2147483648, exceeds INT32_MAX = 2147483647), Go accepts it and produces a descriptor, while C++ rejects it with a range error.
- **Test**: `365_msg_int32_overflow` — all 9 profiles fail.
- **Root cause**: `checkIntRangeOption` is only called at cli.go:4179 inside `resolveCustomFileOptions`. The other functions — `resolveCustomFieldOptions`, `resolveCustomMessageOptions`, `resolveCustomServiceOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`, `resolveCustomExtRangeOptions` — all skip range validation for 32-bit types.
- **C++ protoc**: `test.proto:15:22: Value out of range, -2147483648 to 2147483647, for int32 option "msgoverflow.msg_val".`
- **Go protoc-go**: Silently accepts `0x80000000`, encodes as varint 2147483648, produces valid descriptor.
- **Fix hint**: Add `checkIntRangeOption` calls in each `resolveCustom*Options` function, guarded by `opt.AggregateFields == nil && len(opt.SubFieldPath) == 0` (same guard as in file options). Copy the pattern from cli.go:4177-4184 to each of the 8 other functions.
- **Also affects**: Same bug for uint32 overflow (e.g., `0x100000000` for uint32), sint32 overflow, fixed32 overflow (via ParseUint), sfixed32 overflow (via ParseInt). All 32-bit types on all non-file option entity types.

### Run 35 — int32 overflow inside aggregate option value not caught (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` for `TYPE_INT32` uses `strconv.ParseInt(value, 0, 64)` with 64-bit width and no range validation. For aggregate option values (message literal fields), `checkIntRangeOption` is never called — it's only called for simple (non-aggregate) options in the `resolveCustom*Options` functions. When an aggregate option has an int32 field with value `0x80000000` (2147483648, exceeds INT32_MAX), Go accepts it silently.
- **Test**: `366_aggregate_int32_overflow` — all 9 profiles fail.
- **Root cause**: `encodeAggregateFields()` at cli.go:6260 calls `encodeCustomOptionValue()` directly without any int32 range validation. `encodeCustomOptionValue()` at cli.go:5935 uses `ParseInt(value, 0, 64)` which accepts any value that fits in int64. The `checkIntRangeOption()` function exists but is only called in the `resolveCustom*Options` functions for simple options — the aggregate encoding path bypasses it entirely.
- **C++ protoc**: `test.proto:15:16: Error while parsing option value for "cfg": Integer out of range (0x80000000)` — the text format parser validates int32 ranges.
- **Go protoc-go**: Accepts `0x80000000` as a valid int32 value, encodes it as varint 2147483648, produces a descriptor.
- **Fix hint**: Either (1) add range validation inside `encodeCustomOptionValue()` for TYPE_INT32, TYPE_SINT32, TYPE_UINT32, TYPE_SFIXED32, TYPE_FIXED32 (check result fits in 32-bit range after `ParseInt`/`ParseUint`), or (2) call `checkIntRangeOption()` from `encodeAggregateFields()` before calling `encodeCustomOptionValue()`. Option 1 is cleaner since it catches ALL callers.
- **Also affects**: Same bug for TYPE_UINT32 (e.g., `0x100000000` in aggregate), TYPE_SINT32, TYPE_SFIXED32, TYPE_FIXED32 — all 32-bit integer types lack range validation in the aggregate encoding path. Negative values in aggregate int32 fields (e.g., `-2147483649`) would also overflow.

### Run 36 — Duplicate non-repeated message-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomMessageOptions` (and all other `resolveCustom*Options` except `resolveCustomFileOptions`) does NOT check for duplicate non-repeated custom options. When a message has `option (msg_tag) = 42; option (msg_tag) = 99;` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `367_msg_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `resolveCustomFileOptions` at cli.go:4129-4147 has `seenCustomOpts` map that tracks which non-repeated, non-aggregate, non-subfield options have been set and emits "Option X was already set." for duplicates. None of the other resolvers (`resolveCustomFieldOptions`, `resolveCustomMessageOptions`, `resolveCustomEnumOptions`, `resolveCustomServiceOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`, `resolveCustomExtRangeOptions`) have this check.
- **C++ protoc**: `test.proto:13:10: Option "(msg_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in MessageOptions unknown fields.
- **Fix hint**: Add the `seenCustomOpts` duplicate detection logic to each of the 8 other `resolveCustom*Options` functions. The pattern is: `seenCustomOpts := map[string]bool{}`, skip repeated/aggregate/subfield options, check `seenCustomOpts[extFQN]`, emit error if already set. Note: the `seenCustomOpts` scope should be per-entity (per message, per field, etc.), not per file.
- **Also affects**: Same bug for field-level, enum-level, service-level, method-level, oneof-level, enum-value-level, and ext-range-level custom options. All non-file resolvers are missing the duplicate check.

### Run 37 — Message-level bool option accepts `True` (case mismatch) (VICTORY)
- **Bug**: Go's `resolveCustomMessageOptions` is missing bool validation. When a message has `option (msg_flag) = True;` (capital T), C++ protoc rejects it with `Value must be "true" or "false"`, but Go accepts it and encodes it as a valid bool option.
- **Test**: `368_msg_bool_option_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Bool validation exists only in 3 of the 9 `resolveCustom*Options` functions: `resolveCustomFileOptions` (line 4151), `resolveCustomFieldOptions` (line 4462), and `resolveCustomExtRangeOptions` (line 5767). The other 6 resolvers — `resolveCustomMessageOptions`, `resolveCustomServiceOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions` — are ALL missing the `TYPE_BOOL` check that validates `value == "true" || value == "false"`.
- **C++ protoc**: `test.proto:15:23: Value must be "true" or "false" for boolean option "msgboolcase.msg_flag".`
- **Go protoc-go**: Silently accepts `True`, encodes it as a bool value via `encodeCustomOptionValue` which accepts `True`/`False`/`t`/`f` at line 5986.
- **Fix hint**: Add the bool validation block (checking `ext.GetType() == TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0` and requiring `value == "true" || value == "false"`) to all 6 missing resolvers.
- **Also affects**: Same bug for service-level, method-level, enum-level, enum-value-level, and oneof-level bool custom options — all 6 missing resolvers.

### Run 38 — Service-level bool option accepts `True` (case mismatch) (VICTORY)
- **Bug**: Go's `resolveCustomServiceOptions` is missing bool validation. When a service has `option (svc_flag) = True;` (capital T), C++ protoc rejects it with `Value must be "true" or "false"`, but Go accepts it and encodes it as a valid bool option. Ralph fixed message-level bool validation in Run 37 but only added it to `resolveCustomMessageOptions` — the other 5 resolvers (service, method, enum, enum_value, oneof) are still missing it.
- **Test**: `369_svc_bool_option_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Bool validation now exists in 4 of the 9 `resolveCustom*Options` functions: `resolveCustomFileOptions` (line 4151), `resolveCustomFieldOptions` (line 4462), `resolveCustomMessageOptions` (line 4716), and `resolveCustomExtRangeOptions` (line 5794). The other 5 resolvers — `resolveCustomServiceOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions` — are ALL missing the `TYPE_BOOL` check.
- **C++ protoc**: `test.proto:15:23: Value must be "true" or "false" for boolean option "svcboolcase.svc_flag".`
- **Go protoc-go**: Silently accepts `True`, encodes it as a bool value via `encodeCustomOptionValue` which accepts `True`/`False`/`t`/`f`.
- **Fix hint**: Add the bool validation block to all 5 remaining resolvers. Same pattern as line 4716 in `resolveCustomMessageOptions`.
- **Also affects**: Same bug for method-level, enum-level, enum-value-level, and oneof-level bool custom options.

### Run 39 — Method-level bool option accepts `True` (case mismatch) (VICTORY)
- **Bug**: Go's `resolveCustomMethodOptions` is missing bool validation. When a method has `option (mtd_flag) = True;` (capital T), C++ protoc rejects it with `Value must be "true" or "false"`, but Go accepts it and encodes it as a valid bool option. Ralph fixed service-level bool validation in Run 38 but only added it to `resolveCustomServiceOptions` — the other 3 resolvers (method, enum, enum_value, oneof) are still missing it.
- **Test**: `370_method_bool_option_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Bool validation now exists in 5 of the 9 `resolveCustom*Options` functions: `resolveCustomFileOptions` (line 4153), `resolveCustomFieldOptions` (line 4464), `resolveCustomMessageOptions` (line 4718), `resolveCustomServiceOptions` (line 4924), and `resolveCustomExtRangeOptions` (line 5823). The other 4 resolvers — `resolveCustomMethodOptions`, `resolveCustomEnumOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions` — are ALL missing the `TYPE_BOOL` check.
- **C++ protoc**: `test.proto:16:25: Value must be "true" or "false" for boolean option "mtdboolcase.mtd_flag".`
- **Go protoc-go**: Silently accepts `True`, encodes it as a bool value via `encodeCustomOptionValue` which accepts `True`/`False`/`t`/`f`.
- **Fix hint**: Add the bool validation block to all 4 remaining resolvers. Same pattern as line 4924 in `resolveCustomServiceOptions`.
- **Also affects**: Same bug for enum-level, enum-value-level, and oneof-level bool custom options.

### Run 40 — Enum-level bool option accepts `True` (case mismatch) (VICTORY)
- **Bug**: Go's `resolveCustomEnumOptions` is missing bool validation. When an enum has `option (enum_flag) = True;` (capital T), C++ protoc rejects it with `Value must be "true" or "false"`, but Go accepts it and encodes it as a valid bool option. Ralph fixed file/field/message/service/method/ext-range bool validation in previous runs but never added it to `resolveCustomEnumOptions`, `resolveCustomEnumValueOptions`, or `resolveCustomOneofOptions`.
- **Test**: `371_enum_bool_option_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Bool validation exists in 6 of 9 `resolveCustom*Options` functions (file at 4151, field at 4462, message at 4716, service at 4922, method at 5126, ext-range at 5848). The other 3 resolvers — `resolveCustomEnumOptions` (5270), `resolveCustomEnumValueOptions` (5447), `resolveCustomOneofOptions` (5624) — are ALL missing the `TYPE_BOOL` check that validates `value == "true" || value == "false"`.
- **C++ protoc**: `test.proto:12:24: Value must be "true" or "false" for boolean option "enumboolcase.enum_flag".`
- **Go protoc-go**: Silently accepts `True`, encodes it as a bool value via `encodeCustomOptionValue` which accepts `True`/`False`/`t`/`f` at line 6067.
- **Fix hint**: Add the bool validation block to all 3 remaining resolvers: `resolveCustomEnumOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`. Same pattern as line 4922 in `resolveCustomServiceOptions`.
- **Also affects**: Same bug for enum-value-level and oneof-level bool custom options — both missing the same check.

### Run 41 — Subnormal float default value string differs (VICTORY)
- **Bug**: Go's `simpleFtoa` produces a different string representation than C++'s `SimpleFtoa` for subnormal float32 values (values smaller than `FLT_MIN` ≈ 1.17549435e-38). The round-trip check (`format → parse → compare`) succeeds in Go but fails in C++, causing C++ to use 9 significant digits while Go uses 6.
- **Test**: `372_subnormal_float_default` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `simpleFtoa(float32(1e-45))` in parser.go:6080 formats to `"1.4013e-45"` (6 sig digits), then `ParseFloat("1.4013e-45", 32)` returns a float32 that equals the original — round-trip succeeds. C++'s `SimpleFtoa` formats the same `"1.4013e-45"`, but `strtof("1.4013e-45")` returns a DIFFERENT float32 — round-trip fails, so it falls through to `"%.9g"` → `"1.40129846e-45"` (9 sig digits).
- **C++ protoc**: `default_value: "1.40129846e-45"` (14 bytes).
- **Go protoc-go**: `default_value: "1.4013e-45"` (10 bytes). 4-byte difference in descriptor.
- **Fix hint**: The discrepancy is in `strtof` (C library) vs `ParseFloat` (Go stdlib) behavior for subnormal float32 values. Go's `ParseFloat` is correctly rounding the string back to the same float32, while C's `strtof` (on macOS) fails to round-trip. To match C++ behavior, Go would need to replicate C's less-precise `strtof` round-trip failure, which is platform-specific. One approach: for subnormal float32 values (abs(v) < FLT_MIN and v != 0), always use 9 significant digits (skip the 6-digit attempt). Or: use `FormatFloat(v64, 'g', 6, 32)` (32-bit precision) instead of `64` to match C's float formatting behavior more closely.
- **Also affects**: Any subnormal float32 default value will trigger this. Examples: `1e-45`, `1.17549e-38`, `5e-40`, etc. The exact set of affected values depends on which ones fail the C `strtof` round-trip.

### Run 42 — Enum-value-level bool option accepts `True` (case mismatch) (VICTORY)
- **Bug**: Go's `resolveCustomEnumValueOptions` is missing bool validation. When an enum value has `[(val_flag) = True]` (capital T), C++ protoc rejects it with `Value must be "true" or "false"`, but Go accepts it and encodes it as a valid bool option. Ralph fixed file/field/message/service/method/enum/ext-range bool validation in previous runs but never added it to `resolveCustomEnumValueOptions` or `resolveCustomOneofOptions`.
- **Test**: `373_enumval_bool_option_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Bool validation exists in 7 of 9 `resolveCustom*Options` functions (file at 4151, field at 4462, message at 4716, service at 4922, method at 5126, enum at 5321, ext-range at 5875). The other 2 resolvers — `resolveCustomEnumValueOptions` (around 5512) and `resolveCustomOneofOptions` — are BOTH missing the `TYPE_BOOL` check that validates `value == "true" || value == "false"`.
- **C++ protoc**: `test.proto:12:23: Value must be "true" or "false" for boolean option "evboolcase.val_flag".`
- **Go protoc-go**: Silently accepts `True`, encodes it as a bool value via `encodeCustomOptionValue` which accepts `True`/`False`/`t`/`f` at line 6091.
- **Fix hint**: Add the bool validation block to both remaining resolvers: `resolveCustomEnumValueOptions` and `resolveCustomOneofOptions`. Same pattern as line 5321 in `resolveCustomEnumOptions`.
- **Also affects**: Same bug for oneof-level bool custom options — `resolveCustomOneofOptions` is missing the same check.

### Run 43 — Oneof-level bool option accepts `True` (case mismatch) (VICTORY)
- **Bug**: Go's `resolveCustomOneofOptions` is the LAST remaining resolver missing bool validation. When a oneof has `option (oneof_flag) = True;` (capital T), C++ protoc rejects it with `Value must be "true" or "false"`, but Go accepts it and encodes it as a valid bool option. Ralph fixed all other resolvers (file, field, message, service, method, enum, enum_value, ext-range) in previous runs but never added it to `resolveCustomOneofOptions`.
- **Test**: `374_oneof_bool_option_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Bool validation now exists in 8 of 9 `resolveCustom*Options` functions (file at 4151, field at 4462, message at 4716, service at 4922, method at 5126, enum at 5321, enum_value at 5525, ext-range at 5902). The ONLY remaining resolver — `resolveCustomOneofOptions` (around line 5730) — is missing the `TYPE_BOOL` check that validates `value == "true" || value == "false"`.
- **C++ protoc**: `test.proto:13:27: Value must be "true" or "false" for boolean option "oneofboolcase.oneof_flag".`
- **Go protoc-go**: Silently accepts `True`, encodes it as a bool value via `encodeCustomOptionValue` which accepts `True`/`False`/`t`/`f`.
- **Fix hint**: Add the bool validation block to `resolveCustomOneofOptions` after the int range check (around line 5735). Same pattern as line 5525 in `resolveCustomEnumValueOptions`: `if ext.GetType() == TYPE_BOOL && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0 { value := opt.Value; if value != "true" && value != "false" { errs = append(...); continue } }`.

### Run 44 — Duplicate non-repeated field-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomFieldOptions` does NOT check for duplicate non-repeated custom options. When a field has `[(field_tag) = 42, (field_tag) = 99]` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `375_field_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `resolveCustomFileOptions` at cli.go:4129-4147 has `seenCustomOpts` map that tracks which non-repeated, non-aggregate, non-subfield options have been set and emits "Option X was already set." for duplicates. `resolveCustomMessageOptions` also has `seenMsgOpts` at cli.go:4672. But the other 7 resolvers (`resolveCustomFieldOptions`, `resolveCustomEnumOptions`, `resolveCustomServiceOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`, `resolveCustomExtRangeOptions`) all lack this check.
- **C++ protoc**: `test.proto:12:47: Option "(field_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in FieldOptions unknown fields.
- **Fix hint**: Add a `seenFieldOpts` map (keyed by field pointer + extension FQN) to `resolveCustomFieldOptions`. For non-repeated, non-aggregate, non-subfield options, check if already set and emit error. Same pattern needed for the other 5 resolvers (enum, service, method, enum_value, oneof, ext_range).
- **Also affects**: Same bug for enum-level, service-level, method-level, enum-value-level, oneof-level, and ext-range-level custom options. All 7 non-file/non-message resolvers are missing the duplicate check.

### Run 45 — Duplicate non-repeated enum-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomEnumOptions` does NOT check for duplicate non-repeated custom options. When an enum has `option (enum_tag) = 42; option (enum_tag) = 99;` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `376_enum_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `resolveCustomFileOptions` at cli.go:4129 has `seenCustomOpts`, `resolveCustomFieldOptions` at cli.go:4450 has `seenFieldOpts`, `resolveCustomMessageOptions` at cli.go:4689 has `seenMsgOpts`. But the other 6 resolvers (`resolveCustomEnumOptions`, `resolveCustomServiceOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`, `resolveCustomExtRangeOptions`) all lack this duplicate detection.
- **C++ protoc**: `test.proto:13:10: Option "(enum_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in EnumOptions unknown fields.
- **Fix hint**: Add a `seenEnumOpts` map (keyed by enum pointer + extension FQN) to `resolveCustomEnumOptions`. For non-repeated, non-aggregate, non-subfield options, check if already set and emit error. Same pattern needed for the other 5 resolvers (service, method, enum_value, oneof, ext_range).
- **Also affects**: Same bug for service-level, method-level, enum-value-level, oneof-level, and ext-range-level custom options. All 6 non-file/non-field/non-message resolvers are missing the duplicate check.

### Run 46 — Duplicate non-repeated service-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomServiceOptions` does NOT check for duplicate non-repeated custom options. When a service has `option (svc_tag) = 42; option (svc_tag) = 99;` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `377_svc_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Duplicate detection (`seenCustomOpts` map) exists in 4 of 9 `resolveCustom*Options` functions: `resolveCustomFileOptions` (line 4129), `resolveCustomFieldOptions` (line 4450), `resolveCustomMessageOptions` (line 4689), `resolveCustomEnumOptions` (line 5322). The other 5 resolvers — `resolveCustomServiceOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`, `resolveCustomExtRangeOptions` — all lack this check.
- **C++ protoc**: `test.proto:16:10: Option "(svc_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in ServiceOptions unknown fields.
- **Fix hint**: Add a `seenSvcOpts` map (keyed by service pointer + extension FQN) to `resolveCustomServiceOptions`. For non-repeated, non-aggregate, non-subfield options, check if already set and emit error. Same pattern needed for the other 4 resolvers (method, enum_value, oneof, ext_range).
- **Also affects**: Same bug for method-level, enum-value-level, oneof-level, and ext-range-level custom options. All 5 non-file/non-field/non-message/non-enum resolvers are missing the duplicate check.

### Run 47 — Duplicate non-repeated method-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomMethodOptions` does NOT check for duplicate non-repeated custom options. When a method has `option (mtd_tag) = 42; option (mtd_tag) = 99;` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `378_method_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Duplicate detection (`seenCustomOpts` map) exists in 5 of 9 `resolveCustom*Options` functions: `resolveCustomFileOptions` (line 4129), `resolveCustomFieldOptions` (line 4450), `resolveCustomMessageOptions` (line 4689), `resolveCustomEnumOptions` (line 5339), `resolveCustomServiceOptions` (line 4914). The other 4 resolvers — `resolveCustomMethodOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`, `resolveCustomExtRangeOptions` — all lack this check.
- **C++ protoc**: `test.proto:17:12: Option "(mtd_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in MethodOptions unknown fields.
- **Fix hint**: Add a `seenMethodOpts` map (keyed by method pointer + extension FQN) to `resolveCustomMethodOptions`. For non-repeated, non-aggregate, non-subfield options, check if already set and emit error. Same pattern needed for the other 3 resolvers (enum_value, oneof, ext_range).
- **Also affects**: Same bug for enum-value-level, oneof-level, and ext-range-level custom options. All 4 non-file/non-field/non-message/non-enum/non-service resolvers are missing the duplicate check.

### Run 48 — Duplicate non-repeated enum-value-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomEnumValueOptions` does NOT check for duplicate non-repeated custom options. When an enum value has `[(val_tag) = 42, (val_tag) = 99]` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `379_enumval_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Duplicate detection (`seenCustomOpts` map) exists in 6 of 9 `resolveCustom*Options` functions: `resolveCustomFileOptions` (line 4129), `resolveCustomFieldOptions` (line 4450), `resolveCustomMessageOptions` (line 4689), `resolveCustomServiceOptions` (line 4914), `resolveCustomEnumOptions` (line 5356), `resolveCustomMethodOptions` (line 5135). The other 3 resolvers — `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`, `resolveCustomExtRangeOptions` — all lack this check.
- **C++ protoc**: `test.proto:13:31: Option "(val_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in EnumValueOptions unknown fields.
- **Fix hint**: Add a `seenEvOpts` map (keyed by enum value pointer + extension FQN) to `resolveCustomEnumValueOptions`. For non-repeated, non-aggregate, non-subfield options, check if already set and emit error. Same pattern needed for the other 2 resolvers (oneof, ext_range).
- **Also affects**: Same bug for oneof-level and ext-range-level custom options. Both resolvers are missing the duplicate check.

### Run 49 — Duplicate non-repeated oneof-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomOneofOptions` does NOT check for duplicate non-repeated custom options. When a oneof has `option (oneof_tag) = 42; option (oneof_tag) = 99;` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `380_oneof_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Duplicate detection (`seenCustomOpts` map) exists in 7 of 9 `resolveCustom*Options` functions: `resolveCustomFileOptions` (line 4129), `resolveCustomFieldOptions` (line 4450), `resolveCustomMessageOptions` (line 4689), `resolveCustomServiceOptions` (line 4914), `resolveCustomEnumOptions` (line 5339), `resolveCustomMethodOptions` (line 5135), `resolveCustomEnumValueOptions` (line 5577). The other 2 resolvers — `resolveCustomOneofOptions` and `resolveCustomExtRangeOptions` — both lack this check.
- **C++ protoc**: `test.proto:14:12: Option "(oneof_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in OneofOptions unknown fields.
- **Fix hint**: Add a `seenOneofOpts` map (keyed by oneof pointer + extension FQN) to `resolveCustomOneofOptions`. For non-repeated, non-aggregate, non-subfield options, check if already set and emit error. Same pattern needed for `resolveCustomExtRangeOptions` — the last remaining resolver missing the duplicate check.
- **Also affects**: Same bug for ext-range-level custom options. `resolveCustomExtRangeOptions` is the only remaining resolver missing the duplicate check.

### Run 50 — Duplicate non-repeated ext-range-level custom option not rejected (VICTORY)
- **Bug**: Go's `resolveCustomExtRangeOptions` is the LAST remaining resolver missing duplicate detection for non-repeated custom options. When an extension range has `[(range_tag) = 42, (range_tag) = 99]` (same non-repeated option set twice), C++ protoc rejects the second one with "Option was already set", but Go silently accepts both and encodes two entries in the unknown fields.
- **Test**: `381_extrange_dup_option` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Duplicate detection (`seenCustomOpts` map) now exists in 8 of 9 `resolveCustom*Options` functions: `resolveCustomFileOptions`, `resolveCustomFieldOptions`, `resolveCustomMessageOptions`, `resolveCustomServiceOptions`, `resolveCustomEnumOptions`, `resolveCustomMethodOptions`, `resolveCustomEnumValueOptions`, `resolveCustomOneofOptions`. The ONLY remaining resolver — `resolveCustomExtRangeOptions` (around line 5984) — is missing the `seenCustomOpts` check.
- **C++ protoc**: `test.proto:12:44: Option "(range_tag)" was already set.`
- **Go protoc-go**: Silently accepts both, encodes two varint entries for field 50001 in ExtensionRangeOptions unknown fields.
- **Fix hint**: Add a `seenExtRangeOpts` map (keyed by extension range pointer or message+range key + extension FQN) to `resolveCustomExtRangeOptions`. For non-repeated, non-aggregate, non-subfield options, check if already set and emit error. Same pattern as all other resolvers. This completes the duplicate detection coverage for ALL 9 option entity types.

### Run 51 — Float/double default value `Inf` (capital I) accepted by Go, rejected by C++ (VICTORY)
- **Bug**: Go's default value normalization for float/double fields uses `strings.ToLower(defVal)` and then compares against `"inf"`, `"-inf"`, `"nan"`, `"-nan"`. This means `Inf`, `INF`, `NaN`, `NAN`, `-Inf`, `-NaN` are all accepted. C++ protoc only accepts exact lowercase `inf`, `-inf`, and `nan` — anything else (like `Inf` with capital I) is rejected with "Expected number."
- **Test**: `382_inf_default_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: parser.go:5618 does `lower := strings.ToLower(defVal)` then checks `lower == "inf"`. For `defVal = "Inf"`, `lower = "inf"` → matches → `defVal = "inf"` → produces valid descriptor. C++ protoc's parser does case-sensitive comparison: `"Inf" != "inf"` → error.
- **C++ protoc**: `test.proto:6:46: Expected number.` — rejects `Inf` as a default value.
- **Go protoc-go**: Accepts `Inf`, normalizes to `"inf"`, produces valid descriptor with `default_value: "inf"`.
- **Fix hint**: Remove `strings.ToLower` call at line 5618. Compare `defVal` directly against exact lowercase strings: `if defVal == "inf" || defVal == "-inf" || defVal == "nan" || defVal == "-nan"`. This matches C++ protoc's case-sensitive handling.
- **Also affected**: `INF`, `NaN`, `NAN`, `-Inf`, `-NaN` — all accepted by Go due to case-insensitive comparison. Run 25 tested `infinity` (full word) which was a different sub-bug (full-word matching). This run tests case sensitivity specifically.

### Run 52 — String custom option accepts integer value, C++ rejects it (VICTORY)
- **Bug**: Go's custom option resolver does not validate that string/bytes option values are quoted strings. When `option (my_label) = 42;` sets a string option to an integer literal, C++ protoc rejects it with `Value must be quoted string for string option`, but Go accepts it and encodes `"42"` as the string value.
- **Test**: `383_string_opt_int_value` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `resolveCustomFileOptions` (and all other `resolveCustom*Options` functions) never checks the `opt.ValueType` for TYPE_STRING or TYPE_BYTES options. When the parser encounters `option (str_opt) = 42;`, it stores `42` as the value with `ValueType = TokenInt`. The encoder (`encodeCustomOptionValue`) for TYPE_STRING simply uses `opt.Value` as-is, treating `"42"` as a valid string. C++ protoc's `OptionInterpreter::SetOption` validates that string option values must be string literals.
- **C++ protoc**: `test.proto:14:21: Value must be quoted string for string option "stroptint.my_label".`
- **Go protoc-go**: Silently accepts `42`, encodes it as string bytes `"42"`, produces valid descriptor.
- **Fix hint**: In each `resolveCustom*Options` function, add a check: if `ext.GetType() == TYPE_STRING || ext.GetType() == TYPE_BYTES`, and `opt.AggregateFields == nil && len(opt.SubFieldPath) == 0`, then validate `opt.ValueType == tokenizer.TokenString`. If not, emit error matching C++: `"Value must be quoted string for string option \"FQN\"."`. Same pattern as the TYPE_BOOL validation.
- **Also affects**: TYPE_BYTES options have the same bug. Also, setting a string option to `true` (identifier) or `inf` (identifier) would also be accepted by Go but likely rejected by C++. Aggregate option fields might also lack this validation.

### Run 56 — Enum custom option accepts integer value, C++ requires identifier (VICTORY)
- **Bug**: Go's custom option resolver accepts integer values for TYPE_ENUM options. When `option (my_level) = 1;` sets an enum option to an integer literal, C++ protoc rejects it with `Value must be identifier for enum-valued option`, but Go accepts it and encodes the integer as a varint.
- **Test**: `387_enum_opt_int_value` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `encodeCustomOptionValue()` at cli.go:6450 for TYPE_ENUM first tries `strconv.ParseInt(value, 0, 32)`. When the value is an integer literal like `"1"`, this succeeds and the integer is encoded directly as a varint. C++ protoc's `OptionInterpreter::SetOption` validates that enum option values must be identifiers (enum value names), not integer literals.
- **C++ protoc**: `test.proto:17:21: Value must be identifier for enum-valued option "enumoptint.my_level".`
- **Go protoc-go**: Silently accepts `1`, encodes as varint 1, produces valid descriptor.
- **Fix hint**: In each `resolveCustom*Options` function, add a check: if `ext.GetType() == TYPE_ENUM && opt.AggregateFields == nil && len(opt.SubFieldPath) == 0`, then validate `opt.ValueType == tokenizer.TokenIdent`. If not, emit error: `"Value must be identifier for enum-valued option \"FQN\"."`. The integer encoding in `encodeCustomOptionValue` is correct for aggregate option values (text format allows integers for enum fields), so the validation should be at the resolver level, not the encoder level.
- **Also affects**: All 9 `resolveCustom*Options` functions (file, field, message, service, method, enum, enum_value, oneof, ext_range) are likely missing this validation. Also, hex integers like `0x01` and negative integers like `-1` would also be accepted by Go but rejected by C++ for enum options.

### Run 68 — Field-level float option accepts `Inf` (capital I), C++ rejects it (VICTORY)
- **Bug**: Go's `resolveCustomFieldOptions` is completely MISSING float/double identifier validation. There's a stale comment at cli.go:4561 saying "Validate float/double identifier values must be lowercase 'inf' or 'nan'" but the actual validation code was never added — the lines that follow are about SCI path fixing, not float validation. When a field has `[(field_threshold) = Inf]`, Go accepts it and encodes float32 infinity, while C++ rejects it with "Value must be number".
- **Test**: `398_field_float_inf_case` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `resolveCustomFieldOptions` at cli.go:4561 has NO float/double validation code at all. The comment is there but the actual check is missing. Compare with file-level (line 4186-4197), message-level (line 4829-4841), service-level (line 5078-5090), method-level (line 5325-5337), enum-level (line 5563-5575), oneof-level (line 6062-6074) — all have the validation. Field-level is the only resolver completely missing it.
- **C++ protoc**: `test.proto:14:49: Value must be number for float option "fieldfloatcase.field_threshold".`
- **Go protoc-go**: Silently accepts `Inf`, encodes as float32 infinity, produces valid descriptor.
- **Fix hint**: Add the float validation block after the stale comment at line 4561. Use the same pattern as other resolvers. For field-level, the parser DOES bake `-` into values, so use `floatCheckVal` pattern (strip leading `-` before checking). Example: `floatCheckVal := opt.Value; if opt.Negative && strings.HasPrefix(floatCheckVal, "-") { floatCheckVal = floatCheckVal[1:] }; if floatCheckVal != "inf" && floatCheckVal != "nan" { ... }`.
- **Also affects**: Same `Inf`/`NaN` acceptance bug for field-level double options. Also, the enum-value-level (line 5812) and ext-range-level (line 6308) resolvers have float validation but use `opt.Value` without stripping `-`, so `-inf` and `-nan` would be rejected — same bug as Run 67 but on different entity types.

### Run 69 — Field-level float option with `-inf` rejected by Go, accepted by C++ (VICTORY)
- **Bug**: Go's `resolveCustomFieldOptions` float validation at cli.go:4563 does NOT strip the leading `-` prefix before checking for `inf`/`nan`. The field parser bakes `-` into `opt.Value` (parser.go:5478: `custOpt.Value = "-" + custOpt.Value`), so `opt.Value = "-inf"`. The check `floatCheckVal != "inf" && floatCheckVal != "nan"` evaluates `"-inf" != "inf"` → true, so Go incorrectly rejects `-inf` as invalid. C++ protoc accepts `-inf` fine.
- **Test**: `399_field_neg_inf_option` — all 9 profiles fail (C++ succeeds, Go errors).
- **Root cause**: Ralph fixed Run 68 by adding float validation to the field-level resolver but forgot to add the `strings.HasPrefix(floatCheckVal, "-")` stripping that was added for message/service/method/enum/oneof resolvers (Run 67 fix). Compare line 4563 (no strip) with line 4845 (has strip).
- **C++ protoc**: Accepts `[(field_threshold) = -inf]` fine, produces valid descriptor with -infinity encoded.
- **Go protoc-go**: `test.proto:12:49: Value must be number for double option "fieldneginf.field_threshold".`
- **Fix hint**: Add `if strings.HasPrefix(floatCheckVal, "-") { floatCheckVal = floatCheckVal[1:] }` after `floatCheckVal := opt.Value` at line 4563, matching the pattern at line 4845.
- **Also affects**: Same `-` stripping missing in enum-value-level (line 5825) and ext-range-level (line 6322) resolvers. Both would reject `-inf`/`-nan` for float/double options on those entity types.

### Run 71 — Enum-value-level float option with `-inf` rejected by Go, accepted by C++ (VICTORY)
- **Bug**: Go's `resolveCustomEnumValueOptions` float validation at cli.go:5872 does NOT strip the leading `-` prefix before checking for `inf`/`nan`. The enum-value parser bakes `-` into `opt.Value` (parser.go:2622: `custOpt.Value = "-" + custOpt.Value`), so `opt.Value = "-inf"`. The check `opt.Value != "inf" && opt.Value != "nan"` evaluates `"-inf" != "inf"` → true, so Go incorrectly rejects `-inf` as invalid. C++ protoc accepts `-inf` fine.
- **Test**: `401_enumval_neg_inf_option` — all 9 profiles fail (C++ succeeds, Go errors).
- **Root cause**: Same bug as Run 69 (field-level) but on enum-value-level. Ralph fixed field-level by adding `strings.HasPrefix(floatCheckVal, "-")` stripping, but never applied the fix to enum-value-level or ext-range-level resolvers.
- **C++ protoc**: Accepts `[(val_threshold) = -inf]` on enum value, produces valid descriptor with -infinity encoded.
- **Go protoc-go**: `test.proto:12:30: Value must be number for double option "evneginf.val_threshold".`
- **Fix hint**: Add `if strings.HasPrefix(floatCheckVal, "-") { floatCheckVal = floatCheckVal[1:] }` after the `floatCheckVal := opt.Value` at line 5872, matching the pattern in other resolvers.
- **Also affects**: Same `-` stripping missing in ext-range-level (line 6368) resolver. That would reject `-inf`/`-nan` for float/double options on extension ranges too.

### Run 73 — Edition `features.message_encoding` accepted on non-message field (VICTORY)
- **Bug**: Go does NOT validate that `features.message_encoding` can only be set on message-typed fields. When a scalar field (e.g., `int32`) uses `int32 value = 1 [features.message_encoding = DELIMITED];` in an edition 2023 proto file, C++ protoc rejects it with `Only message fields can specify message encoding.`, but Go accepts it silently and produces a valid descriptor.
- **Test**: `403_edition_msg_encoding_scalar` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go has `checkRepeatedFieldEncodingField` for `repeated_field_encoding` and `checkFieldPresenceRepeatedField` for `field_presence` on repeated fields, but has NO equivalent validation for `message_encoding`. The `featureTargets` map at cli.go:2038 allows `message_encoding` on fields, and no additional validation checks `field.GetType() == TYPE_MESSAGE || field.GetType() == TYPE_GROUP` to reject it on scalar fields.
- **C++ protoc**: `test.proto:6:9: Only message fields can specify message encoding.`
- **Go protoc-go**: Silently accepts, encodes FeatureSet with DELIMITED message_encoding, produces valid descriptor.
- **Fix hint**: Add a `checkMessageEncodingField` function similar to `checkRepeatedFieldEncodingField` that rejects `message_encoding` on non-message fields: if `field.GetType() != TYPE_MESSAGE && field.GetType() != TYPE_GROUP && field.GetOptions().GetFeatures().GetMessageEncoding() != descriptorpb.FeatureSet_LENGTH_PREFIXED`, emit error. Also add a `collectMessageEncodingErrors` function that walks all messages/nested types, and call it from the validation pass.
- **Also affects**: Same validation is missing for `features.utf8_validation` on non-string fields (C++ likely validates this too). Also, `features.message_encoding = DELIMITED` on a map field should probably also be rejected.

### Run 74 — Edition `features.field_presence = LEGACY_REQUIRED` on extension field accepted by Go (VICTORY)
- **Bug**: Go does NOT validate that `features.field_presence = LEGACY_REQUIRED` cannot be set on extension fields in edition 2023. When an extension field uses `extend Extendable { Payload info = 100 [features.field_presence = LEGACY_REQUIRED]; }`, C++ protoc rejects it with `Extensions can't be required.`, but Go accepts it silently and produces a valid descriptor.
- **Test**: `404_edition_required_ext` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's `validateRequiredExtensions` at cli.go:3226 only checks for `LABEL_REQUIRED` (proto2 `required` keyword). In editions mode, required semantics are expressed via `features.field_presence = LEGACY_REQUIRED`, which sets the feature but does NOT change the label to `LABEL_REQUIRED`. So the existing validation doesn't catch it. C++ protoc's `ValidateFieldFeatures` separately checks for `LEGACY_REQUIRED` on extensions in edition files.
- **C++ protoc**: `test.proto:14:11: Extensions can't be required.`
- **Go protoc-go**: Silently accepts, encodes FeatureSet with LEGACY_REQUIRED field_presence, produces valid descriptor.
- **Fix hint**: Add a check in the editions validation pass (or in `validateRequiredExtensions`): for edition files, check if any extension field has `features.field_presence = LEGACY_REQUIRED` in its options. If so, emit `"Extensions can't be required."` with proper line:col info. Check both top-level extensions (`fd.GetExtension()`) and message-level extensions.
- **Also affects**: Same validation is likely missing for `features.field_presence = LEGACY_REQUIRED` on oneof members — C++ rejects that too with a different message.

### Run 76 — Edition `features.message_encoding = DELIMITED` accepted on map field (VICTORY)
- **Bug**: Go's `checkMessageEncodingScalarField` at cli.go:2546 checks `field.GetType() == TYPE_MESSAGE || field.GetType() == TYPE_GROUP` and returns early (allows) if true. Map fields have `TYPE_MESSAGE` type in the descriptor (they're synthetic repeated message fields), so Go allows `features.message_encoding = DELIMITED` on map fields. C++ protoc has a separate `is_map()` check and rejects message_encoding on map fields with "Only message fields can specify message encoding."
- **Test**: `406_map_msg_encoding` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's validation only checks if the field type is NOT a message type (scalar rejection). It doesn't check if the message-typed field is a map field. Map fields ARE `TYPE_MESSAGE` but should NOT be eligible for `message_encoding` overrides. C++ protoc checks `f->is_map()` and rejects.
- **C++ protoc**: `test.proto:6:22: Only message fields can specify message encoding.`
- **Go protoc-go**: Silently accepts, encodes FeatureSet with DELIMITED message_encoding on the map field, produces valid descriptor.
- **Fix hint**: In `checkMessageEncodingScalarField`, after the TYPE_MESSAGE/TYPE_GROUP return, add a check for map fields. Map fields can be identified by checking if the message type they point to has `options.map_entry = true`. Look up the type_name in the parsed descriptors and check `msg.GetOptions().GetMapEntry()`. Alternatively, check if the field has `GetLabel() == LABEL_REPEATED` and the type_name ends with `Entry` and is a nested message with `map_entry = true`. Simpler: pass a set of map entry type names and check `field.GetTypeName()` against it.
- **Also affects**: Same validation gap exists for `features.repeated_field_encoding` on map fields — C++ might also reject that. And `features.field_presence` on map fields — C++ rejects with "Repeated fields can't specify field presence" since map fields are repeated.

### Run 77 — Edition `features.field_presence` accepted on oneof member field (VICTORY)
- **Bug**: Go does NOT validate that `features.field_presence` cannot be set on fields inside a `oneof`. In edition 2023, when a oneof member has `string name = 1 [features.field_presence = LEGACY_REQUIRED];`, C++ protoc rejects it with `Oneof fields can't specify field presence.`, but Go accepts it silently and produces a valid descriptor.
- **Test**: `407_edition_required_oneof` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's `checkRequiredExtensionEditionsField` at cli.go:2597 only checks extension fields for LEGACY_REQUIRED, not oneof members. And Go has no validation for ANY field_presence feature on oneof members. C++ protoc's `ValidateFieldFeatures` checks `f->real_containing_oneof()` and rejects ANY field_presence override on oneof members — not just LEGACY_REQUIRED but also EXPLICIT and IMPLICIT.
- **C++ protoc**: `test.proto:7:12: Oneof fields can't specify field presence.`
- **Go protoc-go**: Silently accepts, encodes FeatureSet with LEGACY_REQUIRED field_presence on the oneof member, produces valid descriptor.
- **Fix hint**: Add a validation check in the editions validation pass: iterate all messages, then for each message iterate its fields, check if `field.GetOneofIndex() != nil` (field is in a oneof), and if so, check if `field.GetOptions().GetFeatures().FieldPresence != nil`. If both true, emit error: `"Oneof fields can't specify field presence."` with proper line:col info from the field's SCI name path.
- **Also affects**: Same validation is missing for `features.field_presence = EXPLICIT` and `features.field_presence = IMPLICIT` on oneof members — C++ rejects ALL field_presence overrides on oneof members, not just LEGACY_REQUIRED. Also, `features.field_presence` on map field entries might need similar validation (C++ rejects with "Repeated fields can't specify field presence" since map fields are repeated).

### Run 78 — Bytes option error message says "bytes" instead of "string" (VICTORY)
- **Bug**: Go's custom option resolver correctly distinguishes between `TYPE_STRING` and `TYPE_BYTES` in the error message when a non-string value is given, saying "bytes option". But C++ protoc uses `CPPTYPE_STRING` which covers both types and always says "string option". The error messages differ.
- **Test**: `409_bytes_option_int_error` — all 9 profiles fail.
- **Root cause**: In `resolveCustomFileOptions` (cli.go:4470-4476), Go checks `ext.GetType() == TYPE_BYTES` and sets `typeName = "bytes"`. In C++, both `TYPE_STRING` and `TYPE_BYTES` map to `CPPTYPE_STRING`, so the error always says `"string option"`. Same pattern exists in all 9 `resolveCustom*Options` functions.
- **C++ protoc**: `test.proto:11:21: Value must be quoted string for string option "bytesopterr.my_bytes".`
- **Go protoc-go**: `test.proto:11:21: Value must be quoted string for bytes option "bytesopterr.my_bytes".`
- **Fix hint**: Change the check at line 4470-4472 (and all 8 other resolver functions) to always use `typeName := "string"` regardless of whether it's TYPE_STRING or TYPE_BYTES, matching C++ behavior.

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
- ~~Edition features.repeated_field_encoding on non-repeated field~~ **DONE in Run 70 (400_edition_repeated_encoding)**
- Edition features scope restrictions (message_encoding on non-message field, utf8_validation on non-string field)
- Edition features + extensions interactions
- Proto files importing the same file via different paths
- Custom option scope resolution (Go returns first match, not proper scope-based lookup)
- ~~Sub-field option type validation (Go doesn't check intermediate fields are MESSAGE type)~~ **DONE in Run 9 (340_scalar_subfield_option)**
- Extension extendee type validation (Go doesn't check extendee is MESSAGE)
- Block comment at EOF without trailing newline (similar bug to line comment?)
- Positive sign `+` in aggregate values for angle-bracket syntax `< count: +42 >` — same bug as Run 11 in `consumeAggregateAngle`
- Aggregate option with `+inf` or `+nan` — how does Go handle these?
- ~~Repeated custom options corrupted when sub-field options present~~ **DONE in Run 12 (343_repeated_with_subfield)**
- ~~Same merge bug but with repeated MESSAGE typed options (repeated Config vs sub-field) — would produce even more corrupted output~~ **Covered by Run 13 (344_field_repeated_merge) — field-level merge still passes nil**
- Same merge bug with repeated bytes options — bytes payloads would be concatenated
- Same merge bug for extension field options (cli.go:3453 also passes nil) — test with repeated options on extension fields
- mergeFieldOptionsInMessages doesn't compute per-field mergeableFields — needs the same fix as file-level
- ~~Message sub-field options not merged~~ **DONE in Run 14 (345_msg_subfield_merge)**
- ~~Enum sub-field options not merged — same pattern as Run 14, `cloneWithMergedExtUnknowns` ignores EnumOptions~~ **DONE in Run 15 (346_enum_subfield_merge)**
- ~~Service sub-field options not merged — same pattern~~ **DONE in Run 16 (347_svc_subfield_merge)**
- ~~Method sub-field options not merged — same pattern~~ **DONE in Run 17 (348_method_subfield_merge)**
- ~~Oneof sub-field options not merged — same pattern~~ **DONE in Run 18 (349_oneof_subfield_merge)**
- ~~EnumValue sub-field options not merged — same pattern~~ **DONE in Run 19 (350_enumval_subfield_merge)**
- ~~ExtensionRange sub-field options not merged — same pattern~~ **DONE in Run 20 (351_extrange_subfield_merge) — actually a parser bug, can't even parse sub-field path syntax**
- Extension range options merge still missing in `cloneWithMergedExtUnknowns` — even after parser fix, merge will fail (test with flat option first to confirm merge is missing too)
- `+inf` in aggregate option values — Go likely doesn't handle `+` prefix for infinity
- Block comment at EOF without trailing newline — similar to Run 2 line comment bug
- Source code info path differences for extension range options
- ~~Custom option scope resolution — nested messages may resolve differently~~ **DONE in Run 23 (354_nested_ext_scope) — bare name fallback bypasses scope**
- ~~Case-insensitive float/double default values — Go accepts `NaN`/`Inf`/`INF`/`infinity`/`Infinity`, C++ only accepts lowercase `nan`/`inf`~~ **DONE in Run 25 (356_infinity_default)**
- ~~Same case-sensitivity issue may exist in custom option value parsing (e.g., `option (my_opt) = Infinity;` — does Go accept it?)~~ **DONE in Run 31 (362_float_option_case) — `Inf` accepted by Go, rejected by C++**
- ~~`simpleFtoa` edge case: find a specific float32 value where Go's `FormatFloat(float64(v), 'g', 6, 64)` differs from C++'s `snprintf(buf, "%.6g", f)` due to the float64 bit width parameter~~ **DONE in Run 26 (357_float_overflow_default) — overflow to infinity produces `"+Inf"` vs `"inf"`**
- ~~`simpleDtoa` same issue for double overflow (e.g., `1e309`) — Go's `ParseFloat` returns `ErrRange` so normalization is skipped entirely, storing raw `"1e309"` instead of `"inf"`~~ **DONE in Run 27 (358_double_overflow_default)**
- Double default with `-0.0` or `0.0` — verify both produce same string
- Negative infinity overflow: `default = -3.5e38` for float → Go produces `"-Inf"` vs C++ `"-inf"` (case difference)
- Custom bool option with `"f"` or `"t"` — same bug as Run 28 but different variant
- Custom bool option in aggregate: `option (cfg) = { enabled: True }` — also accepts `True`
- Aggregate option bool with `"t"` or `"f"` — `encodeAggregateFields` likely has same permissive bool handling
- ~~Enum shadow in compound type resolution — Go skips enums, C++ stops at shadow~~ **DONE in Run 29 (360_enum_shadow_scope)**
- Similar shadow bugs: compound names where first part matches a package name? Or matches a different non-message type?
- Extension extendee scope resolution differences — similar compound name resolution issues
- `resolveTypeName` with three-part compound names (e.g., `A.B.C`) — multiple levels of scope walking
- Custom double option with `NaN` (mixed case) — same bug as Run 31 for double type
- Custom float option with `INF` (all caps) — same bug as Run 31
- Aggregate option float/double with `Inf`/`NaN` — `encodeAggregateFields` likely has same permissive parsing
- `strconv.ParseFloat` accepts `+inf` and `+Inf` — custom option with `+inf` might differ
- Vertical tab (`\v`, 0x0B) as whitespace — same bug as Run 33 form feed, but for `\v` specifically
- Source code info for `option` statements on extension range vs extension field
- `resolveTypeName` with three-part compound names (e.g., `A.B.C`) — multiple levels of scope walking
- Same int32/uint32 overflow bug for field-level, service-level, method-level, enum-level, enum-value-level, oneof-level, ext-range-level custom options (all missing `checkIntRangeOption`)
- ~~Same int32 overflow bug in aggregate option encoding (`encodeAggregateFields` → `encodeCustomOptionValue`) — no range check there either~~ **DONE in Run 35 (366_aggregate_int32_overflow)**
- Same aggregate overflow bug for uint32 (e.g., `0x100000000`), sint32, sfixed32, fixed32 — all 32-bit types in aggregate values
- Aggregate bool with `True`/`t`/`f` — `encodeCustomOptionValue` still accepts case variants (line 5958)
- Aggregate float/double with `Inf`/`NaN` (mixed case) — `strconv.ParseFloat` is case-insensitive
- Source code info for extension declarations inside messages vs top-level
- ~~Duplicate non-repeated custom option on field-level — same bug as Run 36 but for FieldOptions~~ **DONE in Run 44 (375_field_dup_option)**
- Duplicate non-repeated custom option on enum/service/method/oneof/enum-value/ext-range — all missing seenCustomOpts check
- Bool validation missing in message/field/enum/service/method/oneof/enum-value/ext-range resolvers — `True`/`False` accepted
- Float/double identifier validation missing in message/field/enum/service/method/oneof/enum-value/ext-range resolvers — `Inf`/`NaN` accepted
- Aggregate option with `True` bool value — `encodeCustomOptionValue` still accepts `True` at line 5969, bypasses simple option validation- ~~Subnormal float default value string differs~~ **DONE in Run 41 (372_subnormal_float_default)**
- Same `simpleFtoa` issue with `simpleDtoa` for subnormal double values — might also differ
- `simpleFtoa` with `FormatFloat(v64, 'g', 6, 32)` (32-bit) vs `FormatFloat(v64, 'g', 6, 64)` (64-bit) — changing bit width may fix some but break others
- ~~Enum-value-level bool option with `True` — still missing validation in `resolveCustomEnumValueOptions`~~ **DONE in Run 42 (373_enumval_bool_option_case)**
- ~~Oneof-level bool option with `True` — still missing validation in `resolveCustomOneofOptions`~~ **DONE in Run 43 (374_oneof_bool_option_case)**
- Duplicate non-repeated custom option on field/enum/service/method/enum-value/oneof/ext-range — all missing `seenCustomOpts` check
- Aggregate option bool with `True` — `encodeCustomOptionValue` still accepts `True`/`t`/`f` at line 6091
- Aggregate option float/double with `Inf`/`NaN` (mixed case) — `strconv.ParseFloat` is case-insensitive
- String option with identifier value (e.g., `option (my_str) = true;`) — Go likely accepts, C++ rejects
- Bytes option with integer value (e.g., `option (my_bytes) = 42;`) — same bug as Run 52 but for TYPE_BYTES
- String/bytes validation missing in all 9 resolveCustom*Options — same pattern as bool validation (Runs 37-43)
- Float/double option with string value (e.g., `option (my_float) = "hello";`) — does Go reject properly?
- Enum option with string value — same category of type mismatch validation
- Enum option with integer value on message/field/service/method/enum/enum_value/oneof/ext_range — same bug as Run 56, all resolvers likely missing validation
- Aggregate option enum field validation — `encodeCustomOptionValue` correctly accepts integers for enum in aggregate (text format), but does it match C++ for invalid enum value names?
- Float/double option with identifier value other than `inf`/`nan` (e.g., `option (my_float) = FOO;`) — does Go reject properly?
- ~~Surrogate code point in `\U` escape — Go replaces with U+FFFD, C++ encodes raw bytes~~ **DONE in Run 60 (391_surrogate_escape)**
- Lone surrogate via `\u` (not paired) — Go tokenizer's `\u` path checks for head/trail surrogate pairing but a lone head surrogate without trail might also get replaced; and a lone trail surrogate (0xDC00-0xDFFF) with no preceding head surrogate goes straight to `appendUTF8` which would also replace it
- Other invalid Unicode code points via `\U` — e.g., `\U0000FFFE` (non-character), `\U0000FFFF` — does Go treat these as valid?
- ~~Subnormal double default value — `simpleDtoa` has no subnormal check unlike `simpleFtoa`~~ **TESTED Run 60 — C++ and Go agree on subnormal double formatting, no bug**
- ~~Aggregate bool `True` — C++ text format also accepts `True`, not a bug~~ **TESTED Run 60 — both accept**
- ~~Aggregate float `Inf`/`INF` — C++ text format also case-insensitive~~ **TESTED Run 60 — both accept**
- ~~CRLF `\r\n` in comments — C++ also preserves `\r` in comment text~~ **TESTED Run 60 — both match**

### Run 53 — Group field name lookup fails in aggregate option encoding (VICTORY)
- **Bug**: Go's `collectMsgFields()` builds `msgFieldMap` keyed by field names. For group fields, the field name is lowercased (e.g., `"inner"`), but in C++ text format (used by protoc for aggregate options), groups are referenced by their MESSAGE TYPE NAME (capitalized, e.g., `"Inner"`). When a user writes `option (cfg) = { Inner { name: "hello" } }`, Go looks up `msgFields["Inner"]` which doesn't exist — the key is `"inner"`.
- **Test**: `384_group_aggregate_option` — all 9 profiles fail.
- **Root cause**: `collectMsgFields()` at cli.go:6481 inserts `fields[f.GetName()] = f` where `f.GetName()` for a group field returns the lowercased field name `"inner"`. But the parser stores `af.Name = "Inner"` from the source text. Then `encodeAggregateFields()` does `msgFields["Inner"]` which fails.
- **C++ protoc**: Accepts it — text format uses message type name for groups.
- **Go protoc-go**: Fails with `error encoding custom option: unknown field "Inner" in message test.Config`.
- **Fix hint**: In `collectMsgFields`, for `TYPE_GROUP` fields, also add the message type name as a key: extract the last component of `f.GetTypeName()` (which is the CamelCase message name) and add `fields[typeName] = f` as an alias.
- **Also affects**: Any aggregate option value that references a group field by its type name (which is the standard text format convention).

### Run 54 — Negative uint32 option error message and column mismatch (VICTORY)
- **Bug**: Go's error message for negative unsigned option values differs from C++ protoc in both wording and column number. C++ says `"Value must be integer, from 0 to 4294967295"` at column 19 (pointing at `-`). Go says `"Value out of range, 0 to 4294967295"` at column 20 (pointing at `1`).
- **Test**: `385_neg_uint_option` — all 9 profiles fail.
- **Root cause**: Two issues: (1) Go's error message uses `"Value out of range"` instead of C++'s `"Value must be integer"`. C++ treats negative values for unsigned types as "not an integer in range" rather than "out of range". (2) Go's error column points at the numeric value token (`1` at column 20) instead of the sign token (`-` at column 19). The sign and value are separate tokens; Go reports the value token position, C++ reports the sign position.
- **C++ protoc**: `test.proto:10:19: Value must be integer, from 0 to 4294967295, for uint32 option "neguint.my_val".`
- **Go protoc-go**: `test.proto:10:20: Value out of range, 0 to 4294967295, for uint32 option "neguint.my_val".`
- **Fix hint**: (1) Change the error message from `"Value out of range"` to `"Value must be integer"` to match C++ wording. (2) Track the position of the `-` sign token and use it for the error column when reporting unsigned negative value errors. The sign token position is available in the parser where `opt.Negative` is set.
- **Also affects**: Same error message mismatch likely exists for `uint64`, `fixed32`, `fixed64` options with negative values. Also, if this check is in `checkIntRangeOption`, it affects all resolver types that call it.

### Run 55 — Aggregate option string field accepts integer value (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` for TYPE_STRING/TYPE_BYTES does not validate that the value is a string literal. When an aggregate option has `{ name: 42 }` where `name` is a `string` field, Go accepts the integer `42` and encodes it as the string `"42"`. C++ protoc's text format parser requires string fields to have quoted string values and rejects integers with `Expected string, got: 42`.
- **Test**: `386_aggregate_string_int` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `encodeCustomOptionValue()` at cli.go:6339-6342 for TYPE_STRING/TYPE_BYTES just does `protowire.AppendString(b, value)` without checking `valueType`. The aggregate path goes from `encodeAggregateFields` → `encodeCustomOptionValue` without any string type validation. Run 52 fixed simple option string validation in the `resolveCustom*Options` functions, but aggregate options bypass those resolvers entirely.
- **C++ protoc**: `test.proto:16:16: Error while parsing option value for "cfg": Expected string, got: 42`
- **Go protoc-go**: Silently accepts `42`, encodes as string bytes `"42"`, produces valid descriptor.
- **Fix hint**: In `encodeCustomOptionValue`, for TYPE_STRING and TYPE_BYTES, check `valueType != tokenizer.TokenString` and return an error like `Expected string, got: VALUE`. Or add validation in `encodeAggregateFields` before calling `encodeCustomOptionValue` for string/bytes fields.
- **Also affects**: Same bug for TYPE_BYTES fields. Also, setting a string field to an identifier (`name: foo`) or a float (`name: 3.14`) would also be accepted by Go but rejected by C++. This affects all aggregate option encoding paths (both `{ }` and `< >` syntax).

### Run 57 — Empty aggregate option `option (cfg) = {};` fails in Go (VICTORY)
- **Bug**: Go's `consumeAggregate()` returns `nil` for an empty `{}` aggregate, causing `AggregateFields` to be nil. The resolver then falls through to `encodeCustomOptionValue` which doesn't handle TYPE_MESSAGE, producing `unsupported custom option type: TYPE_MESSAGE`. C++ protoc handles empty aggregate options fine, encoding an empty message (zero payload bytes).
- **Test**: `388_empty_aggregate_option` — all 9 profiles fail.
- **Root cause**: `consumeAggregate()` at parser.go:4978 initializes `var fields []AggregateField` which is nil. When the aggregate `{}` is empty, the loop doesn't execute and `nil` is returned. In the resolver at cli.go:4321, `if opt.AggregateFields != nil` is false, so the code falls through to `encodeCustomOptionValue(ext, opt.Value, ...)` where `opt.Value` is `{` and ext type is TYPE_MESSAGE — which hits the default case error.
- **C++ protoc**: Accepts `option (cfg) = {};` and produces valid descriptor with empty Config option.
- **Go protoc-go**: Fails with `error encoding custom option: unsupported custom option type: TYPE_MESSAGE`.
- **Fix hint**: Either (1) change `consumeAggregate()` to return `[]AggregateField{}` instead of nil for empty aggregates (e.g., `fields = make([]AggregateField, 0)`), or (2) in the resolver, also check `valTok.Value == "{"` to detect aggregate syntax even with nil fields, or (3) more robustly, initialize the `aggregateFields` variable in `parseFileOption` to a non-nil empty slice when `{` is consumed.
- **Also affects**: Same bug likely exists in all 9 option parsers (file, message, field, enum, enum_value, service, method, oneof, extension range) — any that call `consumeAggregate()` and check `AggregateFields != nil`.

### Run 58 — Aggregate option field encoding order differs from C++ (VICTORY)
- **Bug**: Go's `encodeAggregateFields()` encodes fields in **source order** (the order they appear in the aggregate option text), while C++ protoc's `TextFormat::Parser` encodes fields in **field number order**. When fields are listed out of field-number order (e.g., field 2 before field 1), the encoded bytes differ.
- **Test**: `389_agg_field_order` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `encodeAggregateFields()` at cli.go:6703 iterates `aggFields` in the order they were parsed (source order) and appends encoded bytes directly. C++ protoc's text format parser sorts/inserts fields by field number when building the message, so the wire format always has fields in field-number order regardless of source order.
- **C++ protoc**: Encodes `child` (field 1) before `value` (field 2) even when `value: 42` is written before `child { ... }` in the source.
- **Go protoc-go**: Encodes `value` (field 2) first, then `child` (field 1), following source order.
- **Fix hint**: Either (1) sort `aggFields` by field number before encoding, or (2) collect encoded bytes with their field numbers and sort before concatenating, or (3) sort the `inner` byte slice by tag after encoding all fields. Option (1) is simplest: `sort.Slice(aggFields, func(i, j int) bool { return msgFields[aggFields[i].Name].GetNumber() < msgFields[aggFields[j].Name].GetNumber() })` before the encoding loop. Note: repeated fields with the same number must preserve relative order.
- **Also affects**: Same issue exists in `encodeAggregateOption()` which has a similar loop. Both functions need the same fix.

### Run 59 — Aggregate option invalid enum value error message mismatch (VICTORY)
- **Bug**: Go's error message for invalid enum value names in aggregate option fields differs from C++ protoc in format, detail, and line/column info. C++ says `Error while parsing option value for "my_cfg": Unknown enumeration value of "NONEXISTENT" for field "level"` with line:column. Go says `error encoding custom option: field level: enum type ".agetest.Level" has no value named "NONEXISTENT"` with no column info.
- **Test**: `390_agg_bad_enum` — all 9 profiles fail.
- **Root cause**: `encodeAggregateFields()` generates a Go-specific error message when an enum value lookup fails, using the internal enum type name format (`.pkg.EnumType`) and different wording. It also lacks the aggregate option name context and line/column info that C++ includes. C++ wraps the error with `Error while parsing option value for "OPTION_NAME": ...` and includes the source location of the aggregate.
- **C++ protoc**: `test.proto:22:19: Error while parsing option value for "my_cfg": Unknown enumeration value of "NONEXISTENT" for field "level".`
- **Go protoc-go**: `test.proto: error encoding custom option: field level: enum type ".agetest.Level" has no value named "NONEXISTENT"`
- **Fix hint**: (1) Change the enum lookup error message to match C++'s wording: `Unknown enumeration value of "VALUE" for field "FIELD"`. (2) Wrap aggregate encoding errors with the option name context: `Error while parsing option value for "OPT_NAME": ...`. (3) Include line/column info from the source token positions.
- **Also affects**: Other aggregate option encoding errors (type mismatches, unknown fields) likely have similar message format differences.

### Run 60 — Surrogate code point in \U escape produces different bytes (VICTORY)
- **Bug**: Go's `appendUTF8` function in `io/tokenizer/tokenizer.go:659` uses `utf8.EncodeRune` to encode Unicode code points from `\U` escape sequences. `utf8.EncodeRune` replaces surrogate code points (0xD800–0xDFFF) with U+FFFD (replacement character). C++ protobuf's `AppendUTF8` does raw UTF-8 encoding without surrogate validation, producing the (invalid but byte-accurate) 3-byte UTF-8 sequence for the surrogate.
- **Test**: `391_surrogate_escape` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `appendUTF8(sb, 0xD800)` calls `utf8.EncodeRune(buf, rune(0xD800))`. Go's `utf8.ValidRune(0xD800)` returns false (surrogate range), so `EncodeRune` substitutes `RuneError` (U+FFFD = 0xEF 0xBF 0xBD). C++ just encodes 0xD800 as 3-byte UTF-8: 0xED 0xA0 0x80. The decoded string bytes differ, so `cEscape` produces different octal representations in the default value.
- **C++ protoc**: default_value = `\355\240\200` (bytes ED A0 80, raw encoding of 0xD800)
- **Go protoc-go**: default_value = `\357\277\275` (bytes EF BF BD, encoding of U+FFFD)
- **Fix hint**: In `appendUTF8`, bypass `utf8.EncodeRune` for surrogate code points and do raw UTF-8 encoding manually: `if cp >= 0xD800 && cp <= 0xDFFF { sb.WriteByte(byte(0xE0 | (cp >> 12))); sb.WriteByte(byte(0x80 | ((cp >> 6) & 0x3F))); sb.WriteByte(byte(0x80 | (cp & 0x3F))); return }`. Or use a custom UTF-8 encoder that doesn't validate surrogates.
- **Also affects**: Any string/bytes literal containing `\U` escapes with surrogate code points (0xD800–0xDFFF). Also affects `\u` escapes for lone surrogates (not part of a surrogate pair), if the Go tokenizer can produce lone surrogates through the `\u` path.

### Run 61 — Aggregate option float/double field with string value produces wrong error message (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` for TYPE_FLOAT/TYPE_DOUBLE just tries `strconv.ParseFloat(value, N)` without checking the token type. When an aggregate option has `{ ratio: "not_a_number" }` where `ratio` is a `double` field, Go returns a generic `"invalid double value: not_a_number"` error with no line:col info. C++ protoc's text format parser recognizes the wrong token type immediately and returns `"Expected double, got: \"not_a_number\""` with line:col info.
- **Test**: `392_agg_float_string_value` — all 9 profiles fail.
- **Root cause**: `encodeCustomOptionValue()` at cli.go:6472-6502 for TYPE_FLOAT and TYPE_DOUBLE never checks `valueType`. It directly passes `value` to `strconv.ParseFloat`, which returns `strconv.ErrSyntax` for non-numeric strings. The error is wrapped by `encodeAggregateFields` as `"field ratio: invalid double value: not_a_number"`, then by `formatAggregateError` which falls through to the generic format `"test.proto: error encoding custom option: field ratio: invalid double value: not_a_number"` (no line:col).
- **C++ protoc**: `test.proto:16:16: Error while parsing option value for "cfg": Expected double, got: "not_a_number"`
- **Go protoc-go**: `test.proto: error encoding custom option: field ratio: invalid double value: not_a_number`
- **Fix hint**: Add a `valueType` check in `encodeCustomOptionValue` for TYPE_FLOAT/TYPE_DOUBLE: if `valueType == tokenizer.TokenString`, return a typed error like `&aggregateExpectedDoubleError{gotValue: "\"" + value + "\""}` that `formatAggregateError` can match to produce the C++ format: `"Expected double, got: \"VALUE\""` with proper line:col from `braceTok`. Also need to add the new error type and handle it in `formatAggregateError`.
- **Also affects**: Same issue for TYPE_INT32, TYPE_INT64, TYPE_UINT32, TYPE_UINT64, TYPE_SINT32, TYPE_SINT64, TYPE_FIXED32, TYPE_FIXED64, TYPE_SFIXED32, TYPE_SFIXED64 — all integer types in aggregate options would produce generic parse errors instead of C++'s typed token-mismatch errors. Also affects TYPE_BOOL (if given a string value like `enabled: "yes"` — though this might already be caught by `aggregateBoolError`). Also affects TYPE_ENUM (if given a string literal instead of an identifier).

### Run 62 — Comment after opening brace treated as detached instead of trailing (VICTORY)
- **Bug**: Go's source code info handling does not treat a comment on the line immediately after a message's opening `{` as a trailing comment for the message. C++ protoc assigns `// Trailing on message opening` as `trailing_comments` on the message's SCI entry (path=[4,0]). Go instead treats it as a `leading_detached_comments` entry on the first field inside the message (path=[4,0,2,0]).
- **Test**: `393_trailing_comment_after_brace` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: C++ protoc's `SourceLocationTable` treats the first comment block inside a `{...}` scope (before any declarations) as a trailing comment for the scope's declaration. Go's `collectComments` in `io/tokenizer/tokenizer.go` does not implement this scope-trailing-comment logic — it simply collects comments as leading/detached for the next token, never assigning them as trailing to a preceding scope-opening statement.
- **C++ protoc**: path=[4,0] has trailing_comments: ' Trailing on the message opening\n'. path=[4,0,2,0] has leading_comments: ' Leading on field x\n' (no detached).
- **Go protoc-go**: path=[4,0] has NO trailing_comments. path=[4,0,2,0] has leading_comments: ' Leading on field x\n' AND leading_detached_comments: ' Trailing on the message opening\n'.
- **Fix hint**: After parsing the opening `{` of a message/enum/service/oneof block, if the next non-blank-line comment exists before the first declaration, assign it as `trailing_comments` for the block's declaration rather than as a detached leading comment for the first member. This requires coordinating between the parser and the SCI tracking — the parser needs to signal that the current comment context is "just opened a block" so that the next comment is tagged as trailing.
- **Also affects**: Same issue likely exists for enum `{`, service `{`, oneof `{`, and extend `{` blocks. Any scope-opening brace where a comment immediately follows on the next line.

### Run 63 — Group field leading comment attached to wrong SCI entry (VICTORY)
- **Bug**: Go's source code info assigns the leading comment on a proto2 group field to the **field descriptor** entry (path=[4,0,2,0]), while C++ protoc assigns it to the **nested message type** entry (path=[4,0,3,0]). In C++ protoc, group declarations produce two SCI entries: one for the field (no comment) and one for the nested message (with the leading comment). Go attaches the comment to the field instead.
- **Test**: `394_group_comment_sci` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: When parsing a group field, the parser generates SCI entries for both the field descriptor (path=[4,0,2,0]) and the nested message type (path=[4,0,3,0]). In C++ protoc, the leading comment is attached to the nested message type's location. In Go, `attachComments` is called on the field descriptor's location entry, which picks up the leading comment first. The nested message type's location entry is added later without the comment since it was already consumed.
- **C++ protoc**: path=[4,0,2,0] (field) has NO leading_comments. path=[4,0,3,0] (nested msg) has leading_comments: ' Leading comment on group\n'.
- **Go protoc-go**: path=[4,0,2,0] (field) has leading_comments: ' Leading comment on group\n'. path=[4,0,3,0] (nested msg) has NO leading_comments.
- **Fix hint**: In `parseGroupField`, call `attachComments` on the nested message type location entry (path=[4,0,3,0]) instead of (or before) the field location entry (path=[4,0,2,0]). The comment should be attached to the nested message, not the field. This matches C++ protoc's behavior where group comments describe the message type, not the field.
- **Also affects**: Same issue likely exists for group fields inside oneof declarations and group fields in extend blocks, if those code paths also attach comments to the field entry first.

### Run 65 — Multiple reserved name statements corrupt SCI location ordering (VICTORY)
- **Bug**: Go's `parseMessageReserved` uses `*nameIdx` (accumulated across ALL reserved name statements) instead of a `count` computed from a `startCount` when rearranging SCI locations. When a message has two separate `reserved "name";` statements, the second statement's rearrangement copies `*nameIdx` (= 2) entries instead of just the 1 entry added by the current statement, pulling in a duplicate of the first statement's name entry and corrupting the SCI location ordering.
- **Test**: `395_multi_reserved_name` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: At parser.go line ~867, the SCI rearrangement does `copy(p.locations[len(p.locations)-int(*nameIdx):], ...)` using the cumulative `*nameIdx` to determine how many entries to shift. Compare with reserved RANGES at line ~990 which correctly uses `count := int(*rangeIdx - startCount)`. The name path lacks a `startCount` variable.
- **C++ protoc**: Produces SCI entries in order: stmt[4,0,10], name[4,0,10,0], stmt[4,0,10], name[4,0,10,1] — interleaved statement and name entries.
- **Go protoc-go**: Produces SCI entries in order: stmt[4,0,10], stmt[4,0,10], name[4,0,10,0], name[4,0,10,1] — statements grouped together, names grouped together (wrong ordering).
- **Same size**: Both produce 229-byte descriptors, but binary content differs due to SCI entry ordering.
- **Fix hint**: Add `startNameCount := *nameIdx` before the name parsing loop, then use `count := int(*nameIdx - startNameCount)` in the copy/rearrangement, matching the pattern used for reserved ranges.
- **Also affects**: Same bug exists in enum reserved names (parser.go ~line 3231) and editions identifier reserved names (parser.go ~line 895). Any proto entity with two or more separate `reserved "name"` statements will have corrupted SCI ordering.

### Run 66 — Custom option scope resolution doesn't walk message scopes (VICTORY)
- **Bug**: Go's `findFileOptionExtension` only walks the package hierarchy (dot-separated package components) when resolving custom option names. It does NOT walk up through enclosing message scopes. When an extension is declared inside a message (e.g., `message Outer { extend google.protobuf.MessageOptions { ... } }`) and used by a nested message (`Outer.Inner`), Go fails to find it because it starts scope resolution from `fd.GetPackage()` instead of from the message's FQN.
- **Test**: `396_nested_extend_scope` — all 9 profiles fail.
- **Root cause**: `findFileOptionExtension()` at cli.go:4381 uses `currentPkg` (= `fd.GetPackage()`) as the starting scope. For message options resolved in `resolveCustomMessageOptions()` at line 4731, the scope is always the file's package (e.g., `nestedscope`), never the message's FQN (e.g., `nestedscope.Outer.Inner`). So the scope walk tries: `nestedscope.msg_label` → `msg_label` → not found. It never tries `nestedscope.Outer.Inner.msg_label` or `nestedscope.Outer.msg_label`, which is where C++ would find the extension (FQN = `nestedscope.Outer.msg_label`).
- **C++ protoc**: Accepts `option (msg_label) = "inner_value"` inside `Outer.Inner` because scope resolution walks: `nestedscope.Outer.Inner.msg_label` → `nestedscope.Outer.msg_label` → FOUND.
- **Go protoc-go**: Fails with `Option "(msg_label)" unknown` because scope walk starts from package, not message.
- **Fix hint**: (1) Add a `messageFQN` field to `CustomMessageOption` that captures the FQN of the message the option is on. (2) Pass this FQN as `currentPkg` to `findFileOptionExtension` instead of `fd.GetPackage()`. (3) Same fix needed for `CustomEnumOption`, `CustomServiceOption`, `CustomMethodOption`, `CustomOneofOption`, `CustomEnumValueOption`, `CustomExtRangeOption`, `CustomFieldOption` — all entity-level custom options should resolve from their entity's scope, not just the file package.
- **Also affects**: Any custom option on any entity type that references an extension declared inside a message. The bug exists in all `resolveCustom*Options` functions.

### Run 67 — Message-level negative inf/nan float option rejected by Go (VICTORY)
- **Bug**: Go's message-level option parser bakes `-` into `custOpt.Value` (parser.go:1722-1726: `val = "-" + val`), so `opt.Value = "-inf"` when the option is `option (x) = -inf;`. The CLI resolver's float/double validation at cli.go:4830-4831 checks `opt.Value != "inf" && opt.Value != "nan"` — since `"-inf" != "inf"` is TRUE, Go incorrectly rejects `-inf` as invalid. File-level options don't have this bug because the file parser stores the raw value without `-` prefix.
- **Test**: `397_msg_neg_inf_option` — all 9 profiles fail.
- **Root cause**: Inconsistent negation handling between file-level and message-level option parsers. File parser (line 4674) stores `Value: valTok.Value` (raw, no dash). Message parser (line 1722-1726) bakes `-` into value. The float validation only checks for `"inf"` and `"nan"`, not `"-inf"` or `"-nan"`.
- **C++ protoc**: Accepts `option (msg_threshold) = -inf;` on a message, produces valid descriptor with -inf encoded.
- **Go protoc-go**: Fails with `test.proto:12:28: Value must be number for double option "test.msg_threshold".`
- **Fix hint**: Either (1) strip the `-` prefix when doing the `inf`/`nan` check in the resolver, or (2) stop baking `-` into the value in the message parser (match file-level pattern), or (3) extend the check to also accept `"-inf"` and `"-nan"`.
- **Also affects**: Same bug exists in enum-level (parser.go:2995), service-level (parser.go:3500), method-level (parser.go:3753), oneof-level (parser.go:4261), enum-value-level (parser.go:2622), and field-level (parser.go:5478) option parsers — ALL bake `-` into value. So `-inf` and `-nan` would be rejected for ALL non-file-level float/double custom options.

### Run 70 — Edition `features.repeated_field_encoding` accepted on non-repeated field (VICTORY)
- **Bug**: Go's parser/validator does NOT check that `features.repeated_field_encoding` can only be applied to repeated fields. When a non-repeated field uses `int32 value = 1 [features.repeated_field_encoding = EXPANDED];` in an edition 2023 proto file, C++ protoc rejects it with `Only repeated fields can specify repeated field encoding.`, but Go accepts it silently and produces a valid descriptor with the feature set.
- **Test**: `400_edition_repeated_encoding` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's field option parsing for editions features does not validate that `repeated_field_encoding` is only meaningful for `LABEL_REPEATED` fields. C++ protoc has explicit validation in the feature resolver that checks `f->is_repeated()` before allowing `repeated_field_encoding` to be set.
- **C++ protoc**: `test.proto:8:9: Only repeated fields can specify repeated field encoding.`
- **Go protoc-go**: Silently accepts the feature, encodes it into the field's FeatureSet, produces valid descriptor.
- **Fix hint**: After parsing field options for edition proto files, add a validation check: if the field has `features.repeated_field_encoding` set and `field.GetLabel() != LABEL_REPEATED`, emit an error matching C++: `"Only repeated fields can specify repeated field encoding."`. The check should be in the parser's field option handling code or in a validation pass in the CLI.
- **Also affects**: Other edition features may have similar scope restrictions not validated by Go (e.g., `features.message_encoding` might only be valid on message-typed fields, `features.utf8_validation` might only be valid on string fields).

### Run 72 — Edition `features.field_presence` accepted on repeated field (VICTORY)
- **Bug**: Go does NOT validate that `features.field_presence` cannot be set on repeated fields. When a repeated field uses `repeated int32 values = 1 [features.field_presence = EXPLICIT];` in an edition 2023 proto file, C++ protoc rejects it with `Repeated fields can't specify field presence.`, but Go accepts it silently and produces a valid descriptor.
- **Test**: `402_field_presence_repeated` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go has `checkRepeatedFieldEncodingField` validation for `repeated_field_encoding` (added after Run 70) but has NO equivalent validation for `field_presence`. The `featureTargets` map at cli.go:2033 allows `field_presence` on all fields, and no additional validation checks `field.GetLabel() == LABEL_REPEATED` to reject it.
- **C++ protoc**: `test.proto:6:18: Repeated fields can't specify field presence.`
- **Go protoc-go**: Silently accepts, encodes FeatureSet with EXPLICIT field_presence, produces valid descriptor.
- **Fix hint**: Add a `checkFieldPresenceField` function similar to `checkRepeatedFieldEncodingField` that rejects `field_presence` on repeated fields: if `field.GetLabel() == LABEL_REPEATED && field.GetOptions().GetFeatures().FieldPresence != nil`, emit error. Also add a `collectFieldPresenceErrors` function that walks all messages/nested types, and call it from the validation pass.
- **Also affects**: Map fields (which are syntactic sugar for repeated message entries) should also reject `field_presence`. Same validation is missing for `features.message_encoding` on non-message fields and `features.utf8_validation` on non-string fields. Also, `field_presence = LEGACY_REQUIRED` on a `oneof` field should probably be rejected.

### Run 75 — Edition `features.utf8_validation = NONE` accepted on non-string field (VICTORY)
- **Bug**: Go does NOT validate that `features.utf8_validation` can only be meaningfully set on string fields. When a non-string field (e.g., `int32`) uses `int32 value = 1 [features.utf8_validation = NONE];` in an edition 2023 proto file, C++ protoc rejects it with `Only string fields can specify utf8 validation.`, but Go accepts it silently and produces a valid descriptor.
- **Test**: `405_utf8_validation_nonstring` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's `collectFieldFeatureErrors` at cli.go:2342 checks that `enum_type` and `json_format` can't be set on fields, but has NO check for `utf8_validation` being on a non-string field. The comment at line 2345 says `utf8_validation` targets FIELD (allowed), but doesn't add the semantic check that the field must be `TYPE_STRING`. The `featureTargets` map at cli.go:2039 allows `utf8_validation` on fields, and no additional validation checks `field.GetType() == TYPE_STRING` to reject it on non-string fields.
- **C++ protoc**: `test.proto:6:9: Only string fields can specify utf8 validation.`
- **Go protoc-go**: Silently accepts, encodes FeatureSet with NONE utf8_validation, produces valid descriptor.
- **Fix hint**: Add a `checkUtf8ValidationField` function similar to `checkRepeatedFieldEncodingField` that rejects `utf8_validation` on non-string fields: if `field.GetType() != TYPE_STRING && field.GetOptions().GetFeatures().Utf8Validation != nil && field.GetOptions().GetFeatures().GetUtf8Validation() != descriptorpb.FeatureSet_VERIFY`, emit error `"Only string fields can specify utf8 validation."`. Add a `collectUtf8ValidationErrors` function that walks all messages/nested types, and call it from the validation pass.
- **Also affects**: Same validation should apply to bytes fields (they're not strings either), and to extension fields with `features.utf8_validation` set.

### Run 77 — Extensions can't specify field_presence (any value, not just LEGACY_REQUIRED) (VICTORY)
- **Bug**: Go's `checkRequiredExtensionEditionsField` only rejects `features.field_presence = LEGACY_REQUIRED` on extension fields. C++ protoc rejects ALL `field_presence` values on extensions (`IMPLICIT`, `EXPLICIT`, `LEGACY_REQUIRED`) with `"Extensions can't specify field presence."`. Go only checks for the `LEGACY_REQUIRED` case, allowing `IMPLICIT` and `EXPLICIT` through silently.
- **Test**: `408_ext_field_presence` — all 9 profiles fail.
- **Root cause**: `checkRequiredExtensionEditionsField` at cli.go:2627-2633 checks `field.GetOptions().GetFeatures().GetFieldPresence() == descriptorpb.FeatureSet_LEGACY_REQUIRED`. This only catches one specific enum value. The correct check is: if `FieldPresence` is set at all (non-nil), reject it — extensions can't specify ANY field_presence.
- **C++ protoc**: `test.proto:11:9: Extensions can't specify field presence.` (exit code 1).
- **Go protoc-go**: Silently accepts it, produces a descriptor set (exit code 0).
- **Fix hint**: Change the condition from checking `== LEGACY_REQUIRED` to just checking if `FieldPresence != nil`. Also rename the function from `checkRequiredExtensionEditionsField` to something like `checkExtensionFieldPresence` since it's not just about required anymore. The error message should also change from "Extensions can't be required." to "Extensions can't specify field presence." to match C++.

### Run 79 — Extending an enum type produces wrong error message (VICTORY)
- **Bug**: Go doesn't check that the extendee of an `extend` block is a message type. When extending an enum, C++ protoc correctly says `"Status" is not a message type` at the extend statement. Go skips this validation and instead falls through to the extension range check, producing a misleading error about extension numbers not being declared.
- **Test**: `410_extend_enum_type` — all 9 profiles fail.
- **Root cause**: In the descriptor validation path, Go doesn't check whether the resolved extendee type is actually a message (vs an enum). It proceeds to check if the extension number falls within the extendee's declared extension ranges, which fails for enums since they don't have extension ranges, giving a confusing error.
- **C++ protoc**: `test.proto:11:8: "Status" is not a message type.` (line of `extend Status`, column of `Status`).
- **Go protoc-go**: `test.proto:12:31: "test.Status" does not declare 100 as an extension number.` (line of the field, wrong column, FQN instead of local name).
- **Fix hint**: In the descriptor validation, before checking extension ranges, verify that the resolved extendee type is a message descriptor (not an enum). If it's an enum, emit `"<name>" is not a message type.` with the correct line/column from the extend statement.

### Run 80 — Go allows utf8_validation on bytes fields, C++ rejects it (VICTORY)
- **Bug**: Go's `checkUtf8ValidationField` at cli.go:2706-2712 returns early (allows) for both `TYPE_STRING` and `TYPE_BYTES` fields. But C++ protoc only allows `utf8_validation` on `TYPE_STRING` fields. Setting `features.utf8_validation = NONE` on a `bytes` field should produce an error, but Go silently accepts it and produces a descriptor.
- **Test**: `411_utf8_bytes_field` — all 9 profiles fail.
- **Root cause**: The condition `if field.GetType() == TYPE_STRING || field.GetType() == TYPE_BYTES { return }` at cli.go:2706-2707 is too broad. It should only allow `TYPE_STRING`.
- **C++ protoc**: `test.proto:5:9: Only string fields can specify utf8 validation.` (rejects with error).
- **Go protoc-go**: Produces valid descriptor (no error) — incorrectly allows utf8_validation on bytes field.
- **Fix hint**: Change the condition at cli.go:2706 from `field.GetType() == TYPE_STRING || field.GetType() == TYPE_BYTES` to just `field.GetType() == TYPE_STRING`.

### Run 81 — Import without string argument produces different error message (VICTORY)
- **Bug**: Go's `ExpectString()` returns a generic `"Expected string."` error, while C++ protoc uses a context-specific error message `"Expected a string naming the file to import."`. When `import;` is written (missing the file path string), the error messages differ.
- **Test**: `412_import_no_string` — all 9 profiles fail.
- **Root cause**: Go's tokenizer `ExpectString()` at tokenizer.go:592-598 always returns `fmt.Errorf("Expected string.")`. C++ protoc's parser calls `ConsumeString(&import_path, "Expected a string naming the file to import.")` which passes a custom error message to the tokenizer for each call site.
- **C++ protoc**: `test.proto:5:7: Expected a string naming the file to import.`
- **Go protoc-go**: `test.proto:5:7: Expected string.`
- **Fix hint**: Either (1) change `ExpectString()` to accept an optional custom error message parameter, or (2) change `parseImport` to not use `ExpectString()` and instead manually check the next token type with a custom error. The same pattern affects `parseSyntax`, `parseEdition`, and reserved name parsing — each should have context-specific error messages matching C++.
- **Also affects**: `syntax = ;` would produce `"Expected string."` instead of C++ protoc's syntax-specific error. `reserved ;` with a string expected would also differ. Each call site of `ExpectString()` could have a different C++-specific message.

### Run 82 — Group syntax in proto3 produces different error message (VICTORY)
- **Bug**: Go parser doesn't recognize the `group` keyword in proto3 at all — it treats `group` as a type name and fails with a generic parse error. C++ protoc recognizes the group syntax but rejects it with a clear semantic error "Groups are not supported in proto3 syntax." pointing at the `group` keyword.
- **Test**: `413_group_proto3` — all 9 profiles fail.
- **Root cause**: Go's `parseField()` in parser.go handles `group` keyword only in proto2 mode. When parsing proto3, `group` is not recognized as a keyword, so the parser sees `group Inner = 1 {` and treats `group` as a type name, `Inner` as the field name, then chokes at `{` expecting `;`.
- **C++ protoc**: `test.proto:6:3: Groups are not supported in proto3 syntax.`
- **Go protoc-go**: `test.proto:6:19: Expected ";".`
- **Fix hint**: In the proto3 parsing path, check for `group` keyword and emit a clear error like "Groups are not supported in proto3 syntax." at the correct position (column 3, where `group` starts) before bailing out. This matches C++ protoc's behavior of recognizing the syntax but rejecting it semantically.

### Run 83 — sfixed32 custom option overflow error message mismatch (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` for `TYPE_SFIXED32` rejects `0x80000000` with a generic `"invalid sfixed32 value"` error without line/column info. C++ protoc recognizes it as a range validation issue and reports `"Value out of range, -2147483648 to 2147483647, for int32 option"` with proper line/column location.
- **Test**: `414_sfixed32_overflow` — all 9 profiles fail.
- **Root cause**: Go's `checkIntRangeOption` only handles `TYPE_INT32`, `TYPE_SINT32`, and `TYPE_UINT32` — it does not check `TYPE_SFIXED32` or `TYPE_FIXED32`. The overflow is caught later in `encodeCustomOptionValue` when `strconv.ParseInt` fails, producing a different error format without source location.
- **C++ protoc**: `test.proto:11:19: Value out of range, -2147483648 to 2147483647, for int32 option "sfixedoverflow.my_val".`
- **Go protoc-go**: `test.proto: error encoding custom option: invalid sfixed32 value: 0x80000000`
- **Fix hint**: Add `TYPE_SFIXED32` to the int32 range check case in `checkIntRangeOption`, and add `TYPE_FIXED32` to the uint32 range check case. This would catch the overflow early with proper line/column info and the standard range error message.
- **Also affects**: `TYPE_FIXED32` (unsigned 32-bit) likely has the same issue — values above `0xFFFFFFFF` would get a different error message format.
