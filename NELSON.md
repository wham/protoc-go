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

### Run 91 — Octal invalid digit missing warning (VICTORY)
- **Bug**: Go tokenizer does not emit a warning/error when a numeric literal starting with `0` contains non-octal digits (8 or 9). C++ protoc produces `"Numbers starting with leading zero must be in octal."` as a separate error in addition to "Integer out of range." Go only produces the latter.
- **Test**: `423_octal_invalid_digit` — all 9 profiles fail (error message mismatch).
- **Root cause**: Go's tokenizer treats `09` as a decimal number and just passes it through. When the value is then validated as an integer literal (which expects octal for 0-prefixed numbers), it produces "Integer out of range" but never checks or warns that non-octal digits are present. C++ protoc's tokenizer explicitly checks for digits 8-9 in a 0-prefixed literal and reports a specific error.
- **C++ protoc**: `test.proto:6:38: Numbers starting with leading zero must be in octal.\ntest.proto:6:37: Integer out of range.` (two errors, different columns)
- **Go protoc-go**: `test.proto:6:37: Integer out of range.` (one error, missing the octal warning)
- **Fix hint**: In the tokenizer's integer parsing code, when a number starts with `0` (not `0x`/`0X`) and contains digits 8 or 9, emit an error `"Numbers starting with leading zero must be in octal."` pointing at the invalid digit position. Then continue to also produce the "Integer out of range" error for consistency with C++.

### Run 92 — Custom option `targets` restriction not validated (VICTORY)
- **Bug**: Go does NOT validate the `targets` restriction on custom option extensions. When a custom option is declared with `targets = TARGET_TYPE_MESSAGE` (meaning it can only be used on messages), but is used on a field, C++ protoc rejects it with `"Option X cannot be set on an entity of type 'field'."` Go accepts it silently and produces a valid descriptor.
- **Test**: `424_option_target_type` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's CLI code never calls `GetTargets()` on custom option extension declarations. The `targets` option is parsed correctly by the parser (parser.go:5987), but no validation function checks whether the extension's targets list includes the entity type it's being used on. The validation only exists for edition features targets (cli.go:2159+), not for custom option targets.
- **C++ protoc**: `test.proto: Option targtest.msg_only_opt cannot be set on an entity of type 'field'.` (exit code 1).
- **Go protoc-go**: Silently accepts the file and produces a valid descriptor (exit code 0).
- **Fix hint**: After resolving custom options for each entity type, check if the extension's `FieldOptions.targets` list is non-empty and does not include the appropriate `TARGET_TYPE_*` for the entity. For field options, check `TARGET_TYPE_FIELD`; for message options, `TARGET_TYPE_MESSAGE`; for file options, `TARGET_TYPE_FILE`; etc. Emit error `"Option <fqn> cannot be set on an entity of type '<type>'."` matching C++ format.
- **Also affects**: All 9 entity types (file, message, field, enum, enum_value, service, method, oneof, extension_range) — none validate targets. Also affects the `retention` option (which was Run 88) — both `targets` and `retention` are custom option metadata that Go doesn't properly enforce.

### Run 93 — Edition `features.field_presence = LEGACY_REQUIRED` accepted at file level (VICTORY)
- **Bug**: Go does NOT validate that `features.field_presence = LEGACY_REQUIRED` cannot be specified at the file level (as a default for all fields). When a file has `option features.field_presence = LEGACY_REQUIRED;` in an edition 2023 proto, C++ protoc rejects it with `Required presence can't be specified by default.`, but Go accepts it silently and produces a valid descriptor.
- **Test**: `425_file_level_legacy_required` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's edition features validation has checks for `LEGACY_REQUIRED` on extension fields (Run 74) and oneof members (Run 77), but has NO check for file-level `features.field_presence = LEGACY_REQUIRED`. C++ protoc's feature validation rejects `LEGACY_REQUIRED` as a default because it would make all fields required by default, which is not allowed. Go's `validateFeatureTargets` and other validation functions never check if `LEGACY_REQUIRED` is set at the file level.
- **C++ protoc**: `test.proto:1:1: Required presence can't be specified by default.` (exit code 1).
- **Go protoc-go**: Silently accepts, encodes FeatureSet with LEGACY_REQUIRED field_presence at file level, produces valid descriptor (exit code 0).
- **Fix hint**: Add a validation check: after parsing file options for edition proto files, if `fd.GetOptions().GetFeatures().GetFieldPresence() == LEGACY_REQUIRED`, emit error `"Required presence can't be specified by default."` at line 1 col 1. This check should be in the file-level validation pass, not the field-level one.
- **Also affects**: Same validation may be missing for `LEGACY_REQUIRED` at the message level (`option features.field_presence = LEGACY_REQUIRED;` inside a message).

### Run 96 — Synthetic oneof name conflict not handled (VICTORY)
- **Bug**: Go does not handle the case where a proto3 `optional` field's synthetic oneof name (`_<fieldname>`) conflicts with an existing real oneof name. C++ protoc detects the conflict and renames the synthetic oneof by prepending `X` (e.g., `_foo` → `X_foo`). Go just creates the duplicate name and then fails with a "already defined" error during validation.
- **Test**: `426_synthetic_oneof_conflict` — all 9 profiles fail (C++ succeeds, Go errors).
- **Root cause**: When a message has `oneof _foo { ... }` AND `optional string foo = 3;`, Go generates synthetic oneof `_foo` (= `"_" + "foo"`) which collides with the real oneof. The name is generated at parser.go:742 (`name: "_" + field.GetName()`) without checking for conflicts. C++ protoc's `DescriptorBuilder::BuildFieldOrExtension()` checks if `_<name>` is taken and if so, tries `X_<name>`, then `XX_<name>`, etc. until a unique name is found.
- **C++ protoc**: Produces valid descriptor with two oneofs: `_foo` (real) and `X_foo` (synthetic, renamed). Descriptor is 107 bytes.
- **Go protoc-go**: Fails with `test.proto: "_foo" is already defined in "synconflict.Msg".` (exit code 1).
- **Fix hint**: After generating the synthetic oneof name `"_" + fieldName`, check if that name conflicts with any existing oneof or field name in the message. If it does, prepend `X` repeatedly until a unique name is found: `X_foo`, `XX_foo`, etc. This should be done in the synthetic oneof creation loop at parser.go:741-744 (and the equivalent for nested messages at parser.go:2407-2410).
- **Also affects**: Same bug exists in editions `optional` field synthetic oneofs (parser.go:2407-2410) and any other place where synthetic oneof names are generated.

### Run 105 — Deep sub-field option merge not recursive (VICTORY)
- **Bug**: Go's `mergeUnknownExtensions` only merges wire entries at the top-level extension field number. It does NOT recursively merge nested message fields within the merged payload. When multiple sub-field option assignments share intermediate path segments (e.g., `option (cfg).inner.value = "hello"; option (cfg).inner.num = 42;`), C++ protoc produces a single merged `inner` entry containing both `value` and `num`. Go produces TWO separate `inner` entries — one with `value`, one with `num`.
- **Test**: `436_deep_subfield_merge` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: Each sub-field option assignment produces a complete wire encoding from the leaf value up through all parent messages. `option (cfg).inner.value = "hello"` produces `inner { value: "hello" }` and `option (cfg).inner.num = 42` produces `inner { num: 42 }`. The merge function concatenates these at the `cfg` level (field 50001), but WITHIN the merged payload, both `inner` entries (field 1) remain separate. C++ pre-merges at every nesting level, producing a single `inner { value: "hello" num: 42 }` entry.
- **C++ protoc**: Produces 326-byte descriptor with single merged `inner` entry: `tag(1,2) + len(9) + value + num`.
- **Go protoc-go**: Produces 328-byte descriptor with two separate `inner` entries: `tag(1,2) + len(7) + value` + `tag(1,2) + len(2) + num`. 2 bytes of extra tag+length overhead.
- **Fix hint**: `mergeUnknownExtensions` (or a new helper) needs to recursively merge BytesType entries with the same field number within each level of the message wire format. After concatenating payloads at the extension level, parse the resulting bytes as wire-format entries, group by field number, and for BytesType entries with the same non-repeated message field number, recursively merge their contents. This requires knowing which fields are message-typed (non-repeated) vs scalar/repeated — use `msgFieldMap` or the extension's type descriptor.
- **Also affects**: Any sub-field option path with 2+ levels of nesting where intermediate segments are shared. The deeper the nesting and the more shared segments, the larger the binary difference. Also affects all 9 option entity types (file, message, field, enum, service, method, oneof, enum_value, ext_range) wherever sub-field option merging occurs.

### Run 106 — MSVS error format not applied to parse errors (VICTORY)
- **Bug**: Go's `--error_format=msvs` flag does not format parse errors from the parser package. When `parser.ParseFile` returns a `*parser.MultiError`, the error is returned directly from `parseRecursive` at cli.go:842 without going through `formatErrorsMSVS`. Only validation errors collected in `collectErrors` are formatted. Parse errors bypass MSVS formatting entirely.
- **Test**: CLI test `cli@error_format_msvs_parse` — 1 test fails (stderr mismatch).
- **Root cause**: `parseRecursive` at cli.go:842-844 returns `MultiError` directly: `if me, ok := err.(*parser.MultiError); ok { return false, me }`. This bypasses the `formatErrorsMSVS(collectErrors, srcTree)` call at line 477. The existing `error_format_msvs` test (testdata/257_msvs_error) only triggers a validation error (duplicate field name), which IS formatted because it goes through `collectErrors`. Parse errors from the tokenizer/parser take a different return path.
- **C++ protoc**: `testdata/437_msvs_parse_error/test.proto(6) : error in column=1: Expected ";".` (MSVS format, full path).
- **Go protoc-go**: `test.proto:6:1: Expected ";".` (gcc format, short filename, no MSVS formatting applied).
- **Fix hint**: In `parseRecursive`, when a `MultiError` is returned and `cfg.errorFormat == "msvs"`, apply MSVS formatting to each error string before returning. Or, collect parse errors into `collectErrors` instead of returning them directly, so they flow through the existing MSVS formatting path. Also, the filename in parse errors uses the virtual (relative) name, not the disk path — MSVS format in C++ includes the full disk path via `VirtualFileToDiskFile()`.
- **Also affects**: Any proto file with a syntax/parse error (missing semicolons, unrecognized tokens, malformed strings, etc.) will produce gcc-format errors even when `--error_format=msvs` is specified. Only post-parse validation errors get MSVS formatting.

### Run 124 — Decode mode doesn't unpack packed repeated fields (VICTORY)
- **Bug**: Go's `printKnownField` in `--decode` mode does NOT handle packed encoding for repeated scalar fields. When a proto3 `repeated int32` field is encoded in packed format (wire type 2 / BytesType containing multiple varints), Go prints a single `values: 0` instead of unpacking and printing each value separately. C++ protoc correctly unpacks packed repeated fields and prints each value on its own line.
- **Test**: Decode test `decode@packed_repeated` — stdout mismatch (1 test fails). Proto in `testdata/453_decode_packed/test.proto`. Hex data: `0a0301020312026f6b` (field 1 packed int32 [1,2,3], field 2 string "ok").
- **Root cause**: `printKnownField` at cli.go:9025 for TYPE_INT32 does `fmt.Fprintf(w, "%s%s: %d\n", prefix, name, int32(e.varint))` without checking if `e.wtype == BytesType`. When a packed repeated field arrives as BytesType, the entry has `e.varint = 0` (default uint64) because the value was stored in `e.bytes`, not `e.varint`. The function never looks at `e.bytes` for integer types. Same bug exists for ALL packable types: TYPE_INT64, TYPE_UINT32, TYPE_UINT64, TYPE_SINT32, TYPE_SINT64, TYPE_BOOL, TYPE_ENUM, TYPE_FIXED32, TYPE_FIXED64, TYPE_SFIXED32, TYPE_SFIXED64, TYPE_FLOAT, TYPE_DOUBLE.
- **C++ protoc**: `values: 1\nvalues: 2\nvalues: 3\nlabel: "ok"` (three repeated entries, correctly unpacked).
- **Go protoc-go**: `values: 0\nlabel: "ok"` (single entry, wrong value, packed encoding not handled).
- **Fix hint**: In `printKnownField`, for each packable type (int32, int64, uint32, uint64, sint32, sint64, bool, enum, fixed32, fixed64, sfixed32, sfixed64, float, double), add a check: if `e.wtype == protowire.BytesType`, iterate over `e.bytes` consuming individual values (varints for varint types, fixed32/fixed64 for fixed types) and print each one. For example, for TYPE_INT32 with BytesType: `pos := 0; for pos < len(e.bytes) { v, n := protowire.ConsumeVarint(e.bytes[pos:]); pos += n; fmt.Fprintf(w, "%s%s: %d\n", prefix, name, int32(v)) }`.
- **Also affects**: ALL packable scalar types in decode mode. Proto3 uses packed encoding by default for repeated scalar fields, so this bug affects essentially ALL proto3 decode operations with repeated numeric/bool/enum fields.

### Run 125 — Decode mode treats wrong-wire-type field as known instead of unknown (VICTORY)
- **Bug**: Go's `printTextProto` in `--decode` mode always routes a field to `knownEntries` if the field number is found in the schema, regardless of wire type mismatch. When `int32 value = 1` (expects varint wire type 0) receives data with fixed32 wire type 5, Go treats it as known and prints `value: 0` (reading from the empty `e.varint` field). C++ protoc recognizes the wire type mismatch, puts the field in the unknown field set, and prints it as `1: 0x0000002a`.
- **Test**: Decode test `decode@wrong_wire` — stdout mismatch (1 test fails). Proto in `testdata/455_decode_wrong_wire/test.proto`. Hex data: `0d2a00000012026f6b` (field 1 as fixed32=42, field 2 string "ok").
- **Root cause**: After parsing wire data at cli.go:9006-9052, the code does `if fd != nil { knownEntries = append(knownEntries, entry) }` without checking if `e.wtype` matches the expected wire type for `fd.GetType()`. Then `printKnownField` for TYPE_INT32 reads `e.varint` (which is 0 since the data was in `e.fixed32`), printing the wrong value. C++ detects the wire type mismatch during parsing and places the field in `UnknownFieldSet` instead.
- **C++ protoc**: `label: "ok"\n1: 0x0000002a` (field 1 as unknown fixed32, correct value).
- **Go protoc-go**: `value: 0\nlabel: "ok"` (field 1 as known int32, wrong value 0).
- **Fix hint**: Before adding to `knownEntries`, check if the wire type is compatible with the field type. For varint types (int32, int64, uint32, uint64, sint32, sint64, bool, enum): expect VarintType. For fixed64 types (fixed64, sfixed64, double): expect Fixed64Type. For fixed32 types (fixed32, sfixed32, float): expect Fixed32Type. For string/bytes/message: expect BytesType. For group: expect StartGroupType. If mismatch (and not a packed repeated field), route to `unknownEntries` instead.
- **Also affects**: ALL field types in decode mode. Any wire type mismatch will either print wrong values (for types without wire type checks) or silently drop fields (for TYPE_DOUBLE/TYPE_FLOAT which have `if e.wtype ==` guards). This is a fundamental correctness bug in the decoder.

### Run 129 — Decode mode prints all oneof fields instead of only the last one (VICTORY)
- **Bug**: Go's `--decode` mode prints ALL fields from a oneof, even when multiple oneof members are present in the binary data. C++ protoc deserializes into a `DynamicMessage` which applies oneof semantics (last field wins), then prints only the final value. Go's decode mode parses the wire format directly and prints every field it encounters, without applying oneof deduplication.
- **Test**: Decode test `decode@oneof_dedup` — stdout mismatch (1 test fails). Proto in `testdata/460_decode_oneof_dedup/test.proto`. Hex data: `0a0568656c6c6f102a1a026f6b` (field 1 string "hello", field 2 varint 42, field 3 string "ok" — fields 1 and 2 are in the same oneof).
- **Root cause**: `printTextProto()` in cli.go processes all wire entries sequentially and adds them to `knownEntries` if a field descriptor is found. It never checks if a field belongs to a oneof, and never removes earlier oneof members when a later one is encountered. C++ protoc's `DynamicMessage::MergeFrom` handles this automatically — when setting a oneof field, it clears any previously set member of the same oneof.
- **C++ protoc**: `id: 42\nlabel: "ok"` (only the LAST oneof member `id` is shown; earlier `name` is discarded).
- **Go protoc-go**: `name: "hello"\nid: 42\nlabel: "ok"` (BOTH oneof members shown — incorrect protobuf semantics).
- **Fix hint**: Before printing, post-process `knownEntries` to apply oneof deduplication. For each oneof in the message descriptor, track which field numbers belong to which oneof. Then scan `knownEntries` in reverse order; for each oneof, keep only the LAST entry and remove earlier entries from the same oneof. Need to use `msg.GetOneofDecl()` and check each field's `GetOneofIndex()` to build the oneof membership map. Or, build a proper `DynamicMessage`-like structure during decode.
- **Also affects**: ANY proto2 or proto3 message with a oneof where the binary data contains multiple members from the same oneof. This is common in real-world data when messages are merged or when streaming updates append new field values. Also affects nested messages with oneofs decoded recursively.

### Run 152 — `--retain_options` flag silently ignored by Go (VICTORY)
- **Bug**: Go's `parseArgs()` silently skips the `--retain_options` flag (line 1355: just `continue`). The flag is NOT stored in any config field — it's completely ignored. C++ protoc uses this flag to prevent stripping source-retention options from `--descriptor_set_out` output. When a proto file defines extensions with `retention = RETENTION_SOURCE`, `--retain_options` should preserve those options in the descriptor output instead of stripping them.
- **Test**: `479_retain_options` + new `descriptor_set_retain` profile — fails on `descriptor_set_retain` profile. Also causes 18 other existing test cases with retention-related options to fail on the new profile.
- **Root cause**: `parseArgs()` at cli.go:1355-1357 just does `continue` when encountering `--retain_options`. The flag value is never stored. `stripSourceRetention()` at line 747 always strips source-retention options unconditionally, regardless of whether `--retain_options` was passed.
- **C++ protoc**: With `--retain_options --descriptor_set_out=...`, produces 255-byte descriptor preserving the `(source_label) = "important"` field option.
- **Go protoc-go**: With same flags, produces 242-byte descriptor that strips `(source_label)` — identical to output WITHOUT `--retain_options`. 13-byte difference.
- **Fix hint**: (1) Add a `retainOptions bool` field to the CLI config struct. (2) In `parseArgs()`, set it when `--retain_options` is seen. (3) In `stripSourceRetention()` (or its caller), check the flag and skip stripping when true.

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

### Run 84 — Negative extension range start produces different error message (VICTORY)
- **Bug**: Go parser produces `"Expected integer."` when encountering a negative number in `extensions -1 to 10;`, while C++ protoc produces `"Expected field number range."`. The `-` sign before the number is not a valid start for an extension range, and each compiler reports a different error about it.
- **Test**: `415_neg_ext_range` — all 9 profiles fail.
- **Root cause**: Go's extension range parsing sees `-` and calls a generic integer parsing function that reports "Expected integer" when it can't parse the negative sign as part of a valid integer in this context. C++ protoc has a more specific check that knows it's parsing an extension range and reports "Expected field number range" — a more helpful context-aware error.
- **C++ protoc**: `test.proto:6:14: Expected field number range.`
- **Go protoc-go**: `test.proto:6:14: Expected integer.`
- **Fix hint**: In the extension range parsing code (parser.go), when encountering `-` as the first token where a range start is expected, emit `"Expected field number range."` instead of falling through to the generic integer parser.

### Run 85 — Go tokenizer silently accepts nested `/*` inside block comments (VICTORY)
- **Bug**: Go's block comment parser does NOT detect `/*` inside a block comment. C++ protoc scans for `/*` within block comments and emits error `"/*" inside block comment.  Block comments cannot be nested.` Go silently accepts `/* outer /* nested */` and treats the whole thing as a valid comment — the inner `/*` is just ignored.
- **Test**: `416_nested_block_comment` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's tokenizer `readBlockComment()` in `io/tokenizer/tokenizer.go` only looks for `*/` to close the comment. It does NOT check if `/*` appears inside the comment body. C++ protoc's `Tokenizer::NextChar()` in `io/tokenizer.cc` specifically checks for `/*` while scanning block comments and calls `AddError()` when found.
- **C++ protoc**: `test.proto:5:11: "/*" inside block comment.  Block comments cannot be nested.` (exit code 1).
- **Go protoc-go**: Silently accepts the file and produces a valid descriptor (exit code 0).
- **Fix hint**: In the block comment scanning loop, while looking for `*/`, also check for `/*` and emit an error like `"/*" inside block comment.  Block comments cannot be nested.` with the correct line/column pointing at the inner `/*`.

### Run 86 — MessageSet scalar extension not validated (VICTORY)
- **Bug**: Go does not validate that extensions of messages with `option message_set_wire_format = true` must be optional messages. When a scalar extension (e.g., `optional int32`) is defined for a MessageSet type, Go silently accepts it and produces a descriptor. C++ protoc correctly rejects it with a clear error message.
- **Test**: `417_msgset_scalar_ext` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's `validateMessageSetFields()` in `cli.go:3479` only checks that MessageSet messages don't have regular fields. There is NO validation that extensions TO MessageSet types must be optional messages (`TYPE_MESSAGE` with `LABEL_OPTIONAL`). C++ protoc's `descriptor.cc` has this validation in `DescriptorBuilder::CrossLinkField()`.
- **C++ protoc**: `test.proto:11:12: Extensions of MessageSets must be optional messages.` (exit code 1).
- **Go protoc-go**: Silently accepts the file and produces a valid descriptor (exit code 0).
- **Fix hint**: Add a new validation function (e.g., `validateMessageSetExtensions`) that iterates all extensions, checks if the extendee has `message_set_wire_format = true`, and if so, validates that the extension field has `type == TYPE_MESSAGE` and `label == LABEL_OPTIONAL`. Error message should be: `"Extensions of MessageSets must be optional messages."` with line/col pointing at the extension field name.

### Run 87 — Aggregate bool field with integer value `2` produces different error message (VICTORY)
- **Bug**: When an aggregate option has `enabled: 2` where `enabled` is a `bool` field, both C++ protoc and Go protoc-go reject it — but with different error messages. C++ says "Integer out of range (2)" while Go says "Invalid value for boolean field "enabled". Value: "2"."
- **Test**: `418_agg_bool_int_error` — all 9 profiles fail (error message mismatch).
- **Root cause**: C++ protoc's text format parser treats bool as an integer type and validates range (only 0 and 1 are valid), producing "Integer out of range (2)". Go's `encodeAggregateFields` in `cli.go` has a separate bool-specific check that produces a more descriptive but non-matching error message.
- **C++ protoc**: `test.proto:15:16: Error while parsing option value for "cfg": Integer out of range (2)`
- **Go protoc-go**: `test.proto:15:16: Error while parsing option value for "cfg": Invalid value for boolean field "enabled". Value: "2".`
- **Fix hint**: In `encodeAggregateFields`, when a bool field receives an out-of-range integer, match C++ error: `"Integer out of range (%s)"` instead of `"Invalid value for boolean field..."`.

### Run 88 — Source-retention options not stripped from descriptor (VICTORY)
- **Bug**: Go protoc does NOT strip source-retention options from the descriptor. When a field extension is declared with `[retention = RETENTION_SOURCE]`, C++ protoc removes the option value from the runtime descriptor (the option is only preserved in source code info). Go keeps it in the descriptor, producing extra bytes.
- **Test**: `420_retention_source` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: Go's CLI code does not check the `retention` field on extension declarations. When encoding custom options into the descriptor, it includes ALL option values regardless of retention setting. C++ protoc checks `retention = RETENTION_SOURCE` and strips those option values from the serialized FileDescriptorProto before outputting descriptor sets or sending to plugins.
- **C++ protoc**: Produces 148-byte descriptor without the `(debug_info)` option value in FieldOptions (stripped due to `RETENTION_SOURCE`).
- **Go protoc-go**: Produces 158-byte descriptor with the `(debug_info)` option value still in FieldOptions — 10 bytes larger.
- **Fix hint**: After encoding custom options into the descriptor, iterate all fields/messages/etc and check if any extension has `options.retention == RETENTION_SOURCE`. If so, remove those extension entries from the unknown fields. Or, during `resolveCustomFieldOptions`, skip encoding options whose extension declaration has `retention = RETENTION_SOURCE` unless generating source-retained output.
- **Also affects**: Likely affects ALL custom option types (file, message, enum, service, method, oneof, enum_value, ext_range) — any custom option with `retention = RETENTION_SOURCE` will be incorrectly retained in the descriptor.

### Run 89 — decode_raw cEscapeForDecode missing single quote escape (VICTORY)
- **Bug**: Go's `cEscapeForDecode` function in `cli.go:7868` does NOT escape the single quote character `'` (0x27). C++ protoc uses `absl::CEscape` which escapes `'` as `\'`. When `--decode_raw` decodes a BytesType field containing a `'`, the output differs.
- **Test**: Added `decode_raw_single_quote` to `STDIN_TESTS` in `scripts/test` with hex data `0a0127` (field 1, string `'`). 1 profile fails (`stdin@decode_raw_single_quote`).
- **Root cause**: `cEscapeForDecode()` at cli.go:7868 has cases for `\n`, `\r`, `\t`, `"`, `\\` but is missing `case '\''`. The `'` character (0x27) is in the printable ASCII range (0x20-0x7E), so it falls through to the default branch and is printed unescaped. Meanwhile, the parser's `cEscape()` function at parser.go:6371 DOES have the `case '\''` and correctly escapes it.
- **C++ protoc**: `1: "\'"` (single quote escaped with backslash)
- **Go protoc-go**: `1: "'"` (single quote unescaped)
- **Fix hint**: Add `case '\'': sb.WriteString(`\'`)` to `cEscapeForDecode()` between the `"` and `\\` cases. This matches what the parser's `cEscape` already does.

### Run 90 — Positive sign `+` in float/double default value produces different error (VICTORY)
- **Bug**: Go parser does not handle the `+` sign before default values for float/double fields. When `[default = +inf]` is used, Go consumes `+` as the value token (a symbol), then treats `inf` as unexpected. C++ protoc recognizes `+` at the position and produces a clear "Expected number" error.
- **Test**: `422_positive_float_default` — all 9 profiles fail.
- **Root cause**: `parseFieldOptions()` at parser.go:5737 only checks for `-` sign before default values (`if optName == "default" && p.tok.Peek().Value == "-"`), not `+`. When `+` is encountered, it becomes `valTok` with `valTok.Value = "+"`, then the float validation code at line 5792-5798 sees `valTok.Type == TokenIdent` with value `"inf"` is never reached (because `valTok` is `+` not `inf`). Go then fails at subsequent parsing.
- **C++ protoc**: `test.proto:6:39: Expected number.` — error at column of `+`
- **Go protoc-go**: `test.proto:6:40: Expected ";".\ntest.proto:6:40: Expected "]".` — error at column of `inf`, wrong error message
- **Fix hint**: Add `+` handling alongside `-` handling: `if optName == "default" && (p.tok.Peek().Value == "-" || p.tok.Peek().Value == "+")`. When `+`, just consume it and skip (positive sign is no-op for the value). Or, replicate C++ behavior and reject `+` with "Expected number" error.
- **Hex escape side note**: Also tested `\x4142` hex escape (testdata/421_hex_escape_length) — both C++ and Go limit `\x` to 2 hex digits, so no bug there. Test removed.

### Run 94 — `--decode` mode not implemented, causes "Missing output directives" (VICTORY)
- **Bug**: Go protoc-go completely ignores the `--decode=MESSAGE_TYPE` flag. The `parseArgs()` function at cli.go:1021 has `if strings.HasPrefix(arg, "--encode=") || strings.HasPrefix(arg, "--decode=") { continue }` — it silently skips these flags. When a valid proto file is provided with `--decode`, Go falls through to the "Missing output directives" validation error instead of entering decode mode.
- **Test**: CLI test `cli@decode_with_file` — added to `CLI_TESTS` in `scripts/test`. Uses `--decode=basic.Person -I testdata/01_basic_message testdata/01_basic_message/basic.proto` with stdin data `x`.
- **Root cause**: `--decode` and `--encode` flags are deliberately skipped in `parseArgs()` with a `continue` statement. No decode/encode mode is implemented. The `cfg` struct has no fields for decode/encode mode. The validation at line 430 (`if len(cfg.plugins) == 0 && cfg.descriptorSetOut == "" && !cfg.printFreeFieldNumbers`) doesn't account for decode/encode being valid "output" modes.
- **C++ protoc**: Enters decode mode, reads "x\n" (0x78 0x0a) from stdin, decodes as valid protobuf (field 15, varint 10), prints "15: 10" to stdout, exits 0.
- **Go protoc-go**: Ignores `--decode`, processes proto file, hits "Missing output directives" check, prints error to stderr, exits 1.
- **Discrepancy**: Exit code mismatch (C++ 0 vs Go 1) and stderr mismatch (C++ empty vs Go "Missing output directives.").
- **Fix hint**: Implement `--decode` mode: (1) add `decodeType string` field to config, (2) parse `--decode=TYPE` in `parseArgs`, (3) skip "Missing output directives" check when decode/encode mode is active, (4) after building descriptor pool, read stdin, decode binary proto using the specified message type, print text format to stdout.
- **Also affects**: `--encode=MESSAGE_TYPE` is similarly unimplemented (same `continue` skip).

### Run 95 — decode_raw fails on group wire types (VICTORY)
- **Bug**: Go's `--decode_raw` mode fails to parse protobuf data containing group wire types (wire type 3 = start group, wire type 4 = end group). C++ protoc correctly decodes groups and prints them with `{ }` notation. Go fails with "Failed to parse input." exit code 1.
- **Test**: Added `decode_raw_group` to `STDIN_TESTS` in `scripts/test` with hex data `0b0a0568656c6c6f0c` (field 1 start group, field 1 string "hello", field 1 end group). 1 profile fails (`stdin@decode_raw_group`).
- **Root cause**: `validateRawProto()` at cli.go:7870-7880 has a `StartGroupType` case that doesn't actually validate group contents. After consuming the start tag, it checks the first inner tag — if it's NOT the matching end group tag, it immediately returns `"group validation not implemented"` instead of actually parsing/skipping the inner fields. The validation failure at line 413 (`if err := validateRawProto(data); err != nil`) blocks `decodeRawProto()` (which DOES handle groups correctly at line 7819-7835) from ever being called.
- **C++ protoc**: Decodes group correctly, prints `1 {\n  1: "hello"\n}` (exit 0).
- **Go protoc-go**: `Failed to parse input.` (exit 1).
- **Fix hint**: In `validateRawProto`, the `StartGroupType` case needs to recursively validate inner fields (like `decodeRawField` does in `decodeRawProto`). Instead of `return fmt.Errorf("group validation not implemented")`, it should consume each inner field by calling itself recursively or by using a helper that skips fields based on wire type, until it finds the matching `EndGroupType` tag.
- **Also affects**: Any protobuf binary data containing group wire types (proto2 groups, MessageSet wire format) will fail `--decode_raw`.

### Run 96 — Non-ASCII codepoint warnings missing from Go tokenizer (VICTORY)
- **Bug**: C++ protoc's tokenizer emits "Interpreting non ascii codepoint NNN." warning messages when it encounters non-ASCII bytes in the input (outside of string literals). Go's tokenizer silently skips or rejects non-ASCII bytes without emitting these diagnostic messages. When a .proto file contains non-ASCII characters in identifiers (e.g., `héllo` with UTF-8 é = bytes 0xC3 0xA9), C++ produces warning lines for each non-ASCII byte before the parse error, while Go produces only the parse error.
- **Test**: `427_non_ascii_ident` — all 9 profiles fail.
- **Root cause**: C++ `io/tokenizer.cc` has a `TryConsumeOne` path that detects non-ASCII bytes and calls `AddError("Interpreting non ascii codepoint %d.", c)` before continuing. Go's `io/tokenizer/tokenizer.go` has no equivalent warning path — it simply treats non-ASCII bytes as unexpected characters and produces a generic parse error.
- **C++ protoc**: `test.proto:2:10: Interpreting non ascii codepoint 195.` + `test.proto:2:10: Expected ";"` + `test.proto:2:11: Interpreting non ascii codepoint 169.`
- **Go protoc-go**: `test.proto:2:10: Expected ";"` (missing the two codepoint warnings).
- **Fix hint**: In Go's tokenizer, when encountering a byte with value >= 128, emit a warning/error `Interpreting non ascii codepoint %d.` (using the raw byte value, not the Unicode codepoint) before producing the parse error. This matches C++ behavior which reports each individual byte of multi-byte UTF-8 sequences.

### Run 97 — Edition `json_format = LEGACY_BEST_EFFORT` doesn't downgrade JSON name conflict to warning (VICTORY)
- **Bug**: Go does NOT downgrade JSON name conflicts to warnings when `features.json_format = LEGACY_BEST_EFFORT` is set at the file level in an edition 2023 proto. When a message has two fields whose default JSON names collide (`foo_bar` → `fooBar` and `fooBar` → `fooBar`), C++ protoc recognizes the `LEGACY_BEST_EFFORT` feature and emits a WARNING (prefixed with "warning:") while still producing the descriptor (exit code 0). Go ignores the feature and treats the conflict as a hard error (exit code 1), refusing to produce the descriptor.
- **Test**: `428_json_format_legacy` — all 9 profiles fail (C++ succeeds, Go errors).
- **Root cause**: Go's `validateJsonNameConflicts()` in `cli.go` always treats JSON name conflicts as errors. It does not check the file's `features.json_format` value. When `LEGACY_BEST_EFFORT` is set, C++ protoc's `ValidateJsonNameConflicts` emits a `WARNING` instead of an `ERROR` and continues compilation. Go has no warning mechanism and no check for this feature.
- **C++ protoc**: `test.proto:9:9: warning: The default JSON name of field "fooBar" ("fooBar") conflicts with the default JSON name of field "foo_bar".` (exit 0, descriptor produced).
- **Go protoc-go**: `test.proto:9:9: The default JSON name of field "fooBar" ("fooBar") conflicts with the default JSON name of field "foo_bar".` (exit 1, no descriptor).
- **Fix hint**: In `validateJsonNameConflicts()`, check if `fd.GetSyntax() == "editions"` and `fd.GetOptions().GetFeatures().GetJsonFormat() == descriptorpb.FeatureSet_LEGACY_BEST_EFFORT`. If true, either skip JSON name conflict detection entirely, or change the errors to warnings (print to stderr with "warning:" prefix but don't add to the error list). The "warning:" prefix on the message AND the successful exit code are both needed to match C++ behavior.
- **Also affects**: Message-level `features.json_format = LEGACY_BEST_EFFORT` should also downgrade conflicts within that specific message (not file-wide). Nested message inheritance of `json_format` feature should also be respected.

### Run 98 — Edition open enum without zero first value not rejected (VICTORY)
- **Bug**: Go does NOT validate that OPEN enums in edition 2023 must have their first value equal to zero. In edition 2023, the default `features.enum_type` is `OPEN`, and OPEN enums require the first value to be 0 (same as proto3). When an edition file has `enum Priority { HIGH = 1; LOW = 2; }` (first value is 1, not 0), C++ protoc rejects it with "The first enum value must be zero for open enums." Go silently accepts it and produces a valid descriptor.
- **Test**: `429_edition_open_enum_zero` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `collectProto3EnumZeroErrors` is only called from `validateProto3()` at cli.go:2412 and 2897, which only runs for `fd.GetSyntax() == "proto3"` files (checked at line 2407). There is NO equivalent validation for editions files. Editions files with default `features.enum_type = OPEN` should have the same first-value-zero requirement, but no validation code checks this.
- **C++ protoc**: `test.proto:6:10: The first enum value must be zero for open enums.` (exit code 1).
- **Go protoc-go**: Silently accepts, produces valid descriptor (exit code 0).
- **Fix hint**: Add a new validation function (e.g., `validateEditionsOpenEnumZero`) that: (1) iterates edition files, (2) for each enum (top-level and nested), checks if the effective `enum_type` is OPEN (check enum-level features override, then file-level features, then default for edition 2023 which is OPEN), (3) if OPEN and first value != 0, emit the same error. Call it from the Phase 2 validation block alongside the other editions validations.
- **Also affects**: Same validation is missing for nested enums inside messages in editions files. Also, enums explicitly set to `features.enum_type = OPEN` (redundant with default) should also be validated. Enums set to `features.enum_type = CLOSED` should be exempt.

### Run 99 — Edition reserved names must be identifiers not string literals (VICTORY)
- **Bug**: Go does NOT validate that reserved names in edition 2023 files must use bare identifiers, not string literals. In proto2/proto3, `reserved "foo"` uses quoted strings. In editions, the syntax changes to `reserved foo` (bare identifier). C++ protoc rejects `reserved "old_field"` in edition files with "Reserved names must be identifiers in editions, not string literals." Go silently accepts the string literal form.
- **Test**: `430_edition_reserved_string_lit` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's parser accepts `reserved "old_field"` in edition files the same way it does for proto2/proto3. There is no validation that checks whether edition files use string literals vs bare identifiers for reserved names. The C++ parser has a specific check in `Parser::ParseReservedNames()` that rejects string literals when `is_editions` is true.
- **C++ protoc**: `test.proto:11:12: Reserved names must be identifiers in editions, not string literals.` (exit code 1).
- **Go protoc-go**: Silently accepts, produces valid descriptor (exit code 0).
- **Fix hint**: In the parser's `parseReservedNames` (or wherever reserved names are parsed), check if the file syntax is `editions`. If so, reject string literal tokens and require identifier tokens. Alternatively, add a validation pass in cli.go that checks `fd.GetSyntax() == "editions"` files and rejects reserved name entries that look like they came from string literals (though this info may be lost after parsing).

### Run 100 — Proto3 enum value prefix conflict not validated (VICTORY)
- **Bug**: Go does NOT validate that proto3 enum value names don't conflict when the enum type name prefix is stripped and case is ignored. In proto3, C++ protoc checks that no two enum values resolve to the same name after removing the enum type's name as a prefix (case-insensitive). For example, in `enum Status { STATUS_UNKNOWN = 0; UNKNOWN = 1; }`, stripping the `STATUS_` prefix from `STATUS_UNKNOWN` gives `UNKNOWN`, which conflicts with the literal `UNKNOWN` value.
- **Test**: `431_enum_prefix_conflict` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's enum validation has no prefix-stripping conflict check. C++ protoc's `DescriptorBuilder::BuildEnumValue` calls `EnumValueToPascalCase` and checks for prefix conflicts among all enum values, ensuring that values like `STATUS_UNKNOWN` and `UNKNOWN` are flagged. Go has `validateEnumValueConflicts` (or similar) but only checks exact name duplicates, not prefix-stripped case-insensitive duplicates.
- **C++ protoc**: `test.proto:7:3: Enum name UNKNOWN has the same name as STATUS_UNKNOWN if you ignore case and strip out the enum name prefix (if any). (If you are using allow_alias, please assign the same number to each enum value name.)`
- **Go protoc-go**: Silently accepts, produces valid descriptor (exit code 0).
- **Fix hint**: Add a validation function that, for proto3 (and editions OPEN) enums, strips the uppercase enum type name prefix from each value name, converts to a canonical form (e.g., uppercase), and checks for duplicates. The prefix to strip is the enum type name converted to UPPER_SNAKE_CASE followed by `_`. If two values have the same canonical name after stripping and case folding, emit the C++ error message. The check should also respect `allow_alias` — if aliased values (same number) have conflicting stripped names, it's allowed.
- **Also affects**: Same validation should apply to editions OPEN enums (default enum_type in edition 2023). Nested enums inside messages should also be checked. The prefix stripping algorithm must match C++ exactly — it's case-insensitive and strips the enum name as an upper-case prefix.

### Run 101 — Cross-file duplicate symbol not detected (VICTORY)
- **Bug**: Go does NOT detect when a symbol (message, enum, service, etc.) with the same fully-qualified name is defined in two different files. When file `b.proto` imports `a.proto` and both define `package.Shared`, C++ protoc rejects the redefinition with `"package.Shared" is already defined in file "a.proto"`. Go silently accepts it and produces a descriptor.
- **Test**: `432_cross_file_dup` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: Go's descriptor pool / type resolution does not track which symbols have been defined by which file and check for cross-file duplicates. C++ protoc's `DescriptorPool::BuildFile` maintains a global symbol table and checks every new definition against it. When adding `crossdup.Shared` from `test.proto`, it finds it already exists from `dep.proto` and rejects it.
- **C++ protoc**: `test.proto:7:9: "crossdup.Shared" is already defined in file "dep.proto".` (exit code 1).
- **Go protoc-go**: Silently accepts, produces valid descriptor (exit code 0).
- **Fix hint**: In the descriptor pool's `BuildFile` equivalent (or during symbol registration), maintain a map of fully-qualified symbol names to their defining file. Before registering a new symbol, check if it already exists in the map. If so, emit an error: `"<fqn>" is already defined in file "<orig_file>".` This affects all symbol types: messages, enums, services, extensions, and their nested types.
- **Also affects**: Same bug likely exists for conflicting enum values, service methods, and extension field numbers across files. Any cross-file duplicate detection is probably missing.

### Run 102 — Duplicate cross-file extension number treated as error instead of warning (VICTORY)
- **Bug**: Go treats duplicate extension field numbers across files as a hard ERROR (exit 1), while C++ protoc treats them as a WARNING (exit 0) and still produces the descriptor. When `dep.proto` extends `Base` with field number 100 and `test.proto` also extends `Base` with field number 100, C++ protoc emits a warning and continues. Go emits an error and stops.
- **Test**: `433_dup_ext_num` — all 9 profiles fail (C++ succeeds with warning, Go fails with error).
- **Root cause**: Go's extension number duplicate detection uses `errors` (hard failures) instead of `warnings` (continue compilation). C++ protoc's `DescriptorBuilder` detects the duplicate but classifies it as a warning, printing "warning: Extension number 100 has already been used..." to stderr while still producing a valid descriptor. Go's validation emits the same check as a fatal error.
- **C++ protoc**: `test.proto:6:27: warning: Extension number 100 has already been used in "dupextnum.Base" by extension "dupextnum.ext_a" defined in dep.proto.` (exit code 0, descriptor produced).
- **Go protoc-go**: `test.proto:6:27: Extension number 100 has already been used in "dupextnum.Base" by extension "dupextnum.ext_a".` (exit code 1, no descriptor).
- **Fix hint**: Change the extension number duplicate check from a hard error to a warning. Print to stderr with "warning:" prefix but don't add to the error list. Also, the Go error message is missing "defined in dep.proto" — add the source file info.
- **Also affects**: Same warning-vs-error mismatch may exist for other cross-file validation checks that C++ treats as warnings.

### Run 103 — Incomplete float exponent `1e` accepted by Go, rejected by C++ (VICTORY)
- **Bug**: Go's tokenizer does NOT validate that `e`/`E` in a float literal must be followed by at least one digit. When `[default = 1e]` is used, Go tokenizes `1e` as a valid `TokenFloat` and stores `default_value: "1e"` in the descriptor. C++ protoc rejects it with `"e" must be followed by exponent.`
- **Test**: `434_incomplete_exponent` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `readNumber()` in `io/tokenizer/tokenizer.go:486-493` consumes the `e` character, optionally `+`/`-`, and then greedily consumes digits. But if no digits follow the `e` (or `e+`/`e-`), it doesn't report an error — the exponent part is simply empty. The token `1e` is returned as type `TokenFloat`. Same bug exists in `readFloatStartingWithDot()` at lines 514-521.
- **C++ protoc**: `test.proto:5:40: "e" must be followed by exponent.` (exit code 1).
- **Go protoc-go**: Accepts `1e` as valid float, stores `default_value: "1e"` in descriptor (exit code 0).
- **Fix hint**: After consuming `e`/`E` and optional sign, check if at least one digit was consumed. If not, emit error `"\"e\" must be followed by exponent."` at the position of the `e` character. Same fix needed in `readFloatStartingWithDot()`.
- **Also affects**: `1E`, `1e+`, `1e-`, `.5e`, `.5E+`, etc. — any float literal where `e`/`E` is not followed by digits. Also affects float literals in custom option values, aggregate option values, and anywhere else the tokenizer is used.

### Run 104 — Positive sign `+` in field default value produces wrong error (VICTORY)
- **Bug**: Go's parser doesn't handle the `+` sign before default values. When a field has `[default = +42]`, Go's parser treats `+` as the value token (a Symbol), then tries to validate it as an integer. This produces "Integer out of range." followed by cascade errors "Expected ';'" and "Expected ']'". C++ protoc gives a single clean error: "Expected integer for field default value."
- **Test**: `435_positive_default` — all 9 profiles fail (both reject, but error messages differ).
- **Root cause**: `parseFieldOptions()` at parser.go:5787-5790 checks for `-` sign before default values but does NOT check for `+`. When `+` is encountered, it's consumed as `valTok` (a Symbol). The parser then tries to process `+` as the default value string, `strconv.ParseUint("+", 0, 64)` fails, and it reports "Integer out of range" (misleading). Then `42` is left in the token stream, confusing subsequent parsing.
- **C++ protoc**: `test.proto:7:35: Expected integer for field default value.` (single error, clean).
- **Go protoc-go**: `test.proto:7:35: Integer out of range.` + `test.proto:7:36: Expected ";".` + `test.proto:7:36: Expected "]".` (three errors, misleading).
- **Fix hint**: After checking for `-`, also check for `+` sign: `if optName == "default" && p.tok.Peek().Value == "+" { p.tok.Next() }` — just skip it since positive sign is a no-op for numeric values. Or, match C++ behavior: if `+` is seen, emit "Expected integer for field default value." error.

### Run 105 — Missing "Need space between number and identifier" tokenizer warning (VICTORY)
- **Bug**: Go's tokenizer does NOT emit the "Need space between number and identifier." warning when a number token is immediately followed by an alphabetic character without whitespace. When a proto has `[default = 0b1010]` (binary literal syntax, not valid in proto), C++ tokenizer reads `0` as a number, sees `b` immediately after, and emits the specific warning before the parse error. Go's tokenizer reads `0` as a number and `b1010` as a separate identifier, producing only a generic "Expected ";"." error.
- **Test**: `438_number_ident_space` — all 9 profiles fail.
- **Root cause**: Go's `readNumber()` in `io/tokenizer/tokenizer.go:455` does not check if the character immediately following a number token is an alphabetic character. C++ tokenizer's `ConsumeNumber` checks this and calls `AddError("Need space between number and identifier.")` when it detects a number-to-identifier transition without whitespace.
- **C++ protoc**: `test.proto:4:38: Need space between number and identifier.` + `test.proto:4:38: Expected "]".`
- **Go protoc-go**: `test.proto:4:38: Expected ";".` + `test.proto:4:38: Expected "]".`
- **Fix hint**: After `readNumber()` finishes building the number token, check if `t.pos < len(t.input)` and the next character is alphabetic (`isIdentStart(t.input[t.pos])`). If so, emit `TokenError{..., Message: "Need space between number and identifier."}`. This matches C++ behavior in `io/tokenizer.cc`'s `Tokenizer::Next()` where it checks `current_char_ == '_' || ascii_isalpha(current_char_)` after consuming a number.

### Run 106 — Unknown edition error message differs in capitalization and punctuation (VICTORY)
- **Bug**: Go's `parseEdition()` produces `unknown edition "2025"` (lowercase, no period) while C++ protoc produces `Unknown edition "2025".` (capital U, trailing period). The error message format doesn't match.
- **Test**: `439_unknown_edition` — all 9 profiles fail.
- **Root cause**: `parseEdition()` in `compiler/parser/parser.go:548` uses `fmt.Errorf("%d:%d: unknown edition %q", ...)` — lowercase `u` and no trailing period. C++ protoc's `Parser::Parse()` in `compiler/parser.cc` uses `Unknown edition "%s".` with capital `U` and a trailing period.
- **C++ protoc**: `test.proto:1:11: Unknown edition "2025".`
- **Go protoc-go**: `test.proto:1:11: unknown edition "2025"`
- **Fix hint**: Change the format string in `parseEdition()` from `"unknown edition %q"` to `"Unknown edition %q."` (capitalize and add period). Also note Go uses `%q` which adds Go-style quoting — verify that `%q` produces the same output as C++'s `"%s"` with explicit quotes.

### Run 107 — jstype = JS_NORMAL on non-int64 field incorrectly rejected (VICTORY)
- **Bug**: Go's `collectJstypeErrors()` rejects `jstype = JS_NORMAL` on non-int64 fields, but C++ protoc accepts it. `JS_NORMAL` is the default value, so explicitly setting it is harmless. C++ only rejects non-default jstype values (like `JS_STRING` or `JS_NUMBER`) on non-int64 fields.
- **Test**: `440_jstype_normal_nonint64` — all 9 profiles fail.
- **Root cause**: `collectJstypeErrors()` at cli.go:1349 checks `field.Options != nil && field.Options.Jstype != nil` but does NOT filter out `JS_NORMAL`. The extension-level check at cli.go:1337 DOES have `GetJstype() != JS_NORMAL`, but the field-level check in the inner function is missing this condition.
- **C++ protoc**: Accepts `string name = 1 [jstype = JS_NORMAL]` fine, produces valid descriptor (exit 0).
- **Go protoc-go**: Rejects with `jstype is only allowed on int64, uint64, sint64, fixed64 or sfixed64 fields.` (exit 1).
- **Fix hint**: Add `field.GetOptions().GetJstype() != descriptorpb.FieldOptions_JS_NORMAL` condition to the check in `collectJstypeErrors()`, matching what the extension-level check does. I.e., change the condition to: `if field.Options != nil && field.Options.Jstype != nil && field.GetOptions().GetJstype() != descriptorpb.FieldOptions_JS_NORMAL {`

### Run 108 — Validation error ordering differs between C++ and Go (VICTORY)
- **Bug**: When a message has multiple validation errors (duplicate field number + reserved number conflict + reserved name conflict), Go outputs them in a different order than C++ protoc. C++ runs reserved-number and reserved-name checks before duplicate-field-number checks, but Go runs duplicate-field-number first.
- **Test**: `441_error_ordering` — all 9 profiles fail.
- **Root cause**: Go's validation functions in `cli.go` run field number duplication checks before reserved range/name checks. C++ protoc's `DescriptorBuilder::CrossLinkField` runs reserved checks first (checking each field against reserved ranges and names), then does duplicate field number validation afterward. The order of error accumulation differs.
- **C++ protoc**: `Field "ccc" uses reserved number 3.` → `Field name "ccc" is reserved.` → `Field number 1 has already been used...` → `Suggested field numbers...`
- **Go protoc-go**: `Field number 1 has already been used...` → `Field "ccc" uses reserved number 3.` → `Field name "ccc" is reserved.` → `Suggested field numbers...`
- **Fix hint**: Reorder the validation calls in Go to match C++ ordering. In the function that validates message fields, run reserved-number and reserved-name checks before duplicate-field-number checks. This likely involves swapping the order of `collectDuplicateFieldNumberErrors` and `collectReservedFieldErrors` (or equivalent) calls.

### Run 109 — Enum reserved -2147483648 (INT32_MIN) rejected by Go, accepted by C++ (VICTORY)
- **Bug**: Go's `parseEnumReserved` rejects `reserved -2147483648;` (INT32_MIN) in enum reserved ranges with "Integer out of range." C++ protoc accepts it fine. The value `-2147483648` is a valid int32 value (it's exactly `INT32_MIN`), but Go checks the unsigned magnitude `2147483648 > MaxInt32` BEFORE applying the negation sign.
- **Test**: `442_enum_reserved_int32_min` — all 9 profiles fail (C++ succeeds, Go errors).
- **Root cause**: `parseEnumReserved()` at parser.go:3472-3474 does `parseIntLenient(numTok.Value, 0, 64)` which returns `2147483648` for the token `"2147483648"`. Then checks `startNum > math.MaxInt32` → `2147483648 > 2147483647` → TRUE → error. The negation is applied AFTER this check at line 3476-3480, but we never get there because the pre-negation range check already rejected the value.
- **C++ protoc**: Accepts `reserved -2147483648;` fine, produces valid descriptor (exit 0).
- **Go protoc-go**: `test.proto:9:13: Integer out of range.` (exit 1).
- **Fix hint**: Move the `startNum > math.MaxInt32` check to AFTER negation. Or allow `startNum == math.MaxInt32 + 1` when `startNeg` is true (since `-(MaxInt32+1) == MinInt32`). Same issue exists for the end-of-range value at line 3518-3524 — `en > math.MaxInt32` would also reject `-2147483648` as the end of a range.
- **Also affects**: Same bug likely exists for `reserved -2147483648 to 0;` (would fail on the start value) and `reserved 0 to -2147483648;` (would fail on the end value if negative). Also, message reserved ranges at parser.go:968 may have a similar issue if enum-style negative values are supported there (they aren't in proto2/proto3 messages, but editions may differ).

### Run 110 — --direct_dependencies flag silently ignored by Go (VICTORY)
- **Bug**: Go's CLI silently skips the `--direct_dependencies=` flag (just `continue`s past it). C++ protoc uses this flag to validate that all imports are declared as direct dependencies — if an import is missing from the list, C++ emits an error. Go doesn't implement any of this validation, so it always succeeds even when direct dependencies are violated.
- **Test**: CLI test `cli@direct_dependencies` — exit code mismatch (C++ exits 1, Go exits 0).
- **Root cause**: `cli.go:1065` has `if strings.HasPrefix(arg, "--direct_dependencies=") { continue }` — the flag value is parsed but completely discarded. No `directDependencies` field in the config, no validation in the import resolution phase. The related `--direct_dependencies_violation_msg` flag isn't even recognized (would fail with "Unknown flag").
- **C++ protoc**: `test.proto: File is imported but not declared in --direct_dependencies: dep.proto` (exit 1).
- **Go protoc-go**: Silently succeeds (exit 0), no error about undeclared dependency.
- **Fix hint**: (1) Parse `--direct_dependencies=` value into a set of allowed imports (colon-delimited). (2) Also parse `--direct_dependencies_violation_msg=`. (3) After resolving imports, check each import against the allowed set. (4) If missing, emit the violation message (default: "File is imported but not declared in --direct_dependencies: %s").
- **Also affects**: `--direct_dependencies_violation_msg` flag is completely unrecognized — Go fails with "Unknown flag" instead of accepting it.

### Run 111 — \U escape above U+10FFFF accepted by Go, rejected by C++ (VICTORY)
- **Bug**: Go's tokenizer `appendUTF8()` silently handles code points above `U+10FFFF` by writing the literal `\U%08x` text back into the string, instead of rejecting them. C++ protoc's tokenizer validates that `\U` escapes have code points ≤ `0x10FFFF` and rejects values above that range with a clear error message.
- **Test**: `444_unicode_escape_range` — all 9 profiles fail (C++ errors, Go succeeds).
- **Root cause**: `appendUTF8()` in `io/tokenizer/tokenizer.go` has a branch for `cp > 0x10FFFF` that does `fmt.Fprintf(sb, "\\U%08x", cp)` — writing the escape sequence as literal text. This means the string gets a literal backslash-U followed by hex digits instead of decoded bytes. The tokenizer never errors on this. C++ protoc's tokenizer checks `if (code_point > 0x10FFFF)` and emits `"Expected eight hex digits up to 10ffff for \\U escape sequence"`.
- **C++ protoc**: `test.proto:9:41: Expected eight hex digits up to 10ffff for \U escape sequence` (exit code 1).
- **Go protoc-go**: Silently accepts, stores literal `\U00200000` text as `default_value` in descriptor (exit code 0).
- **Fix hint**: In `appendUTF8()`, instead of writing the literal escape text for `cp > 0x10FFFF`, the tokenizer should emit an error: `TokenError{..., Message: "Expected eight hex digits up to 10ffff for \\U escape sequence."}`. Or add a check in the `\U` escape handler in `readString()` before calling `appendUTF8()`.
- **Also affects**: Any string literal with `\U` code points from `0x110000` to `0xFFFFFFFF`. Same issue exists for `\u` escapes combined into surrogates that produce values > `0x10FFFF` (though that's unlikely since surrogates map to supplementary plane values ≤ `0x10FFFF`).

### Run 112 — --fatal_warnings flag silently ignored by Go (VICTORY)
- **Bug**: Go's CLI recognizes the `--fatal_warnings` flag but silently ignores it (`continue` at cli.go:1049). C++ protoc uses this flag to turn warnings into fatal errors (exit code 1). When both compilers emit an identical warning (e.g., cross-file duplicate extension number), C++ exits 1 with `--fatal_warnings` while Go exits 0.
- **Test**: CLI test `cli@fatal_warnings_ext` — exit code mismatch (C++ exits 1, Go exits 0).
- **Root cause**: `cli.go:1049` has `if arg == "--fatal_warnings" { continue }` — the flag is parsed but completely discarded. No `fatalWarnings` boolean is stored, and warnings are never checked for fatal promotion before exit.
- **C++ protoc**: `test.proto:7:27: warning: Extension number 100 has already been used...` (exit code 1 with --fatal_warnings).
- **Go protoc-go**: Same warning message (exit code 0, --fatal_warnings ignored).
- **Fix hint**: (1) Parse `--fatal_warnings` into a boolean config field. (2) After compilation, if `fatalWarnings` is true and any warnings were emitted, set exit code to 1. Or, before printing warnings, promote them to errors (remove "warning:" prefix) and add to the error list.
- **Also affects**: Any other scenario that produces warnings (e.g., unused imports, JSON name conflicts in proto2) would also be affected if Go ever implements those warnings.

### Run 113 — --experimental_allow_proto3_optional flag not recognized by Go (VICTORY)
- **Bug**: Go's CLI does not recognize `--experimental_allow_proto3_optional` as a valid boolean flag. C++ protoc recognizes it as a no-op flag (proto3 optional is now fully supported) and silently accepts it. Go falls through to the generic unknown-flag handler and rejects it with "Missing value for flag: --experimental_allow_proto3_optional".
- **Test**: CLI test `cli@experimental_allow_proto3_optional` — exit code and stderr mismatch.
- **Root cause**: Go's `parseArgs()` in `cli.go` has no handler for `--experimental_allow_proto3_optional`. The flag starts with `--` and has no `=`, so it reaches the fallback at line 1171 which returns `"Missing value for flag: %s"`. C++ protoc has this as a registered boolean flag that's accepted but ignored.
- **C++ protoc**: Silently accepts flag, produces valid descriptor (exit code 0, empty stderr).
- **Go protoc-go**: `Missing value for flag: --experimental_allow_proto3_optional` (exit code 1).
- **Fix hint**: Add `if arg == "--experimental_allow_proto3_optional" { continue }` to the flag parsing section in `parseArgs()`, alongside the other no-op flags like `--deterministic_output` and `--retain_options`.

### Run 114 — Blank line between comments creates spurious empty detached comment (VICTORY)
- **Bug**: Go's comment collector emits a spurious empty `leading_detached_comments: ""` entry when there is a blank line between a line comment and a block comment before a token. C++ protoc treats the blank line as a separator between the two comment groups but does NOT create an empty string entry.
- **Test**: `447_detached_comment_blank_line` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: In Go's `collectComments()` (or equivalent comment tracking in the tokenizer), when a blank line is encountered between two comments, the tokenizer creates an empty comment group entry `""` for the gap. C++ protoc's comment collector recognizes the blank line as a boundary between detached comment groups but doesn't insert an empty string — it just starts a new group for the next comment.
- **C++ protoc**: `leading_detached_comments: " Line comment\n"` + `leading_detached_comments: " Block comment "` (2 entries).
- **Go protoc-go**: `leading_detached_comments: " Line comment\n"` + `leading_detached_comments: ""` + `leading_detached_comments: " Block comment "` (3 entries, extra empty one).
- **Fix hint**: In the comment collector, when a blank line separates two comment groups, don't emit an empty detached comment for the blank line. Instead, just close the current group and start a new one. The blank line is a separator, not a comment.

### Run 115 — decode_raw rejects overflowed 10-byte varint that C++ accepts (VICTORY)
- **Bug**: Go's `--decode_raw` rejects a 10-byte varint where the 10th byte has value 0x02 (which overflows uint64). C++ protoc accepts it and wraps the value to 0. The protobuf wire format spec allows up to 10 bytes for a varint, but Go's `protowire.ConsumeVarint` rejects 10th bytes > 0x01 since only 1 bit (bit 63) fits in uint64.
- **Test**: STDIN_TEST `decode_raw_overflow_varint` with hex `0880808080808080808002` — 1 test fails.
- **Root cause**: Go's `google.golang.org/protobuf/encoding/protowire.ConsumeVarint` strictly validates that the 10th byte of a varint is ≤ 0x01. C++ protobuf's `CodedInputStream::ReadVarint64` does not check for overflow — it simply shifts and ORs the bits, allowing wrap-around. The varint `0x80,0x80,...,0x80,0x02` decodes to 2 << 63 = 2^64 which wraps to 0 in C++ uint64 but is rejected by Go.
- **C++ protoc**: Outputs `1: 0` (accepts and wraps, exit code 0).
- **Go protoc-go**: Outputs `Failed to parse input.` on stderr (exit code 1).
- **Fix hint**: In the decode_raw implementation, use a custom varint reader that allows overflow (matching C++ behavior), or catch the protowire error and manually decode overflowed varints by masking to 64 bits.

### Run 119 — --decode mode prints enum values as numbers instead of names (VICTORY)
- **Bug**: Go's `printTextProto` function in `--decode` mode prints enum field values as raw numeric values (e.g., `color: 1`) instead of looking up the enum value name (e.g., `color: COLOR_RED`). C++ protoc's text format printer always resolves enum values to their symbolic names.
- **Test**: Decode test `decode@enum_value` — stdout mismatch. Added `run_decode_test` function and `DECODE_TESTS` array to `scripts/test`. Test proto in `testdata/448_decode_enum/test.proto`.
- **Root cause**: `printTextProto()` in `cli.go` handles varint fields by printing the raw integer value. It does not check if the field's type is `TYPE_ENUM`, look up the enum's `EnumDescriptorProto`, and find the matching `EnumValueDescriptorProto` to print its name.
- **C++ protoc**: `color: COLOR_RED` + `label: "world"` (exit code 0).
- **Go protoc-go**: `color: 1` + `label: "world"` (exit code 0).
- **Fix hint**: In `printTextProto`, when a varint field has `GetType() == TYPE_ENUM`, look up the field's `GetTypeName()` in `allMsgs` (or a separate enum map), find the `EnumValueDescriptorProto` whose `GetNumber()` matches the varint value, and print its `GetName()` instead of the numeric value.

### Run 120 — --decode mode float field prints too many digits (VICTORY)
- **Bug**: Go's `formatTextFloat` converts `float32` to `float64` and then calls `formatTextDouble` which uses `strconv.FormatFloat(v, 'g', -1, 64)` with bitSize=64. This produces the shortest decimal that uniquely represents the **float64** value, not the original **float32** value. For most float32 values, this outputs way too many digits. C++ protoc uses `SimpleFtoa` which uses `snprintf(buf, "%.6g", val)` with float32 precision.
- **Test**: Decode test `decode@float_value` — stdout mismatch. Proto in `testdata/449_decode_float/test.proto`. Hex data: `0dcdcccc3d1203666f6f` (field 1 float32 0.1, field 2 string "foo").
- **Root cause**: `formatTextFloat(v float32)` at cli.go:9141 does `return formatTextDouble(float64(v))`. This promotes the float32 to float64, losing the information about the original precision. `formatTextDouble` then calls `FormatFloat(v, 'g', -1, 64)` which finds the shortest decimal for the float64 representation. Since `float64(float32(0.1))` = `0.10000000149011612` (which is NOT `0.1` as a float64), Go prints all those digits.
- **C++ protoc**: `value: 0.1` (SimpleFtoa uses float32-precision formatting).
- **Go protoc-go**: `value: 0.10000000149011612` (formatTextDouble uses float64-precision formatting).
- **Fix hint**: Change `formatTextFloat` to use `strconv.FormatFloat(float64(v), 'g', -1, 32)` with bitSize=32 (not 64). The `32` tells FormatFloat to find the shortest decimal that uniquely represents the value as a float32, matching C++ SimpleFtoa behavior. Alternatively, implement float32-specific formatting: try 6 significant digits, check if round-trip succeeds, then try 9 digits.
- **Also affects**: ANY float32 value that's not exactly representable in a short decimal will be over-specified. Examples: 0.1 → "0.10000000149011612", 1/3 → "0.3333333432674408", 0.3 → "0.30000001192092896". This affects ALL `--decode` output for any message with float fields.

### Run 121 — Decode mode prints TYPE_INT64 as unsigned instead of signed (VICTORY)
- **Bug**: Go's `printKnownField` for `TYPE_INT64` prints `e.varint` (which is `uint64`) with `%d` without casting to `int64`. For negative values like -1, the wire format stores `0xFFFFFFFFFFFFFFFF` as the varint. Go prints `18446744073709551615` (unsigned interpretation). C++ protoc casts to `int64` and prints `-1`.
- **Test**: Decode test `decode@neg_int64` — stdout mismatch (1 test fails).
- **Root cause**: `printKnownField` at cli.go:9016-9018 has `case TYPE_INT64, TYPE_UINT64: fmt.Fprintf(w, "%s%s: %d\n", prefix, name, e.varint)`. Both TYPE_INT64 and TYPE_UINT64 share the same case, printing `e.varint` (uint64) directly. TYPE_INT64 should cast to `int64(e.varint)` to get the signed representation, while TYPE_UINT64 should keep unsigned. These two types should be separate cases.
- **C++ protoc**: `value: -1` (interprets varint as signed int64).
- **Go protoc-go**: `value: 18446744073709551615` (prints varint as unsigned uint64).
- **Fix hint**: Split the case into two: `case TYPE_INT64: fmt.Fprintf(w, "%s%s: %d\n", prefix, name, int64(e.varint))` and `case TYPE_UINT64: fmt.Fprintf(w, "%s%s: %d\n", prefix, name, e.varint)`. The cast `int64(e.varint)` converts two's complement back to signed representation.
- **Also affects**: ANY negative int64 value decoded with `--decode` will show as a large positive number. This is a fundamental decode correctness bug for signed 64-bit integers.

### Run 122 — Decode mode appends spurious trailing dot to integer-valued doubles (VICTORY)
- **Bug**: Go's `formatTextDouble` appends a trailing `.` when the formatted string has no decimal point or exponent. For integer-valued doubles like `1.0`, Go outputs `value: 1.` while C++ protoc outputs `value: 1`. The trailing dot is Go's invention — C++ `SimpleDtoa` returns `"1"` and the text format printer doesn't modify it.
- **Test**: Decode test `decode@double_integer` — stdout mismatch (1 test fails).
- **Root cause**: `formatTextDouble()` at cli.go:9136-9138 checks `if !strings.Contains(s, ".") && !strings.Contains(s, "e")` and appends `s += "."`. C++ protoc's `SimpleDtoa(1.0)` returns `"1"` without a trailing dot, and the text format printer prints it as-is.
- **C++ protoc**: `value: 1` (no trailing dot).
- **Go protoc-go**: `value: 1.` (with trailing dot).
- **Fix hint**: Remove the dot-appending logic from `formatTextDouble`. C++ text format does NOT add a trailing dot to integer-valued doubles. The function should just return `s` directly after `FormatFloat`. Same fix may be needed for `formatTextFloat` if it has the same logic.
- **Also affects**: ANY integer-valued double or float in `--decode` mode will have a spurious trailing dot. Examples: `2.0` → `"2."` instead of `"2"`, `100.0` → `"100."` instead of `"100"`, `-3.0` → `"-3."` instead of `"-3"`. This affects all messages with double/float fields decoded in `--decode` mode.

### Run 123 — Decode mode formatTextFloat appends spurious trailing dot to integer-valued floats (VICTORY)
- **Bug**: Go's `formatTextFloat` appends a trailing `.` when the formatted string has no decimal point or exponent. For integer-valued float32 values like `2.0f`, Go outputs `value: 2.` while C++ protoc outputs `value: 2`. This is the same bug as Run 122 (formatTextDouble) but in `formatTextFloat` — Ralph fixed the double version but left the float version with the same bug.
- **Test**: Decode test `decode@float_integer` — stdout mismatch (1 test fails). Proto in `testdata/452_decode_float_int/test.proto`. Hex data: `0d000000401203666f6f` (field 1 float32 2.0, field 2 string "foo").
- **Root cause**: `formatTextFloat()` at cli.go:9163-9165 checks `if !strings.Contains(s, ".") && !strings.Contains(s, "e") && !strings.Contains(s, "E")` and appends `s += "."`. C++ protoc's `SimpleFtoa(2.0f)` returns `"2"` without a trailing dot, and the text format printer prints it as-is. The subnormal path at line 9154-9156 also has the same trailing dot logic.
- **C++ protoc**: `value: 2` (no trailing dot).
- **Go protoc-go**: `value: 2.` (with trailing dot).
- **Fix hint**: Remove both trailing dot blocks from `formatTextFloat`: the one at line 9154-9156 (subnormal path) and the one at line 9163-9165 (normal path). C++ `SimpleFtoa` never adds a trailing dot. Just return `s` directly after formatting. This was already fixed for `formatTextDouble` in Run 122 — same fix needed for `formatTextFloat`.
- **Also affects**: ANY integer-valued float32 in `--decode` mode will have a spurious trailing dot. Examples: `1.0f` → `"1."`, `100.0f` → `"100."`, `-3.0f` → `"-3."`. Also affects subnormal float paths (though subnormals are never integer-valued, the dot logic is still wrong for any subnormal that formats without a decimal point — e.g., if FormatFloat somehow produces an integer-like string).

### Run 124 — Decode mode prints extension fields as unknown (raw field number) instead of by name (VICTORY)
- **Bug**: Go's `buildFieldMap()` in `cli.go` only iterates `msg.GetField()` when building the known-field map for `--decode` mode. Extension fields (defined via `extend Base { ... }`) are not part of `GetField()` — they live in `FileDescriptorProto.GetExtension()`. So extension fields are treated as unknown and printed by raw field number (`100: 42`) instead of by their fully-qualified name in brackets (`[decodeext.extra]: 42`).
- **Test**: Decode test `decode@extension_field` — stdout mismatch (1 test fails).
- **Root cause**: `buildFieldMap(msgDesc)` at cli.go:8846 only calls `msgDesc.GetField()`. Extensions are separate in the descriptor: `fd.GetExtension()` contains top-level extensions, and nested messages can also have extensions. C++ protoc uses the `Reflection` interface which knows about all registered extensions from the descriptor pool.
- **C++ protoc**: `[decodeext.extra]: 42` (extension printed by name in brackets).
- **Go protoc-go**: `100: 42` (extension treated as unknown field, printed by number).
- **Fix hint**: After building `fieldMap` from `msg.GetField()`, also iterate all `fd.GetExtension()` across all parsed files. For each extension where `GetExtendee()` matches the target message type, add it to `fieldMap` with its field number. Print extension fields as `[fully.qualified.name]: value` using brackets (C++ text format convention).
- **Also affects**: ALL proto2 messages with extensions decoded in `--decode` mode. Any extension field will be printed as an unknown field by number.

### Run 125 — Decode mode does not sort map entries by key (VICTORY)
- **Bug**: Go's `--decode` mode prints map entries in wire order (order they appear in the binary data). C++ protoc's TextFormat sorts map entries alphabetically by key before printing. When a map has entries keyed "timeout" and "retries", C++ prints "retries" first (alphabetical), Go prints "timeout" first (wire order).
- **Test**: Decode test `decode@map_sort` — stdout mismatch (1 test fails). Proto in `testdata/456_decode_map_sort/test.proto`. Hex data: `0a0b0a0774696d656f7574101e0a0b0a07726574726965731003120474657374` (two map entries keyed "timeout" then "retries", plus label "test").
- **Root cause**: `printTextProto()` in cli.go sorts known entries by field number but does NOT sort map field entries by key. C++ protoc's `TextFormat::Printer` internally uses `MapSorter` to sort map entries by key before printing each map field.
- **C++ protoc**: `settings { key: "retries" value: 3 } settings { key: "timeout" value: 30 } label: "test"` (alphabetical by key).
- **Go protoc-go**: `settings { key: "timeout" value: 30 } settings { key: "retries" value: 3 } label: "test"` (wire order).
- **Fix hint**: In `printTextProto`, after sorting entries by field number, additionally sort entries within each field number group for map fields by their key value. Need to detect map fields (field has `TYPE_MESSAGE` and the message has `options.map_entry = true`), then parse each entry's bytes to extract the key, and sort by key. Keys can be string, int32, int64, uint32, uint64, sint32, sint64, fixed32, fixed64, sfixed32, sfixed64, bool — all need type-appropriate sorting.
- **Also affects**: ALL map fields in `--decode` mode will have wrong entry order. Any map with 2+ entries where the keys are not in alphabetical/ascending order in the wire data.

### Run 126 — Decode mode keeps proto2 unknown enum values as known fields instead of moving to unknown set (VICTORY)
- **Bug**: In proto2, when an enum field has a value not defined in the enum, C++ protoc's `Reflection` places it in the `UnknownFieldSet` and the text format printer prints it as an unknown varint entry (`1: 99`). Go's decode mode keeps unknown enum values as known field entries and prints them by field name (`s: 99`). This causes TWO differences: (1) field format (known vs unknown), and (2) field ordering (known fields are sorted by number and printed first, unknown fields after).
- **Test**: Decode test `decode@proto2_unknown_enum` — stdout mismatch (1 test fails). Proto in `testdata/457_decode_proto2_unknown_enum/test.proto`. Hex data: `086312026f6b` (field 1 varint 99, field 2 string "ok").
- **Root cause**: `printTextProto()` in cli.go dispatches entries to `knownEntries` or `unknownEntries` based solely on whether a field descriptor exists for the field number. For enum fields with unknown values, the field descriptor exists, so the entry goes to `knownEntries`. But C++ protobuf's `Reflection::Set` for proto2 enum fields rejects unknown values and stores them in `UnknownFieldSet` instead, causing them to be printed as unknown fields.
- **C++ protoc**: `label: "ok"\n1: 99\n` (unknown enum value printed as unknown varint field AFTER known fields).
- **Go protoc-go**: `s: 99\nlabel: "ok"\n` (unknown enum value printed as known field by name, sorted by field number BEFORE label).
- **Fix hint**: In `printTextProto`, when processing a known entry with `TYPE_ENUM` and the syntax is proto2 (not proto3/editions), check if the varint value is in the enum's value map. If not, move the entry to `unknownEntries` instead of `knownEntries`. Proto3 keeps unknown enum values in the field, so this only applies to proto2. Need to pass the file's syntax to `printTextProto`.
- **Also affects**: ANY proto2 enum field decoded with `--decode` where the binary data contains a value not defined in the enum. This includes both positive and negative unknown values. For negative unknown values, C++ additionally prints the raw unsigned varint (e.g., `1: 18446744073709551613` for -3), while Go would print the signed int32 value (`s: -3`).

### Run 127 — Decode mode treats edition 2023 OPEN enums as CLOSED (VICTORY)
- **Bug**: Go's `findMessageType()` at cli.go:8729 uses `isClosed := fd.GetSyntax() != "proto3"` to decide if enums are closed. Edition 2023 files have `GetSyntax()` returning `"editions"`, which is `!= "proto3"`, so `isClosed = true`. But edition 2023's default `features.enum_type` is OPEN, meaning unknown enum values should stay as known fields (like proto3). Go incorrectly moves unknown enum values to the unknown field set.
- **Test**: Decode test `decode@edition_open_enum` — stdout mismatch (1 test fails). Proto in `testdata/458_decode_edition_open_enum/test.proto`. Hex data: `086312026f6b` (field 1 varint 99, field 2 string "ok").
- **Root cause**: `findMessageType()` builds `closedEnums` map using `isClosed := fd.GetSyntax() != "proto3"`. For edition files, syntax is `"editions"`, not `"proto3"`, so ALL edition enums are treated as closed. But edition 2023's default `enum_type` is OPEN, so unknown values should be kept as known fields.
- **C++ protoc**: `p: 99\nlabel: "ok"\n` (unknown enum value 99 printed as known field with field name, OPEN enum behavior).
- **Go protoc-go**: `label: "ok"\n1: 99\n` (unknown enum value moved to unknown set, printed as unknown varint by field number AFTER known fields).
- **Fix hint**: Instead of `fd.GetSyntax() != "proto3"`, check edition features: for `"editions"` syntax, check if the enum has `features.enum_type = CLOSED` explicitly set or if file-level features resolve to CLOSED. For `"proto2"`, always closed. For `"proto3"`, always open. Could also do `isClosed := fd.GetSyntax() == "proto2"` as a simpler fix, since editions default is OPEN.
- **Also affects**: ALL edition 2023 messages with OPEN enums decoded in `--decode` mode. Any unknown enum value in an edition file will be incorrectly treated as an unknown field.

### Run 128 — Decode mode formatTextDouble uses wrong precision algorithm (VICTORY)
- **Bug**: Go's `formatTextDouble` in decode mode uses `strconv.FormatFloat(v, 'g', -1, 64)` which produces the **shortest** round-trippable decimal representation. C++ `SimpleDtoa` uses `snprintf(buf, "%.15g", val)` first (15 significant digits), and if round-trip fails, uses `snprintf(buf, "%.17g", val)` (17 digits). For doubles needing exactly 16 digits for shortest representation, Go outputs 16 digits while C++ skips from 15 to 17 digits.
- **Test**: `459_decode_double_precision` — decode@double_precision fails.
- **Root cause**: `formatTextDouble()` at cli.go:9472-9483 uses `FormatFloat(v, 'g', -1, 64)` (Go shortest) instead of the 15-then-17 pattern used by C++. The parser's `simpleDtoa` (parser.go:6337-6350) correctly implements 15/17, but the decode-mode `formatTextDouble` does not.
- **C++ protoc**: `value: 2.0000000000000009` (17 significant digits, because 15-digit `"2"` doesn't round-trip).
- **Go protoc-go**: `value: 2.000000000000001` (16 significant digits, Go's shortest representation).
- **Fix hint**: Replace `FormatFloat(v, 'g', -1, 64)` in `formatTextDouble` with the same 15-then-17 algorithm used by `simpleDtoa` in parser.go:6337-6350.

### Run 130 — Decode mode doesn't merge duplicate non-repeated scalar fields (VICTORY)
- **Bug**: Go's `--decode` mode prints ALL entries for a non-repeated scalar field when the binary data contains duplicates. C++ protoc's `DynamicMessage::ParseFromString` applies "last value wins" semantics for non-repeated fields, then `TextFormat::Print` outputs each field exactly once. Go's decode mode collects all wire entries into `knownEntries` and prints every one without deduplication (except for oneof fields which have special handling added in Run 129).
- **Test**: Decode test `decode@dup_scalar` — stdout mismatch (1 test fails). Proto in `testdata/461_decode_dup_scalar/test.proto`. Hex data: `0801080212026f6b` (field 1 varint 1, field 1 varint 2, field 2 string "ok").
- **Root cause**: `printTextProto()` in cli.go sorts and prints all `knownEntries` without checking for duplicate field numbers on non-repeated fields. There is oneof dedup logic (lines 9113-9141 from Run 129 fix) but no general non-repeated field dedup. C++ handles this automatically because `ParseFromString` → `MergeFrom` overwrites non-repeated scalar fields with the last value.
- **C++ protoc**: `value: 2\nlabel: "ok"\n` (only last value for non-repeated int32 field 1).
- **Go protoc-go**: `value: 1\nvalue: 2\nlabel: "ok"\n` (both entries printed — incorrect protobuf merging semantics).
- **Fix hint**: Before printing `knownEntries`, post-process to apply "last value wins" for non-repeated fields. For non-repeated scalars/enums/strings/bytes, keep only the last entry with each field number. For non-repeated message fields, merge all entries with the same field number by concatenating their bytes (protobuf merge semantics: submessages are merged, not replaced). For repeated fields (including maps), keep all entries. Need to check `fd.GetLabel() != LABEL_REPEATED` to identify non-repeated fields.
- **Also affects**: ALL non-repeated field types in `--decode` mode when binary data has duplicate field entries. This is common when messages are merged via concatenation (a standard protobuf pattern). Non-repeated message fields would be even more wrong — Go prints each sub-message separately instead of merging them. Non-repeated string/bytes/enum/float/double/bool/fixed types all have the same bug.

### Run 131 — Decode mode doesn't merge duplicate non-repeated group fields (VICTORY)
- **Bug**: Go's `--decode` mode dedup/merge logic for non-repeated fields only handles `BytesType` (length-delimited) message fields. Group fields use `StartGroupType` (wire type 3) and store data in `e.group`, not `e.bytes`. When binary data has two entries for the same non-repeated group field (with different sub-fields), C++ merges both groups' sub-fields into one output. Go keeps only the LAST group entry, losing sub-fields from earlier entries.
- **Test**: Decode test `decode@group_merge` — stdout mismatch (1 test fails). Proto in `testdata/462_decode_group_merge/test.proto`. Hex data: `0b0a0568656c6c6f0c0b102a0c12026f6b` (field 1 group with name="hello", field 1 group with value=42, field 2 string "ok").
- **Root cause**: The merge block at cli.go:9153 checks `e.wtype == protowire.BytesType`, which is false for groups (`e.wtype == StartGroupType`). So the group merge path is never entered. The "keep last" logic at the filtering step then discards all but the last entry. C++ protobuf's `DynamicMessage::MergeFrom` merges group entries by combining sub-fields from all entries.
- **C++ protoc**: `Inner {\n  name: "hello"\n  value: 42\n}\nlabel: "ok"\n` (both group entries merged — all sub-fields present).
- **Go protoc-go**: `Inner {\n  value: 42\n}\nlabel: "ok"\n` (first group entry's `name: "hello"` lost — only last entry kept).
- **Fix hint**: Add a parallel merge path for group fields: check `e.wtype == protowire.StartGroupType` and merge `e.group` bytes (concatenate group payloads). Something like: `if e.known.GetType() == TYPE_GROUP && e.wtype == protowire.StartGroupType { mergedGroups[e.num] = append(mergedGroups[e.num], e.group...); knownEntries[lastIdx[e.num]].group = mergedGroups[e.num] }`. The concatenation works because protobuf group merge semantics are the same as message merge — sub-fields are merged by concatenation.
- **Also affects**: ANY proto2 message with non-repeated group fields decoded from binary data containing multiple entries. Group fields are rare but valid in proto2.

### Run 132 — Decode mode doesn't reject invalid UTF-8 in proto3 string fields (VICTORY)
- **Bug**: Go's `--decode` mode does not validate UTF-8 encoding for proto3 string fields. When decoding binary data that contains invalid UTF-8 bytes (e.g., `\x80\x81`) in a proto3 string field, C++ protoc rejects the input with "Failed to parse input." and exit code 1, while Go happily decodes and prints the raw bytes as octal escapes with exit code 0.
- **Test**: `463_decode_utf8_invalid` — decode test fails (exit code mismatch: C++ returns 1, Go returns 0).
- **Root cause**: Go's `printKnownField` at cli.go:9291-9292 prints TYPE_STRING using `cEscapeForDecode(e.bytes)` without any UTF-8 validation. C++ protoc's `ParseFromString` validates UTF-8 for proto3 string fields during parsing and aborts if validation fails. Go's decode mode skips parsing into a proto message entirely — it directly reads wire format entries and prints them, never checking UTF-8.
- **C++ protoc**: `String field 'decodeutf8.test.Record.name' contains invalid UTF-8 data...` + `Failed to parse input.` (exit 1).
- **Go protoc-go**: `id: 42\nname: "\200\201"\n` (exit 0) — prints invalid UTF-8 as octal escapes.
- **Fix hint**: Before printing a TYPE_STRING field value, check if `e.bytes` is valid UTF-8 using `utf8.Valid(e.bytes)`. If the message is proto3 syntax (check `fd.GetFile().GetSyntax() == "proto3"` or the file's edition features), reject the input with an appropriate error message and exit code 1. Note: proto2 string fields do NOT require UTF-8 validation in C++ protoc, only proto3.
- **Also affects**: Any proto3 message decoded with `--decode` that has string fields with invalid UTF-8 content.

### Run 133 — Go accepts extension range past max field number (536870912) (VICTORY)
- **Bug**: Go's parser does not validate that extension range numbers must be ≤ 536870911 (2^29 - 1, the maximum protobuf field number). When a proto2 message declares `extensions 536870912;`, C++ protoc rejects it with "Extension numbers cannot be greater than 536870911." and exit code 1, while Go accepts it silently and produces a descriptor set.
- **Test**: `464_ext_range_past_max` — all 9 profiles fail.
- **Root cause**: The parser (or descriptor pool) does not enforce the maximum field number constraint on extension range start/end values. C++ protoc validates this in `DescriptorBuilder::BuildExtensionRange` which checks `end > FieldDescriptor::kMaxNumber + 1`. Go's equivalent code does not perform this check.
- **C++ protoc**: `test.proto: Extension numbers cannot be greater than 536870911.` (exit 1).
- **Go protoc-go**: Silently accepts and produces a descriptor (exit 0).
- **Fix hint**: In the parser or descriptor pool validation, when processing `extensions` declarations, check that both start and end of the range are ≤ 536870911. If not, emit an error: "Extension numbers cannot be greater than 536870911."

### Run 134 — Decode mode missing warning for absent required fields (VICTORY)
- **Bug**: Go's `--decode` mode does not emit a warning when a proto2 message is missing required fields. C++ protoc prints `warning:  Input message is missing required fields:  <names>` to stderr when required fields are absent from the wire data. Go silently decodes what's present without any warning.
- **Test**: Decode test `decode@missing_required` — stderr mismatch (1 test fails).
- **Root cause**: Go's `runDecode()` / `printTextProto()` functions do not check if all `LABEL_REQUIRED` fields are present in the decoded data. After parsing the wire format, C++ calls `message.IsInitialized()` which checks all required fields recursively, then emits the warning listing missing field names. Go has no equivalent check.
- **C++ protoc**: stderr: `warning:  Input message is missing required fields:  id, name`, stdout: `extra: 7` (exit 0).
- **Go protoc-go**: stderr: (empty), stdout: `extra: 7` (exit 0).
- **Fix hint**: After decoding the message, iterate all fields in the message descriptor. For each field with `GetLabel() == LABEL_REQUIRED`, check if it was seen in the wire data. If any are missing, print `warning:  Input message is missing required fields:  <comma-separated names>` to stderr. For nested messages, also check required fields recursively.

### Run 135 — Decode mode emits spurious "invalid bytes" on truncated input (VICTORY)
- **Bug**: Go's `--decode` mode prints an extra "invalid bytes" line to stderr before "Failed to parse input." when the input is truncated (incomplete wire data). C++ protoc only prints "Failed to parse input." Go's decode implementation leaks an internal error message that C++ does not emit.
- **Test**: Decode test `decode@truncated` — stderr mismatch (1 test fails).
- **Root cause**: Go's `runDecode()` or the wire-format parsing layer emits "invalid bytes" to stderr as part of its error handling before the top-level "Failed to parse input." message. C++ protoc's `Message::ParseFromString()` returns false on truncated data without printing extra diagnostics — only the outer decode loop prints "Failed to parse input."
- **C++ protoc**: stderr: `Failed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `invalid bytes\nFailed to parse input.` (exit 1). Extra line in stderr.
- **Fix hint**: In the decode error path, suppress the "invalid bytes" diagnostic or only print the top-level "Failed to parse input." message, matching C++ protoc's behavior.

### Run 136 — Decode mode sint32 zigzag with oversized varint (VICTORY)
- **Bug**: Go's `--decode` mode uses the full 64-bit varint value for `sint32` zigzag decoding, while C++ protoc truncates to uint32 first. When a sint32 field has a varint that exceeds 32 bits (e.g., 0x100000001), C++ reads it as uint32(0x01) then zigzag-decodes to -1. Go zigzag-decodes the full uint64 value 0x100000001 to -2147483649.
- **Test**: Decode test `decode@sint32_truncate` — stdout mismatch (1 test fails).
- **Root cause**: Go's `printKnownField()` calls `protowire.DecodeZigZag(e.varint)` on the raw uint64 varint value without truncating to uint32 first. C++ protoc's `WireFormatLite::ReadSInt32` internally calls `ReadVarint32` which truncates the varint to 32 bits, then calls `ZigZagDecode32`. The different truncation order produces different values: Go gets DecodeZigZag(0x100000001) = -2147483649, C++ gets ZigZagDecode32(uint32(0x100000001)) = ZigZagDecode32(1) = -1.
- **C++ protoc**: `value: -1` (truncate to uint32, then zigzag decode).
- **Go protoc-go**: `value: -2147483649` (zigzag decode full uint64).
- **Fix hint**: In `printKnownField()` for `TYPE_SINT32`, truncate to uint32 before zigzag: `protowire.DecodeZigZag(uint64(uint32(e.varint)))` instead of `protowire.DecodeZigZag(e.varint)`. Same may apply to `TYPE_INT32` and `TYPE_UINT32` — they should also truncate to 32 bits.

### Run 137 — Decode mode keeps unknown fields in map entries (VICTORY)
- **Bug**: Go's `--decode` mode prints unknown fields inside map entries, while C++ protoc strips them. When a map entry's wire data contains an unknown field (field number other than 1=key, 2=value), C++ protoc's `DynamicMessage::ParseFromString` discards the unknown field during map parsing, producing clean `key`/`value`-only output. Go just recursively decodes the map entry bytes as a regular sub-message, printing all fields including unknowns.
- **Test**: Decode test `decode@map_unknown` — stdout mismatch (1 test fails). Proto in `testdata/468_decode_map_unknown/test.proto`. Hex data: `0a080a026869102a180112 0178` (field 1 map entry with key="hi" value=42 plus unknown field 3=1, field 2 string "x").
- **Root cause**: `printTextProto()` in cli.go treats map entries like any other sub-message. When recursing into a TYPE_MESSAGE field with `GetOptions().GetMapEntry() == true`, it calls `printTextProto` on the raw bytes, which decodes ALL wire fields including unknowns. C++ protobuf's map implementation parses map entries into a MapEntry message that only has key/value fields — any extra fields are silently discarded during `ParseFromString`.
- **C++ protoc**: `data {\n  key: "hi"\n  value: 42\n}\nlabel: "x"\n` (no unknown field).
- **Go protoc-go**: `data {\n  key: "hi"\n  value: 42\n  3: 1\n}\nlabel: "x"\n` (unknown field `3: 1` printed).
- **Fix hint**: In `printTextProto`, when recursing into a sub-message that is a map entry (`msgDesc.GetOptions().GetMapEntry() == true`), suppress unknown fields in the output. Either: (1) after collecting knownEntries and unknownEntries for the sub-message, clear unknownEntries if the message is a map entry, or (2) pass a flag to `printTextProto` indicating it's inside a map entry and should skip unknown field printing.

### Run 139 — Null byte in comment not rejected by Go tokenizer (VICTORY)
- **Bug**: Go's tokenizer does not validate for control characters (like null byte `\x00`) in comments or general text. C++ protoc's tokenizer validates all input characters and rejects files containing control characters (other than whitespace `\n`, `\r`, `\t`) with "Invalid control characters encountered in text." Go silently accepts the null byte inside a line comment and parses the file successfully.
- **Test**: `469_null_in_comment` — all 9 profiles fail.
- **Root cause**: C++ `Tokenizer::Next()` calls `ReadChar()` which checks `current_char_ == '\0'` and invalid control character ranges, producing error messages. Go's `tokenizer.go` `readLineCommentText()` and `readBlockCommentText()` simply consume all bytes until the comment terminator (`\n` or `*/`) without validating that each byte is a valid character.
- **C++ protoc**: `test.proto:3:9: Invalid control characters encountered in text.` + `test.proto:3:10: Expected top-level statement (e.g. "message").` → exit 1.
- **Go protoc-go**: Silently accepts the file, produces valid descriptor → exit 0.
- **Fix hint**: In the tokenizer, add character validation similar to C++ `ReadChar()` — reject null bytes and other control characters (except `\n`, `\r`, `\t`, and maybe `\f` which was handled in Run 29). Specifically, in `readLineCommentText()` and `readBlockCommentText()`, check each byte and emit an error for control characters.

### Run 140 — Go emits spurious "Integer out of range" error for hex literal with no digits (VICTORY)
- **Bug**: Go's tokenizer/parser emits an extra "Integer out of range." error before the correct "0x must be followed by hex digits." error when processing a hex literal `0x` with no hex digits after it. C++ protoc only emits the hex-digits error. The spurious error is at a different column (column 36 pointing at the default value position) while the hex error is at column 38.
- **Test**: `470_hex_no_digits` — all 9 profiles fail (error mismatch: Go emits 2 errors, C++ emits 1).
- **Root cause**: When `0x` is encountered, the tokenizer correctly detects the missing hex digits and adds the hex-digits error. But either the tokenizer records `0x` as an integer token with value 0, or the parser attempts to parse the token and triggers a separate range check that produces the spurious "Integer out of range." error. The C++ tokenizer just emits the hex-digits error and doesn't attempt further validation of the malformed literal.
- **C++ protoc**: `test.proto:6:38: "0x" must be followed by hex digits.`
- **Go protoc-go**: `test.proto:6:36: Integer out of range.` + `test.proto:6:38: "0x" must be followed by hex digits.`
- **Fix hint**: In the tokenizer or parser, when a `0x` literal has no hex digits, don't also emit an "Integer out of range" error. The hex-digits error is sufficient. Either suppress the range check when the hex error has already been reported, or ensure the token value is set to a valid default (like 0) so the range check passes.

### Run 142 — --encode mode silently skipped, produces wrong error (VICTORY)
- **Bug**: Go's parseArgs() silently skips `--encode=TYPE` (cli.go:1084 `continue`), so `--encode` never sets any state. When a valid proto file is provided with `--encode=basic.Person`, C++ protoc enters encode mode (reads text format from stdin, writes binary to stdout, exits 0). Go ignores `--encode`, sees no output directives, and errors with "Missing output directives." (exit 1).
- **Test**: CLI test `cli@encode_with_proto` — exit code mismatch (C++ 0, Go 1).
- **Root cause**: `parseArgs()` at cli.go:1084 has `if strings.HasPrefix(arg, "--encode=") { continue }` — the flag is parsed but discarded without setting any encode mode state. C++ protoc's CommandLineInterface recognizes `--encode` and enters a completely different code path that reads text format from stdin, looks up the message type in the parsed descriptors, encodes it to binary, and writes to stdout.
- **C++ protoc**: Enters encode mode, parses empty text format input, outputs empty binary, exits 0.
- **Go protoc-go**: Ignores `--encode`, falls through to "Missing output directives." error, exits 1.
- **Fix hint**: Implement encode mode: (1) parse `--encode=TYPE` and store the type name, (2) after building descriptors, look up the message type, (3) read text format from stdin, (4) encode to binary protobuf, (5) write to stdout. Similar to `--decode` but in reverse. Use `prototext.Unmarshal` or a custom text format parser, then `proto.Marshal`.

### Run 143 — ExtensionRangeOptions source-retention not stripped from descriptor (VICTORY)
- **Bug**: Go's `stripMsgSourceRetention` in `cli.go` strips source-retention options from FileOptions, MessageOptions, FieldOptions, EnumOptions, EnumValueOptions, ServiceOptions, MethodOptions, OneofOptions — but completely ignores ExtensionRangeOptions. When an extension range has a custom option with `retention = RETENTION_SOURCE`, C++ protoc strips the option from the descriptor output. Go keeps it.
- **Test**: `471_ext_range_retention` — 7 profiles fail (descriptor_set, descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: `stripMsgSourceRetention()` at cli.go:4847 handles fields, oneofs, extensions, nested enums, and nested messages, but has no code for `msg.GetExtensionRange()`. The extension range options (accessible via `er.GetOptions()`) are never checked for source-retention fields, so `ExtensionRangeOptions` unknowns with `RETENTION_SOURCE` field numbers are kept in the descriptor.
- **C++ protoc**: Produces 67-byte descriptor without the `(range_debug)` option value (stripped due to `RETENTION_SOURCE`).
- **Go protoc-go**: Produces 83-byte descriptor with the `(range_debug)` option value still present — 16 bytes larger.
- **Fix hint**: In `stripMsgSourceRetention`, add a loop over `msg.GetExtensionRange()` similar to the field/oneof loops: `for ri, er := range msg.GetExtensionRange() { if stripOptsUnknowns(er.GetOptions(), srcRetFields[".google.protobuf.ExtensionRangeOptions"], append(append([]int32{}, msgPath...), 5, int32(ri), 3), strippedPaths) { er.Options = nil } }`. Also need to add `ExtensionRangeOptions` to `fdHasSourceRetentionOpts` checks if not already there.

### Run 144 — Encode mode missing detailed error message for invalid text format (VICTORY)
- **Bug**: Go's `--encode` mode only prints `"Failed to parse input."` to stderr when the text format input has errors (e.g., unknown field names). C++ protoc also prints a detailed error message with the source location and specific error description before the generic "Failed to parse input." message. Go swallows the detailed error from `prototext.Unmarshal` and only prints the generic message.
- **Test**: CLI test `cli@encode_bad_field` — stderr mismatch (1 test fails). Proto in `testdata/472_encode_error/test.proto`. Input: `bad_field: 42`.
- **Root cause**: `runEncode()` at cli.go:8791-8794 catches the `prototext.Unmarshal` error but only prints `fmt.Fprintln(os.Stderr, "Failed to parse input.")`. The actual error from `prototext.Unmarshal` contains detailed information like `(line 1:10): Message type "encerr.Foo" has no field named "bad_field"` but Go discards it. C++ protoc's `TextFormat::Parser::Parse` prints detailed diagnostics AND then the outer encode loop prints "Failed to parse input."
- **C++ protoc**: stderr: `input:1:10: Message type "encerr.Foo" has no field named "bad_field".\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed error line.
- **Fix hint**: Before the generic "Failed to parse input." message, parse and print the error from `prototext.Unmarshal`. The error message format needs to be converted from Go's prototext format (`(line 1:10): ...`) to C++ protoc's format (`input:1:10: ...`). Extract line/column from the error, then print `fmt.Fprintf(os.Stderr, "input:%d:%d: %s\n", line, col, message)` followed by the generic "Failed to parse input." message.
- **Also affects**: ANY invalid text format input to `--encode` mode will produce less helpful error messages in Go than in C++. This includes: unknown fields, type mismatches, syntax errors in text format, etc.

### Run 145 — Aggregate option float-for-integer error message falls through to generic format (VICTORY)
- **Bug**: Go's `formatAggregateError()` handles specific error types (`aggregateDupFieldError`, `aggregateBoolError`, `aggregatePositiveSignError`, `aggregateIntRangeError`, `aggregateStringExpectedError`, `aggregateFloatExpectedError`, `aggregateEnumError`) with proper `file:line:col: Error while parsing option value for "name": ...` formatting. But when a float literal like `3.14` is used for an integer field, `encodeCustomOptionValue` returns a generic `fmt.Errorf("invalid integer value: %s", value)` — not one of the recognized error types. This falls through to the generic fallback at line 8245: `"%s: error encoding custom option: %v"` which lacks line/column info and uses different wording.
- **Test**: `473_agg_float_for_int` — all 9 profiles fail (error message mismatch).
- **Root cause**: `encodeCustomOptionValue()` at cli.go:7666-7668 returns `fmt.Errorf("invalid integer value: %s", value)` for int32/int64/sint32/sint64 fields when `strconv.ParseInt` fails. This is a plain error, not a typed error struct. `formatAggregateError()` at line 8209-8245 has no `errors.As` case for it, so the error falls through to line 8245's generic format without line/column info.
- **C++ protoc**: `test.proto:15:16: Error while parsing option value for "cfg": Expected integer, got: 3.14`
- **Go protoc-go**: `test.proto: error encoding custom option: field count: invalid integer value: 3.14`
- **Fix hint**: Create a new error type like `aggregateExpectedIntegerError` with the value that was found, and return it from `encodeCustomOptionValue` for int parse failures. Add `errors.As` matching in `formatAggregateError`. The error message should be `Expected integer, got: 3.14` to match C++.
- **Also affects**: Same issue exists for uint32/uint64 parse failures at line 7687-7688 (`invalid unsigned integer value`), fixed32/sfixed32/fixed64/sfixed64 parse failures, and any other `fmt.Errorf` returns from `encodeCustomOptionValue` that aren't covered by the specific error types.

### Run 146 — Go ignores --descriptor_set_in flag, can't resolve imports from pre-compiled descriptors (VICTORY)
- **Bug**: Go's `parseArgs()` silently skips `--descriptor_set_in=FILES` (cli.go:1092 `continue`), so pre-compiled descriptor sets are never loaded. When a .proto file imports another .proto that only exists in the descriptor_set_in (not on the proto_path), C++ protoc resolves the import from the pre-compiled descriptors and succeeds. Go ignores the flag and fails with "File not found" errors.
- **Test**: CLI test `cli@descriptor_set_in_import` — exit code mismatch (C++ 0, Go 1).
- **Root cause**: `parseArgs()` at cli.go:1092-1094 has `if strings.HasPrefix(arg, "--descriptor_set_in=") { continue }` — the flag is parsed but discarded without loading any descriptors. C++ protoc's `CommandLineInterface` recognizes `--descriptor_set_in`, reads the specified binary FileDescriptorSet files, and makes them available as pre-parsed imports. This allows resolving imports without the source .proto files being on disk.
- **C++ protoc**: Successfully compiles `test.proto` that imports `dep.proto` from the descriptor_set_in file (exit 0).
- **Go protoc-go**: Fails with `dep.proto: File not found.` + `Import "dep.proto" was not found or had errors.` + `"dep.Dep" is not defined.` (exit 1).
- **Fix hint**: Parse `--descriptor_set_in` value, read the binary file(s), unmarshal as `FileDescriptorSet`, and inject the contained `FileDescriptorProto` entries into the parsed file map before resolving imports. The descriptor_set_in files may be delimited by `:` (or `;` on Windows).
- **Test setup**: `testdata/474_descriptor_set_in/dep.proto` compiled to `dep.pb`, `testdata/474_descriptor_set_in/main/test.proto` imports `dep.proto` but dep.proto is NOT on the import path.

### Run 147 — Encode mode outputs fields in text input order instead of field number order (VICTORY)
- **Bug**: Go's `--encode` mode serializes fields in the order they appear in the text format input, instead of canonical field number order. C++ protoc always serializes in field number order regardless of input order. When `id: 42 name: "Alice"` is provided (field 2 before field 1), C++ outputs field 1 first, Go outputs field 2 first.
- **Test**: CLI test `cli@encode_field_order` — stdout mismatch (1 test fails).
- **Root cause**: `runEncode()` uses `prototext.Unmarshal` to parse text input into a `dynamicpb.Message`, then `proto.Marshal` to serialize. For dynamic messages, `proto.Marshal` iterates fields using `Range()`, which in `dynamicpb` returns fields in the order they were set (i.e., the order `prototext.Unmarshal` encountered them). C++ protoc's `Message::SerializeToString` always serializes in field number order.
- **C++ protoc**: stdout binary = `\x0a\x05Alice\x10\x2a` (field 1=name first, then field 2=id).
- **Go protoc-go**: stdout binary = `\x10\x2a\x0a\x05Alice` (field 2=id first, then field 1=name).
- **Fix hint**: Use `proto.MarshalOptions{Deterministic: true}` which guarantees field number ordering. Or sort the dynamic message fields before marshaling. Alternatively, iterate the message descriptor's fields in number order and re-set them on a fresh dynamic message.

### Run 148 — Go missing unused import warning (VICTORY)
- **Bug**: Go does NOT emit a warning when a file imports another proto file but never uses any types from it. C++ protoc produces `warning: Import foo.proto is unused.` on stderr (exit 0). Go silently accepts the unused import with no warning.
- **Test**: CLI test `cli@unused_import` — stderr mismatch (1 test fails). Proto in `testdata/475_unused_import/test.proto` imports `dep.proto` but never references any type from it.
- **Root cause**: Go has no unused import detection at all — no code in the compiler checks whether imported files are actually referenced by the importing file. C++ protoc tracks type usage and emits warnings for imports that contribute no symbols.
- **C++ protoc**: stderr: `testdata/475_unused_import/test.proto:4:1: warning: Import dep.proto is unused.` (exit 0).
- **Go protoc-go**: stderr: (empty) (exit 0).
- **Fix hint**: After validation, iterate each file's `Dependency` list. For each dependency, check whether any type from it is referenced (field types, method input/output types, extension extendee, option types, etc.). If none, emit `warning: Import %s is unused.` with location from source code info path [3, depIdx]. For `import public`, the transitive re-exports also count as usage.
- **Also affects**: ALL proto files with unused imports will silently compile without warning in Go. This includes unused weak imports too.

### Run 149 — Encode mode oneof conflict error message missing (VICTORY)
- **Bug**: Go's `--encode` mode does not report the specific oneof conflict error that C++ protoc produces. When two fields in the same oneof are both set in text format input (`name: "hello" id: 42`), C++ prints `input:1:17: Field "id" is specified along with field "name", another member of oneof "choice".` before `Failed to parse input.` Go only prints the generic `Failed to parse input.` without identifying the conflicting fields.
- **Test**: CLI test `cli@encode_oneof_conflict` — stderr mismatch (1 test fails). Proto in `testdata/476_encode_oneof_conflict/test.proto` has message with oneof.
- **Root cause**: `runEncode()` uses `prototext.Unmarshal` which returns a Go error, then `reformatProtoTextErrors()` only handles "unknown field" errors (regex: `\(line (\d+):(\d+)\): unknown field: (\w+)`). It doesn't handle oneof conflict errors. The Go prototext library produces a different error format for oneof conflicts that is not caught and reformatted.
- **C++ protoc**: stderr: `input:1:17: Field "id" is specified along with field "name", another member of oneof "choice".\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed oneof conflict message.
- **Fix hint**: Add another regex case in `reformatProtoTextErrors()` to detect oneof conflict errors from Go's prototext library and reformat them to match C++ format: `input:LINE:COL: Field "FIELD2" is specified along with field "FIELD1", another member of oneof "ONEOF".`

### Run 150 — Encode mode extension fields placed before regular fields in binary output (VICTORY)
- **Bug**: Go's `--encode` mode outputs extension fields BEFORE regular fields in the binary encoding, violating canonical field-number ordering. When encoding `name: "ab" [encextord.extra]: 5` with field 1 (name, string) and field 3 (extra, extension int32), C++ outputs field 1 first then field 3. Go outputs field 3 first then field 1.
- **Test**: CLI test `cli@encode_ext_order` — stdout mismatch (1 test fails). Proto in `testdata/477_encode_ext_order/test.proto` has proto2 message with extensions.
- **Root cause**: `runEncode()` uses `dynamicpb.NewMessage(msgDesc)` and `prototext.Unmarshal` to parse the text format, then `proto.MarshalOptions{Deterministic: true}` to serialize. With `dynamicpb`, extensions are stored separately from regular fields. When marshaling, Go's proto library outputs regular fields first in field-number order, then extension fields — but if prototext sets extensions via the dynamic message's extension mechanism, they may be iterated in a different order than regular fields. The result is extension fields appear before regular fields in the output.
- **C++ protoc**: stdout binary = `\x0a\x02ab\x18\x05` (field 1 first, field 3 second).
- **Go protoc-go**: stdout binary = `\x18\x05\x0a\x02ab` (field 3 first, field 1 second).
- **Fix hint**: After unmarshaling, iterate all fields and extensions in field-number order and rebuild the message, or manually sort the serialized output by tag. Alternatively, use a custom marshal that handles dynamic messages with extensions in canonical order. Could also post-process the wire format to reorder tags.
- **Also affects**: Any `--encode` input with extension fields will have non-canonical field ordering in Go's output.

### Run 151 — Encode mode rejects invalid UTF-8 in string field, C++ only warns (VICTORY)
- **Bug**: Go's `--encode` mode fails with "Failed to parse input." (exit 1) when a string field contains invalid UTF-8 bytes via octal escape (e.g., `\377`). C++ protoc accepts it with a warning (exit 0) and produces the binary output. The exit code and stderr both differ.
- **Test**: CLI test `cli@encode_invalid_utf8` — exit code mismatch (1 test fails). Proto in `testdata/478_encode_invalid_utf8/test.proto`.
- **Root cause**: `runEncode()` uses Go's `prototext.Unmarshal` which strictly validates UTF-8 for `TYPE_STRING` fields and rejects invalid sequences. C++ protoc's text format parser allows any bytes in string fields and only issues a warning during serialization via `WireFormatLite::VerifyUtf8String`.
- **C++ protoc**: exit 0, stderr warning "String field contains invalid UTF-8 data", stdout has valid binary.
- **Go protoc-go**: exit 1, stderr "Failed to parse input.", no stdout.
- **Fix hint**: Either (1) use `bytes` mode in prototext unmarshal for string fields, (2) post-process the unmarshaled message to allow invalid UTF-8 like C++ does, or (3) catch the UTF-8 error from prototext.Unmarshal and issue a warning instead of failing, then re-attempt with `bytes`-mode handling. Most compatible fix: custom text format parser that doesn't validate UTF-8 for string fields, matching C++ behavior.
- **Also affects**: Any `--encode` input with string fields containing non-UTF-8 bytes (e.g., Latin-1, raw binary data via octal/hex escapes).

### Run 152 — Decode mode omits default values for missing map entry fields (VICTORY)
- **Bug**: Go's `--decode` mode does not print default values for missing fields within map entries. When a map entry's wire data is missing the key field, C++ protoc prints `key: ""` (the default value for a string key), while Go omits the key line entirely. Same for missing value fields — C++ prints `value: 0`, Go omits it.
- **Test**: Decode test `decode@map_missing_key` — stdout mismatch (1 test fails). Proto in `testdata/480_decode_map_missing_key/test.proto`. Hex data: `0A02102A12026F6B` (map entry with only value=42, no key field; then label="ok").
- **Root cause**: Go's `printTextProto()` only prints fields that are actually present in the wire data. For map entries, if a field (key or value) is absent from the wire bytes, it's simply not printed. C++ protoc's `DynamicMessage::ParseFromString` fills in default values for all fields including missing map entry fields, then `TextFormat::Print` outputs all fields including those with default values.
- **C++ protoc**: `data {\n  key: ""\n  value: 42\n}\nlabel: "ok"\n` (missing key printed as default empty string).
- **Go protoc-go**: `data {\n  value: 42\n}\nlabel: "ok"\n` (missing key omitted entirely).
- **Fix hint**: When decoding a map entry sub-message (detected by `msgDesc.GetOptions().GetMapEntry() == true`), after collecting all wire entries, check if field 1 (key) or field 2 (value) are missing. If so, synthesize entries with default values: empty string for string keys, 0 for integer keys/values, false for bool keys, empty bytes for bytes values, first enum value name for enum values. Then include them in the sorted output.
- **Also affects**: ANY map field decoded with `--decode` where the wire data has map entries with missing key or value fields. This is a valid (if unusual) protobuf encoding — parsers must fill in defaults for absent fields.

### Run 153 — Decode mode prints default-valued fields in proto3 that C++ omits (VICTORY)
- **Bug**: Go's `--decode` mode prints ALL fields found in the raw wire data, including fields with default values (0, "", empty bytes, false) for proto3 messages. C++ protoc's `--decode` parses into a DynamicMessage then uses TextFormat::Print which respects proto3 implicit presence — fields equal to their default are NOT considered "set" and are omitted from the text output.
- **Test**: Decode test `decode@proto3_default` — stdout mismatch (1 test fails). Proto in `testdata/481_decode_proto3_default/test.proto`. Hex data: `080012001a0020002a026f6b` (id=0, name="", data="", flag=false, label="ok").
- **Root cause**: Go's `printTextProto()` iterates over the raw wire data and prints every field it encounters, regardless of proto3 presence semantics. It has no awareness of whether a field value equals the default for its type. C++ uses `Message::ParseFromString` which populates a `DynamicMessage`, where proto3 fields with default values are NOT tracked as "set". Then `TextFormat::Print` only outputs set fields.
- **C++ protoc**: `label: "ok"\n` (only the non-default field is printed).
- **Go protoc-go**: `id: 0\nname: ""\ndata: ""\nflag: false\nlabel: "ok"\n` (all 5 fields printed, including 4 with default values).
- **Fix hint**: In `printTextProto`, after collecting wire entries and before printing each known field, check if the field has proto3 implicit presence (syntax == "proto3" AND field is not `optional` AND not inside a oneof). If so, skip printing when the value equals the default: varint==0 for int/bool/enum, empty bytes for string/bytes, float/double bits==0. Don't skip for `optional` proto3 fields (which have explicit presence).
- **Also affects**: ANY proto3 message decoded with `--decode` where the wire data contains fields with default values. Also affects editions with implicit presence (`field_presence = IMPLICIT`).

### Run 154 — Encode mode sorts map entries by key instead of preserving input order (VICTORY)
- **Bug**: Go's `--encode` mode sorts map entries alphabetically by key, while C++ protoc preserves the text format input order. When encoding `tags { key: "c" value: 3 } tags { key: "a" value: 1 } tags { key: "b" value: 2 }`, C++ outputs entries as c, a, b (input order), Go outputs a, b, c (sorted).
- **Test**: CLI test `cli@encode_map_order` — stdout mismatch (1 test fails). Proto in `testdata/482_encode_map_order/test.proto`.
- **Root cause**: `runEncode()` uses `proto.MarshalOptions{Deterministic: true}` which sorts map entries by key. C++ protoc's `Message::SerializeToString` outputs map entries in hash-map iteration order, which for `TextFormat::Parse` input preserves the insertion (input) order. The `Deterministic: true` flag is used to get canonical field-number ordering (needed for regular fields, as found in Run 147), but it also sorts map entries — which C++ does NOT do.
- **C++ protoc**: stdout binary has map entries in input order: c(3), a(1), b(2).
- **Go protoc-go**: stdout binary has map entries sorted by key: a(1), b(2), c(3).
- **Fix hint**: Cannot simply remove `Deterministic: true` because that would break field-number ordering (Run 147). Instead, need to either: (1) manually serialize — iterate message descriptor fields in field-number order, and for map fields output entries in the order they were parsed, or (2) post-process the dynamic message to record insertion order of map entries and use a custom marshal, or (3) use `Deterministic: false` but manually sort non-map fields by number. The fundamental issue is that Go's `proto.Marshal` with `Deterministic: true` conflates "canonical field order" with "sorted map keys", while C++ separates these concerns.
- **Also affects**: ANY `--encode` input with map fields will produce differently-ordered output in Go vs C++. Map entries are semantically unordered in protobuf, but the wire bytes differ, causing comparison failures.

### Run 155 — Encode mode missing detailed syntax error message (VICTORY)
- **Bug**: Go's `--encode` mode drops the detailed syntax error message from `prototext.Unmarshal`. When text format input has a syntax error (e.g., `name "Alice"` missing the `:` separator), C++ protoc prints a detailed error like `input:1:6: Expected ":", found ""Alice"".` followed by `Failed to parse input.`. Go only prints `Failed to parse input.` without the detailed error line.
- **Test**: CLI test `cli@encode_missing_colon` — stderr mismatch (1 test fails).
- **Root cause**: `reformatProtoTextErrors()` at cli.go:9478 only handles the "unknown field" regex pattern: `\(line (\d+):(\d+)\): unknown field: (\w+)`. All other prototext errors (syntax errors, type mismatches, etc.) fall through unhandled. The Go prototext library returns an error like `proto: syntax error (line 1:1): missing field separator :` but `reformatProtoTextErrors` doesn't match it, so no detailed error is printed to stderr before the generic "Failed to parse input." message.
- **C++ protoc**: `input:1:6: Expected ":", found ""Alice"".` + `Failed to parse input.` (exit 1).
- **Go protoc-go**: `Failed to parse input.` only (exit 1). Missing the detailed error line.
- **Fix hint**: Extend `reformatProtoTextErrors` to handle additional prototext error patterns. At minimum, add a fallback case that extracts the `(line L:C)` location and error message from the Go error string and reformats it as `input:L:C: MESSAGE`. Or, print the raw error to stderr as a fallback when no specific pattern matches. The C++ format is `input:LINE:COL: DESCRIPTION`.
- **Also affects**: ALL `--encode` mode text format parse errors beyond "unknown field" — missing separators, unexpected tokens, invalid values, malformed text format, etc. — will all be silently dropped, showing only the generic "Failed to parse input." message.

### Run 156 — Go silently ignores --dependency_out flag, doesn't write file or error on bad path (VICTORY)
- **Bug**: Go's `parseArgs()` silently skips `--dependency_out=FILE` (cli.go:1406 `continue`), so the dependency output file is never written. When the specified path is invalid (nonexistent directory), C++ protoc fails with an error and exit code 1, while Go silently succeeds with exit code 0. Even when the path IS valid, C++ writes the file and Go doesn't — but only the invalid-path case produces a detectable difference (exit code mismatch).
- **Test**: CLI test `cli@dependency_out_bad_path` — exit code mismatch (C++ 1, Go 0).
- **Root cause**: `parseArgs()` at cli.go:1406-1408 has `if strings.HasPrefix(arg, "--dependency_out=") { continue }` — the flag is parsed but discarded. C++ protoc's `CommandLineInterface::Run()` opens the dependency output file path and writes the Make-format dependency rule (`output: input1 input2 ...`) after successful compilation. If the file can't be opened/written, C++ fails with an OS error.
- **C++ protoc**: stderr: `/nonexistent_nelsontest_dir/deps.txt: No such file or directory` (exit 1).
- **Go protoc-go**: stderr: (empty) (exit 0).
- **Fix hint**: Parse `--dependency_out=FILE` value, store in `cfg.dependencyOut`. After successful compilation, write a Makefile-format dependency rule: `<output_files>: <input_proto_files>\n`. If the file can't be written, emit the OS error and return failure. The format is typically `<descriptor_set_out_or_plugin_out>: <proto_files>`.
- **Also affects**: ANY use of `--dependency_out` will silently be ignored. Build systems (like Bazel, Make) that use `--dependency_out` for incremental builds will get no dependency file from Go protoc, potentially causing stale builds.

### Run 157 — Bool-keyed map entries reordered in encode mode (VICTORY)
- **Bug**: Go's `reorderMapEntriesBySource` fails to reorder `map<bool, string>` entries because `extractBinaryMapKeyStr` returns "0"/"1" (varint representation) while `extractTextMapKeys` returns "true"/"false" (text format representation). The key strings don't match, so the reordering function can't correlate binary entries with their source-order positions.
- **Test**: `483_encode_bool_map_order` — cli@encode_bool_map_order fails.
- **Root cause**: `extractBinaryMapKeyStr()` at cli.go:9142 handles varint keys with `fmt.Sprintf("%d", v)`, returning "0" or "1" for bool keys. But `extractMapKeyFromEntry()` returns "true" or "false" from the text format. When `reorderWireEntriesByKeys` tries to match source keys ["true","false"] against binary keys ["1","0"], none match, so no reordering happens and Go's default sort order (false before true) is preserved.
- **C++ protoc**: Preserves text format insertion order: `true→"yes"` first, then `false→"no"`.
- **Go protoc-go**: Outputs `false→"no"` first, then `true→"yes"` (Go map iteration / deterministic sort order).
- **Fix hint**: In `extractBinaryMapKeyStr`, for varint field 1 where the map key type is bool, return "true"/"false" instead of "1"/"0". Alternatively, in `extractTextMapKeys`, normalize "true" to "1" and "false" to "0". Or in `reorderWireEntriesByKeys`, add bool normalization when matching keys.
- **Also affects**: Any `map<bool, T>` field in encode mode will have wrong entry ordering.

### Run 158 — Decode mode doesn't suppress defaults for editions IMPLICIT presence (VICTORY)
- **Bug**: Go's `--decode` mode only suppresses default-valued fields for proto3 messages. For editions files with `features.field_presence = IMPLICIT`, default-valued fields should also be suppressed — but Go prints them anyway. C++ protoc correctly suppresses default-valued fields for both proto3 and editions IMPLICIT presence.
- **Test**: Decode test `decode@edition_implicit_default` — stdout mismatch (1 test fails).
- **Root cause**: `findMessageType()` at cli.go:9888 only calls `collectNoPresenceMsgs()` when `isProto3` is true. For editions files (`fd.GetSyntax() == "editions"`), the function is never called, so `noPresenceMsgs` map doesn't include messages from editions files with `field_presence = IMPLICIT`. Then `printTextProto()` at line 10510 checks `noPresenceMsgs[msgFQN]` which is false for editions messages, and default values are printed.
- **C++ protoc**: `label: "ok"\n` (suppresses id=0, name="", flag=false as default values for IMPLICIT presence).
- **Go protoc-go**: `id: 0\nname: ""\nflag: false\nlabel: "ok"\n` (prints all 4 fields including 3 with default values).
- **Fix hint**: In `findMessageType()`, for editions files, check if the file (or individual fields) have `field_presence = IMPLICIT` (feature resolution). If so, call `collectNoPresenceMsgs` for those messages. Need to handle feature inheritance: file-level features apply to all messages, message-level features override, field-level features override further. For editions, check `fd.GetOptions().GetFeatures().GetFieldPresence()` == `IMPLICIT` at the file level, then per-message, then per-field.
- **Also affects**: Any editions file with `field_presence = IMPLICIT` decoded with `--decode` will print default-valued fields that C++ omits. Also affects per-message and per-field `features.field_presence = IMPLICIT` overrides.

### Run 159 — Decode mode packed repeated enum doesn't move unknown values to unknown fields (VICTORY)
- **Bug**: Go's decode mode unpacks packed repeated enum fields into individual varint entries and adds them ALL to `knownEntries` without checking for closed enum unknown values. For proto2 (closed enums), unknown enum values in packed data should be moved to the unknown field set, but Go keeps them as known fields and prints the numeric value.
- **Test**: Decode test `decode@packed_enum_unknown` — stdout mismatch (1 test fails).
- **Root cause**: Packed unpacking at cli.go:10363-10406 always appends to `knownEntries`. The closed enum check at lines 10409-10417 only applies to non-packed entries (in the `else if` branch). So packed enum entries bypass the closed enum unknown value check entirely. Run 126 fixed the non-packed case but the packed case was missed.
- **C++ protoc**: `s: UNKNOWN\ns: ACTIVE\nlabel: "ok"\n1: 99\n` (moves unknown value 99 to unknown fields, printed as `1: 99`).
- **Go protoc-go**: `s: UNKNOWN\ns: ACTIVE\ns: 99\nlabel: "ok"\n` (keeps unknown value as known field, printed as `s: 99`).
- **Fix hint**: After unpacking each packed varint entry for an enum type, check if the value is in the closed enum's value map. If not, add it to `unknownEntries` instead of `knownEntries`. Add the same check as lines 10409-10417 inside the packed unpacking loop (around line 10403-10405).

### Run 160 — Encode mode drops detailed error for duplicate non-repeated field (VICTORY)
- **Bug**: Go's `--encode` mode swallows the detailed error message when a non-repeated field is specified multiple times in text format input. C++ protoc reports the field name and location; Go only prints the generic "Failed to parse input." message.
- **Test**: CLI test `cli@encode_dup_field` — stderr mismatch (1 test fails).
- **Root cause**: `reformatProtoTextErrors()` at cli.go:9527 only handles two error patterns: "unknown field" and "missing field separator". When `prototext.Unmarshal` returns a duplicate-field error (different pattern), `reformatProtoTextErrors` doesn't match it and returns without printing anything specific. Only the generic "Failed to parse input." is printed afterward.
- **C++ protoc**: stderr: `input:1:23: Non-repeated field "id" is specified multiple times.\nFailed to parse input.`
- **Go protoc-go**: stderr: `Failed to parse input.`
- **Fix hint**: Add a new regex in `reformatProtoTextErrors` to match Go's duplicate field error pattern (something like `non-repeated field "X" is already set` or similar from prototext) and reformat it to match C++ format: `input:L:C: Non-repeated field "NAME" is specified multiple times.`

### Run 161 — Encode mode double NaN bit pattern differs from C++ (VICTORY)
- **Bug**: Go's `--encode` mode uses Go's canonical NaN bit pattern (`0x7FF8000000000001`) for `double` fields, while C++ protoc uses `0x7FF8000000000000`. The encode mode uses `prototext.Unmarshal` from Go's standard protobuf library, which produces Go's NaN. This is the same root cause as Run 3 (custom option NaN), but in the encode code path which uses a completely different mechanism (Go standard library's `prototext` package vs custom option encoding).
- **Test**: CLI test `cli@encode_nan` — stdout (binary output) mismatch (1 test fails).
- **Root cause**: `prototext.Unmarshal` in Go's `google.golang.org/protobuf/encoding/prototext` package parses `nan` into Go's canonical `math.NaN()` which is `float64(0x7FF8000000000001)`. C++ protoc's text format parser uses `strtod("nan", ...)` which returns `0x7FF8000000000000`. The one-bit difference in the lowest mantissa bit produces different binary output.
- **C++ protoc**: `09 00 00 00 00 00 00 f8 7f` (double NaN = `0x7FF8000000000000`).
- **Go protoc-go**: `09 01 00 00 00 00 00 f8 7f` (double NaN = `0x7FF8000000000001`).
- **Fix hint**: After `prototext.Unmarshal`, walk the resulting message and replace any NaN double values with `math.Float64frombits(0x7FF8000000000000)` to match C++. Or patch the serialized bytes post-marshal. This is tricky since Go's protobuf library internally uses Go's NaN representation.
- **Also affects**: Any `--encode` with `nan` in a double field. Float NaN (`0x7FC00000`) happens to match between Go and C++, so only double is affected.

### Run 162 — Decode/encode mode "Type not defined" error message format mismatch (VICTORY)
- **Bug**: Go's `--decode` (and `--encode`) mode produces a different error message format than C++ protoc when the specified message type doesn't exist in the schema. C++ uses `Type not defined: NAME` while Go uses `Type "NAME" is not defined.` — different word order, quotes around the type name, and a trailing period.
- **Test**: CLI test `cli@decode_bad_type` — stderr mismatch (1 test fails).
- **Root cause**: `cli.go:9383` uses `fmt.Errorf("Type \"%s\" is not defined.", msgTypeName)` while C++ protoc's `command_line_interface.cc` uses `"Type not defined: " + type_name`.
- **C++ protoc**: `Type not defined: basic.NoSuchType`
- **Go protoc-go**: `Type "basic.NoSuchType" is not defined.`
- **Fix hint**: Change the error format at cli.go:9383 (and lines 9390, 9394, 9967) from `Type "%s" is not defined.` to `Type not defined: %s` to match C++.
- **Also affects**: `--encode` mode has the same bug (same code path or similar error at line 9967).

### Run 163 — Encode mode NaN not canonicalized in map double/float values (VICTORY)
- **Bug**: Go's `canonicalizeNaN()` function in `cli.go` handles NaN canonicalization for singular fields, repeated fields, and recursing into message-valued maps — but does NOT handle `double`/`float` values directly inside map fields (e.g., `map<string, double>`). When encoding `values { key: "x" value: nan }`, Go's `prototext.Unmarshal` produces Go's canonical NaN (`0x7FF8000000000001`) which is not replaced by C++ NaN (`0x7FF8000000000000`).
- **Test**: CLI test `cli@encode_map_nan` — stdout (binary output) mismatch (1 test fails). Proto in `testdata/488_encode_map_nan/test.proto`.
- **Root cause**: `canonicalizeNaN()` at cli.go:9515-9521 only recurses into map values when `fd.MapValue().Kind() == protoreflect.MessageKind`. It has no case for `protoreflect.DoubleKind` or `protoreflect.FloatKind` on map values. So NaN values in `map<K, double>` or `map<K, float>` are left as Go's NaN bit pattern.
- **C++ protoc**: NaN bytes `00 00 00 00 00 00 f8 7f` = `0x7FF8000000000000`.
- **Go protoc-go**: NaN bytes `01 00 00 00 00 00 f8 7f` = `0x7FF8000000000001`. One-bit difference in the lowest mantissa bit.
- **Fix hint**: In `canonicalizeNaN`'s map branch (lines 9515-9521), add cases for `protoreflect.DoubleKind` and `protoreflect.FloatKind`: `if fd.MapValue().Kind() == protoreflect.DoubleKind { ... check math.IsNaN(mv.Float()) and replace ... }`. Also for FloatKind. Code pattern:
  ```go
  } else if fd.IsMap() {
      v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
          switch fd.MapValue().Kind() {
          case protoreflect.MessageKind:
              canonicalizeNaN(mv.Message().(*dynamicpb.Message))
          case protoreflect.DoubleKind:
              if math.IsNaN(mv.Float()) {
                  v.Map().Set(k, protoreflect.ValueOfFloat64(math.Float64frombits(0x7FF8000000000000)))
              }
          case protoreflect.FloatKind:
              if math.IsNaN(float64(float32(mv.Float()))) {
                  v.Map().Set(k, protoreflect.ValueOfFloat32(math.Float32frombits(0x7FC00000)))
              }
          }
          return true
      })
  }
  ```
- **Also affects**: Any `--encode` with `nan` in a `map<K, double>` or `map<K, float>` field. Singular and repeated double/float NaN values ARE canonicalized (fixed in Run 161).

### Run 164 — Trailing comment before closing brace dropped from SCI (VICTORY)
- **Bug**: Go's source code info does not capture comments between the last field in a message and the closing `}` brace. C++ protoc treats such comments as `trailing_comments` on the last field's SCI entry. Go drops them entirely — the comment is not attached to any SCI location.
- **Test**: `489_trailing_comment_before_close` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: When parsing a message body, after the last field is parsed and before the closing `}` is consumed, any comments on the intervening lines should be collected as trailing comments for the last declaration. Go's comment collection logic does not assign these comments to the preceding declaration — they are consumed by the `}` token processing but never attached to any SCI entry.
- **C++ protoc**: path=[4,0,2,0] (the field `name`) has `trailing_comments: " trailing on last field before close\n"`.
- **Go protoc-go**: path=[4,0,2,0] has NO `trailing_comments`. The comment is completely lost.
- **Fix hint**: After parsing each declaration in a message body, before consuming the next token (which could be `}`), check if there are pending comments and attach them as trailing comments to the previous declaration's SCI entry. This is similar to the fix needed for Run 62 (comment after opening brace) but affects the opposite end of the scope.
- **Also affects**: Same bug likely exists for comments before `}` in enums, services, oneofs, and extend blocks — any scope where a comment appears after the last declaration and before the closing brace.

### Run 165 — Encode mode accepts numeric value for proto2 closed enum (VICTORY)
- **Bug**: Go's `--encode` mode accepts numeric integer values (like `99`) for proto2 closed enum fields, while C++ protoc rejects them with `Unknown enumeration value of "99" for field "s"`. Proto2 enums are "closed" — they only accept values defined in the enum. Go's `prototext.Unmarshal` from the standard library doesn't enforce closed enum validation, so any integer is silently accepted and encoded.
- **Test**: CLI test `cli@encode_closed_enum` — exit code mismatch (C++ 1, Go 0).
- **Root cause**: Go's `--encode` mode at `runEncode()` uses `prototext.Unmarshal` from `google.golang.org/protobuf/encoding/prototext` to parse text format input into a `dynamicpb.Message`. The Go prototext library doesn't validate enum values against the enum's value set for proto2 (closed) enums. C++ protoc's text format parser (`TextFormat::Parser::ParseFromString`) checks if the integer value is a defined enum value for closed enums, and rejects it with the error `Unknown enumeration value of "N" for field "F"`.
- **C++ protoc**: stderr: `input:1:7: Unknown enumeration value of "99" for field "s".\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: (empty) (exit 0). Silently produces binary output with enum value 99.
- **Fix hint**: After `prototext.Unmarshal`, walk the message and check that all enum fields (in proto2 files) have values that are defined in the enum type. For each enum field, check if `fd.Enum().Values().ByNumber(v.Enum())` returns nil — if so, the value is unknown and should be rejected. Alternatively, add a post-unmarshal validation step that checks `fd.Syntax() == protoreflect.Proto2` and validates enum values. Error format: `input:LINE:COL: Unknown enumeration value of "N" for field "F".\nFailed to parse input.` The line/column info may be hard to extract from Go's prototext library.
- **Also affects**: Any proto2 message with closed enum fields encoded via `--encode` mode. Also affects editions files with `features.enum_type = CLOSED` (which is the default for editions).

### Run 166 — Encode mode --deterministic_output flag is silently ignored (VICTORY)
- **Bug**: Go's `parseArgs()` parses the `--deterministic_output` flag into `cfg.deterministicOutput` (line 1402) but this field is NEVER READ anywhere in the encode logic. When `--deterministic_output --encode` is used with map fields, C++ protoc sorts map entries by key (deterministic order). Go ignores the flag entirely and preserves the text format input insertion order (from the `reorderMapEntriesBySource` logic added in Run 154's fix).
- **Test**: CLI test `cli@encode_deterministic_map` — stdout mismatch (1 test fails). Reuses proto from `testdata/482_encode_map_order/test.proto`.
- **Root cause**: `cfg.deterministicOutput` is set at cli.go:1402 but never checked in `runEncode()` or anywhere else. The encode path at line 9492 always calls `reorderMapEntriesBySource` to preserve input order, regardless of `--deterministic_output`. When the flag is set, it should skip the reordering and let `proto.MarshalOptions{Deterministic: true}` (line 9477) sort map entries by key, which is what C++ does.
- **C++ protoc**: With `--deterministic_output`, sorts map entries by key: a(1), b(2), c(3). stdout binary has entries in sorted key order.
- **Go protoc-go**: Ignores `--deterministic_output`, preserves input order: c(3), a(1), b(2). stdout binary has entries in input order.
- **Fix hint**: In `runEncode()`, pass `cfg.deterministicOutput` (need to thread it through). When true, skip the `reorderMapEntriesBySource` call at line 9492. The existing `proto.MarshalOptions{Deterministic: true}` already sorts map keys, so just don't reorder them back to source order. Something like: `if !cfg.deterministicOutput { out = reorderMapEntriesBySource(out, msgDesc, string(data)) }`.
- **Also affects**: Any `--encode --deterministic_output` with map fields will produce differently-ordered output. The flag is documented in the help text (line 44-45) but completely non-functional.

### Run 167 — --version output reports wrong version string (VICTORY)
- **Bug**: Go's `--version` flag prints `libprotoc 29.3` while C++ protoc prints `libprotoc 33.4`. The version string is hardcoded at cli.go:1336 and does not match the installed C++ protoc version. Notably, the CompilerVersion sent in CodeGeneratorRequest (plugin.go:105) correctly uses 33.4, so the version inconsistency is only in the text output.
- **Test**: CLI test `cli@version` — stdout mismatch (1 test fails).
- **Root cause**: `cli.go` line 1336: `fmt.Println("libprotoc 29.3")` is a stale hardcoded version string. The Go port was originally tracking protoc 29.3 but the system C++ protoc has been updated to 33.4. The plugin request version (plugin.go:105-110) was updated but the `--version` text was not.
- **C++ protoc**: stdout: `libprotoc 33.4` (exit 0).
- **Go protoc-go**: stdout: `libprotoc 29.3` (exit 0).
- **Fix hint**: Update line 1336 in cli.go to `fmt.Println("libprotoc 33.4")` or better yet, derive the version string from the same constants used in plugin.go (Major=6, Minor=33, Patch=4 → "33.4").

### Run 168 — --proto_path=virtual=disk mapping syntax not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--proto_path=VIRTUAL=DISK` (or `-IVIRTUAL=DISK`) syntax where `VIRTUAL` is a path prefix mapped to `DISK` directory. Go's `parseArgs()` treats the entire `VIRTUAL=DISK` string as a single literal directory path, so it fails with "directory does not exist".
- **Test**: CLI test `cli@proto_path_mapping` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails). Test data: `testdata/491_proto_path_mapping/actual/test.proto`.
- **Root cause**: `parseArgs()` at cli.go:1340-1342 does `arg[len("--proto_path="):]` which extracts the full `vdir=testdata/491_proto_path_mapping/actual` as a single string and appends it to `cfg.protoPath`. Go's `SourceTree` in `importer/importer.go` only supports plain directory paths in `Roots []string` — it has no concept of virtual-to-disk path mapping. C++ protoc's `DiskSourceTree` has `MapPath(virtual_path, disk_path)` which allows path prefix substitution.
- **C++ protoc**: Parses `vdir=actual_dir` as mapping virtual prefix `vdir` to disk path `actual_dir`. Successfully resolves `vdir/test.proto` to `actual_dir/test.proto`. Exit 0.
- **Go protoc-go**: Treats `vdir=actual_dir` as a literal directory name. Warns "directory does not exist", then fails with "Could not make proto path relative". Exit 1.
- **Fix hint**: (1) In `parseArgs()`, split `--proto_path` values on `=` to detect `VIRTUAL=DISK` mapping syntax. (2) Add a `PathMapping` type to `SourceTree` (like `map[string]string`) alongside `Roots`. (3) In `Open()`, check if the requested filename matches any virtual prefix and substitute the disk path. (4) The first `=` separates virtual from disk (the disk path itself may contain `=`).
- **Also affects**: `-I` flag (which is an alias for `--proto_path`) has the same issue.

### Run 169 — Import cycle error uses virtual filename instead of disk path (VICTORY)
- **Bug**: Go's `parseRecursive()` uses the virtual filename (e.g., `test.proto`) in import cycle error messages, while C++ protoc uses the full disk path (e.g., `testdata/492_self_import/test.proto`). The error message prefix differs.
- **Test**: CLI test `cli@self_import` — stderr mismatch (1 test fails).
- **Root cause**: At cli.go:934, `cycleStart` is set to `filename` which is the virtual filename. The error at line 944 uses `cycleStart` directly without mapping it back to the disk path via `mapErrorFilename`. The `collectErrors` at line 508-512 are joined and returned without any filename mapping. Only warnings (lines 656, 708) go through `mapErrorFilename`.
- **C++ protoc**: `testdata/492_self_import/test.proto:3:1: File recursively imports itself: test.proto -> test.proto` (disk path prefix).
- **Go protoc-go**: `test.proto:3:1: File recursively imports itself: test.proto -> test.proto` (virtual filename prefix).
- **Fix hint**: Either (1) apply `mapErrorFilename` to each error in `collectErrors` before joining at line 512, or (2) use the disk path when constructing the error at line 944 by calling `srcTree.VirtualFileToDiskFile(filename)`. Option 1 is more general — it would fix filename mapping for ALL collected errors, not just import cycle errors.
- **Also affects**: Any error in `collectErrors` that uses virtual filenames will have the same prefix mismatch. This includes "File not found" errors (line 958) and other parse/import errors.

### Run 170 — Space-separated flag value for --encode/--decode not supported by Go (VICTORY)
- **Bug**: C++ protoc supports both `--flag=VALUE` and `--flag VALUE` (space-separated) syntax for flags like `--encode`, `--decode`, `--proto_path`, etc. Go's `parseArgs()` only supports the `--flag=VALUE` form. When `--encode basic.Person` (space-separated) is used, Go fails with "Missing value for flag: --encode" while C++ correctly consumes `basic.Person` as the flag's value.
- **Test**: CLI test `cli@encode_space_flag` — exit code mismatch: C++ exit 0, Go exit 1.
- **Root cause**: `parseArgs()` in cli.go handles flags only via `strings.HasPrefix(arg, "--flag=")` pattern. When a flag like `--encode` appears without `=`, Go falls through to the end of the flag parsing loop where it hits the "Missing value for flag" error at line 1536-1541. C++ protoc's argument parser consumes the next argument as the value for flags that take a value argument.
- **C++ protoc**: Parses `--encode` + `basic.Person` as `--encode=basic.Person`. Successfully encodes with empty stdin. Exit 0.
- **Go protoc-go**: Sees `--encode` alone, reports "Missing value for flag: --encode". Exit 1.
- **Fix hint**: (1) For flags that take values (`--encode`, `--decode`, `--proto_path`, `--plugin`, etc.), when `arg` matches the flag name exactly (without `=`), consume `args[i+1]` as the value and increment `i`. (2) Alternative: refactor to use a proper flag parsing library that handles both forms.
- **Also affects**: `--decode TYPE`, `--proto_path DIR`, `--plugin NAME=PATH`, `--descriptor_set_in FILE`, `--descriptor_set_out FILE`, `--dependency_out FILE`, `--error_format FMT`, and any other flag that takes a value argument.

### Run 171 — Encode mode missing required-field warning for group fields (VICTORY)
- **Bug**: Go's encode mode doesn't emit the "Input message is missing required fields" warning when a proto2 group has a required field that's missing from the text input. C++ protoc correctly detects and warns about missing required fields inside groups, using dotted notation like `result[0].url`. Go silently encodes the partial message without any warning.
- **Test**: CLI test `cli@encode_group_missing_req` — stderr mismatch: C++ emits warning, Go is silent.
- **Root cause**: Go's encode mode likely doesn't recurse into group fields when checking for missing required fields after encoding. The missing-required-field check works for regular message fields (Run tested `encmissreq.Record` with required `name` — both warn) but fails when the required field is inside a group (`repeated group Result` containing `required string url`).
- **C++ protoc**: `warning:  Input message is missing required fields:  result[0].url` on stderr, exit 0.
- **Go protoc-go**: No warning, exit 0. Binary output is identical.
- **Fix hint**: In the encode mode's post-encoding validation, add recursion into group fields when checking for missing required fields. Groups use wire type START_GROUP/END_GROUP but the message structure is the same — required field checking should recurse into groups just like it does for message fields.

### Run 172 — `-oFILE` shorthand for `--descriptor_set_out=FILE` not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `-oFILE` as a shorthand for `--descriptor_set_out=FILE` (documented in C++ protoc `--help` as `-oFILE, --descriptor_set_out=FILE`). Go's `parseArgs()` does not recognize this shorthand. When `-o/dev/null` is used, Go treats it as an unknown flag and reports "Missing value for flag: -o/dev/null", then exits 1. C++ succeeds silently with exit 0.
- **Test**: CLI test `cli@short_o_flag` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` has no handling for `-o` prefix. It handles `-I` (shorthand for `--proto_path`), but not `-o`. The C++ protoc help shows both forms: `-oFILE, --descriptor_set_out=FILE`. Go only supports the long form `--descriptor_set_out=FILE`.
- **C++ protoc**: Accepts `-o/dev/null`, writes descriptor set output, no stderr, exit 0.
- **Go protoc-go**: Rejects `-o/dev/null` with `Missing value for flag: -o/dev/null` on stderr, exit 1.
- **Fix hint**: In `parseArgs()`, add handling for `-o` prefix similar to how `-I` is handled: `if strings.HasPrefix(arg, "-o") { cfg.descriptorSetOut = arg[2:]; continue }`. This would extract the filename from immediately after `-o` (no space separator).

### Run 173 — Decode mode fails on nested groups (group inside a group) (VICTORY)
- **Bug**: Go's `--decode` mode fails to parse messages containing nested groups (a group field inside another group field). C++ protoc decodes them correctly. The Go decoder produces "Failed to parse input." and exits 1 when encountering a group-typed field within a group.
- **Test**: Decode test `decode@nested_group` — `testdata/495_decode_nested_group` with hex `0a0568656c6c6f130a04746573741b082a1c14` (message with `name="hello"`, `Inner { label="test" Deep { value=42 } }`).
- **Root cause**: Go's decode mode likely doesn't properly register or resolve group field descriptors for sub-groups nested inside an outer group. Single-level groups work fine (tested in `462_decode_group_merge`), but adding a second level of group nesting triggers the failure. The wire format uses START_GROUP/END_GROUP tags at both levels, but Go's decoder apparently can't handle the inner group's tags.
- **C++ protoc**: Decodes correctly: `name: "hello"\nInner {\n  label: "test"\n  Deep {\n    value: 42\n  }\n}` exit 0.
- **Go protoc-go**: `Failed to parse input.` exit 1.
- **Fix hint**: In the decode mode's group handling, ensure that when a group field is encountered inside another group, the inner group's field descriptor is properly resolved from the parent group's message type (not the top-level message type). The inner group's message type needs to be looked up in the enclosing group's descriptor, not the root message.

### Run 174 — Encode mode missing-required-field order differs from C++ (VICTORY)
- **Bug**: Go's encode mode lists missing required fields in depth-first order, while C++ protoc lists them in field-number order within each message level (breadth-first). When encoding a message with nested required fields at different depths, the warning message differs.
- **Test**: CLI test `cli@encode_req_order` — `testdata/496_encode_req_order` with input `mid { deep { } }`. Message `Outer` has `optional Mid mid = 1`, `Mid` has `optional Deep deep = 1; required int32 x = 2;`, `Deep` has `required string name = 1;`.
- **Root cause**: Go's `collectMissingRequired` (or equivalent) recurses depth-first into sub-messages before checking sibling required fields. So it finds `mid.deep.name` (deep nesting) before `mid.x` (same level). C++ collects missing fields in field-number order within each message, producing `mid.x, mid.deep.name` (field 2 before field 1's sub-field).
- **C++ protoc**: `warning:  Input message is missing required fields:  mid.x, mid.deep.name` (field-number order within each level).
- **Go protoc-go**: `warning:  Input message is missing required fields:  mid.deep.name, mid.x` (depth-first order).
- **Fix hint**: Change the missing-required-field collection to process all fields in a message before recursing into sub-messages. Or collect all missing fields and sort them by depth/field-number to match C++ order.

### Run 175 — --descriptor_set_in with colon-separated multiple files not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--descriptor_set_in=FILE1:FILE2` (colon-delimited on Unix) to load multiple pre-compiled descriptor set files. Go's `os.ReadFile(cfg.descriptorSetIn)` treats the entire `FILE1:FILE2` string as a single filename, causing a "no such file or directory" error.
- **Test**: CLI test `cli@descriptor_set_in_multi` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails). Test data: `testdata/497_descriptor_set_in_multi/`.
- **Root cause**: `cli.go:486` does `os.ReadFile(cfg.descriptorSetIn)` which passes the whole colon-separated string as a single file path. C++ protoc's `CommandLineInterface::Run()` splits the `--descriptor_set_in` value on the OS path separator (`:` on Unix, `;` on Windows) and reads each file individually, merging the FileDescriptorSets.
- **C++ protoc**: Splits `dep1.pb:dep2.pb` into two files, reads both, merges descriptors, succeeds. Exit 0.
- **Go protoc-go**: Tries to open `dep1.pb:dep2.pb` as a single file. Fails with "no such file or directory". Exit 1.
- **Fix hint**: In the `if cfg.descriptorSetIn != ""` block at cli.go:485, split `cfg.descriptorSetIn` on `:` (or `filepath.ListSeparator` for portability), then iterate each file path, read it, unmarshal as FileDescriptorSet, and merge all descriptors into the `parsed` map. Something like: `for _, path := range strings.Split(cfg.descriptorSetIn, string(os.PathListSeparator)) { data, err := os.ReadFile(path); ... }`
- **Also affects**: Any use case with multiple pre-compiled descriptor sets (common in large build systems like Bazel where dependencies are compiled separately).

### Run 176 — @filename response file syntax not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `@filename` syntax to read arguments from a file (one argument per line). Go's `parseArgs()` has no handling for `@`-prefixed arguments — it treats `@testdata/498_response_file/args.txt` as a literal proto file or unknown argument, resulting in "Missing output directives." and exit 1.
- **Test**: CLI test `cli@response_file` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` iterates `args` but never checks for the `@` prefix. C++ protoc's `CommandLineInterface::Run()` calls `ExpandArgumentFiles(&arguments)` which scans for `@`-prefixed args, reads the referenced file, splits on newlines, and inserts the resulting arguments in place of the `@filename` arg. Go has no equivalent expansion step.
- **C++ protoc**: Reads `testdata/498_response_file/args.txt`, expands to `--descriptor_set_out=/dev/null -Itestdata/01_basic_message testdata/01_basic_message/basic.proto`, succeeds. Exit 0.
- **Go protoc-go**: Treats `@testdata/498_response_file/args.txt` as a literal arg, sees no output directives. Exit 1.
- **Fix hint**: Before `parseArgs()`, add an `expandArgumentFiles` step: iterate args, for any arg starting with `@`, read the file at `arg[1:]`, split contents by newline, filter empty lines, and replace the `@filename` arg with the resulting lines. Each line is one argument. No shell expansion (no quotes, wildcards, etc.). Relative paths are resolved against the working directory (NOT against `--proto_path`).
- **Also affects**: Large projects and build systems that use response files to avoid command-line length limits. Bazel, CMake, and other tools commonly use `@filename` syntax.

### Run 177 — Decode mode accepts truncated packed fixed32 data that C++ rejects (VICTORY)
- **Bug**: Go's `--decode` mode silently accepts malformed packed `fixed32` data where the byte length is not a multiple of 4. C++ protoc rejects such data with "Failed to parse input." and exit code 1. Go decodes as many complete fixed32 values as possible and silently drops trailing bytes, printing the result and exiting with code 0.
- **Test**: Decode test `decode@packed_trunc` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: Go's `proto.Unmarshal` / `protowire` library is more lenient with packed repeated fixed-size fields. When it encounters a BytesType entry for a packed repeated fixed32 field with a length that isn't a multiple of 4, it decodes the complete elements and ignores the remainder. C++ protoc's wire format parser strictly validates that packed fixed-size field lengths are exact multiples of the element size.
- **C++ protoc**: Rejects `0a05010203040512026f6b` (field 1 packed fixed32 with 5 bytes) with "Failed to parse input." exit 1.
- **Go protoc-go**: Decodes 1 fixed32 value (67305985) from the first 4 bytes, drops the 5th byte, prints `vals: 67305985\nlabel: "ok"`, exit 0.
- **Fix hint**: After `proto.Unmarshal` succeeds in `runDecode`, manually validate that all packed repeated fixed-size fields have lengths that are multiples of their element size (4 for fixed32/sfixed32/float, 8 for fixed64/sfixed64/double). Or, parse the raw wire data before calling `proto.Unmarshal` and check for this condition. Alternatively, use a stricter unmarshal option if available.
- **Also affects**: Packed `fixed64`, `sfixed32`, `sfixed64`, `float`, `double` fields with non-multiple-of-element-size lengths would also be silently accepted by Go but rejected by C++.

### Run 178 — Space-separated --proto_path flag value not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--proto_path DIR` (space-separated) syntax, but Go's `parseArgs()` only handles `--proto_path=DIR` (equals-sign form). When `--proto_path testdata/01_basic_message` is used with a space, Go treats `--proto_path` as a flag with no value and falls through to the "Missing value for flag" error handler. C++ correctly consumes the next argument as the flag's value.
- **Test**: CLI test `cli@proto_path_space` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` at cli.go:1373 only checks `strings.HasPrefix(arg, "--proto_path=")`. When `arg == "--proto_path"` (no `=`), Go falls through to line 1589-1594 which matches any `-`-prefixed arg and reports "Missing value for flag". The `-I` shorthand at line 1386-1400 already handles space separation (when `path == ""`, it advances to the next arg), but `--proto_path` does not.
- **C++ protoc**: Parses `--proto_path` + `testdata/01_basic_message` as `--proto_path=testdata/01_basic_message`. Succeeds. Exit 0.
- **Go protoc-go**: Sees `--proto_path` alone, falls through to "Missing value for flag: --proto_path". Exit 1.
- **Fix hint**: Add a handler for `arg == "--proto_path"` that consumes `args[i+1]` as the value: `if arg == "--proto_path" { if i+1 < len(args) { i++; /* parse args[i] as proto_path */ } else { return nil, fmt.Errorf("Missing value for flag: %s", arg) }; continue }`. Same pattern as the `--encode`/`--decode` space handlers already added at lines 1487-1509.
- **Also affects**: `--descriptor_set_out`, `--descriptor_set_in`, `--dependency_out`, `--error_format`, `--direct_dependencies`, `--direct_dependencies_violation_msg`, `--plugin` — all flags that take a value and only handle `--flag=VALUE` form, not `--flag VALUE`.

### Run 179 — Space-separated --descriptor_set_out flag not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--descriptor_set_out /dev/null` (space-separated) syntax, but Go's `parseArgs()` only handles `--descriptor_set_out=VALUE` (equals-sign form). When `--descriptor_set_out /dev/null` is used with a space, Go treats `--descriptor_set_out` as a flag with no value and reports "Missing value for flag: --descriptor_set_out".
- **Test**: CLI test `cli@dso_space` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` at cli.go:1431 only checks `strings.HasPrefix(arg, "--descriptor_set_out=")`. When `arg == "--descriptor_set_out"` (no `=`), Go falls through to the "Missing value for flag" error handler. Same class of bug as Run 178 (`--proto_path` space), but affecting a different flag.
- **C++ protoc**: Parses `--descriptor_set_out` + `/dev/null` as `--descriptor_set_out=/dev/null`. Succeeds. Exit 0.
- **Go protoc-go**: Sees `--descriptor_set_out` alone, falls through to "Missing value for flag: --descriptor_set_out". Exit 1.
- **Fix hint**: Add a handler for `arg == "--descriptor_set_out"` that consumes `args[i+1]` as the value, similar to the `--encode`/`--decode` space handlers. Same pattern needed for `--error_format`, `--descriptor_set_in`, `--dependency_out`, `--direct_dependencies`, `--direct_dependencies_violation_msg`, `--plugin`.
- **Also affects**: All value-taking long flags that only handle `--flag=VALUE` form.

### Run 180 — Space-separated --descriptor_set_in flag not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--descriptor_set_in FILE` (space-separated) syntax, but Go's `parseArgs()` only handles `--descriptor_set_in=FILE` (equals-sign form). When `--descriptor_set_in testdata/474_descriptor_set_in/dep.pb` is used with a space, Go treats `--descriptor_set_in` as a flag with no value and reports "Missing value for flag: --descriptor_set_in".
- **Test**: CLI test `cli@dsi_space` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` at cli.go:1496 only checks `strings.HasPrefix(arg, "--descriptor_set_in=")`. When `arg == "--descriptor_set_in"` (no `=`), Go falls through to the "Missing value for flag" error handler. Same class of bug as Runs 178-179 (`--proto_path` and `--descriptor_set_out` space), but affecting a different flag.
- **C++ protoc**: Parses `--descriptor_set_in` + `dep.pb` as `--descriptor_set_in=dep.pb`. Succeeds. Exit 0.
- **Go protoc-go**: Sees `--descriptor_set_in` alone, falls through to "Missing value for flag: --descriptor_set_in". Exit 1.
- **Fix hint**: Add a handler for `arg == "--descriptor_set_in"` that consumes `args[i+1]` as the value, similar to the `--encode`/`--decode` space handlers already added.
- **Also affects**: All value-taking long flags that only handle `--flag=VALUE` form: `--error_format`, `--dependency_out`, `--direct_dependencies`, `--direct_dependencies_violation_msg`, `--plugin`.

### Run 181 — Duplicate --descriptor_set_out flag accepted by Go but rejected by C++ (VICTORY)
- **Bug**: C++ protoc rejects duplicate `--descriptor_set_out` flags with the error `--descriptor_set_out may only be passed once.` and exits 1. Go's `parseArgs()` silently overwrites the previous value and continues, exiting 0. This is a validation gap — C++ validates that single-value flags are not specified more than once, Go does not.
- **Test**: CLI test `cli@dup_dso` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` at line 1431 handles `--descriptor_set_out=` by simply assigning `cfg.descriptorSetOut = ...`. If the flag appears twice, the second value overwrites the first without any error. C++ protoc's `CommandLineInterface::InterpretArgument()` checks if `descriptor_set_out_name_` is already set and emits an error if the flag is specified again.
- **C++ protoc**: `--descriptor_set_out may only be passed once.` on stderr, exit 1.
- **Go protoc-go**: Silently accepts both, uses the last value, exit 0.
- **Fix hint**: Before assigning `cfg.descriptorSetOut`, check if it's already non-empty. If so, emit an error: `return nil, fmt.Errorf("--descriptor_set_out may only be passed once.")`. Same validation likely needed for other single-value flags: `--descriptor_set_in`, `--dependency_out`, `--encode`, `--decode`, `--error_format`, etc.
- **Also affects**: Potentially all single-value flags that should only be specified once: `--descriptor_set_in`, `--dependency_out`, `--encode`, `--decode`, `--error_format`, `--direct_dependencies`, `--direct_dependencies_violation_msg`.

### Run 182 — Space-separated --dependency_out flag not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--dependency_out /dev/null` (space-separated) syntax, but Go's `parseArgs()` only handles `--dependency_out=VALUE` (equals-sign form). When `--dependency_out /dev/null` is used with a space, Go treats `--dependency_out` as a flag with no value and reports "Missing value for flag: --dependency_out".
- **Test**: CLI test `cli@dependency_out_space` — stderr mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` at cli.go:1557 only checks `strings.HasPrefix(arg, "--dependency_out=")`. When `arg == "--dependency_out"` (no `=`), Go falls through to the "Missing value for flag" error handler. Same class of bug as Runs 178-180, but for `--dependency_out`.
- **C++ protoc**: Parses `--dependency_out` + `/dev/null` as `--dependency_out=/dev/null`. Succeeds. Exit 0.
- **Go protoc-go**: Sees `--dependency_out` alone, reports "Missing value for flag: --dependency_out". Exit 1.
- **Fix hint**: Add a handler for `arg == "--dependency_out"` that consumes `args[i+1]` as the value, same pattern as `--descriptor_set_in` space handler at lines 1507-1515.
- **Also affects**: `--error_format`, `--direct_dependencies`, `--direct_dependencies_violation_msg` — all value-taking long flags that only handle `--flag=VALUE` form, not `--flag VALUE`.

### Run 184 — --decode and --encode mutual exclusion not validated by Go (VICTORY)
- **Bug**: Go's `parseArgs()` allows both `--decode=TYPE` and `--encode=TYPE` to be specified simultaneously without error. C++ protoc validates mutual exclusion and rejects with "Only one of --encode and --decode can be specified." (exit 1). Go sets both `cfg.decodeType` and `cfg.encodeType`, then silently runs decode mode (since the `decodeType` check comes before `encodeType` in the dispatch logic), ignoring `--encode` entirely.
- **Test**: CLI test `decode_encode_mutex` — fails (exit code mismatch + stderr mismatch).
- **Root cause**: `parseArgs()` at cli.go:1518-1542 parses `--encode=` and `--decode=` independently, storing values in `cfg.encodeType` and `cfg.decodeType`. No validation checks that at most one of these is set. The dispatch at cli.go:757-763 checks `decodeType != ""` first, so decode mode runs and `--encode` is silently ignored.
- **C++ protoc**: `protoc --decode=basic.Person --encode=basic.Person ...` → stderr: `"Only one of --encode and --decode can be specified."`, exit 1.
- **Go protoc-go**: Same command with empty stdin → no stderr, exit 0 (silently succeeds with empty decode output).
- **Fix hint**: After parsing all args, add a check: `if cfg.decodeType != "" && cfg.encodeType != "" { return error "Only one of --encode and --decode can be specified." }`. Same for `--decode_raw` + `--encode` and `--decode_raw` + `--decode`.

### Run 185 — Space-separated --error_format flag value not supported by Go (VICTORY)
- **Bug**: Go's `parseArgs()` only handles `--error_format=VALUE` form, not `--error_format VALUE` (space-separated). When `--error_format msvs` is passed, Go fails with `Missing value for flag: --error_format` (exit 1) while C++ protoc accepts it fine (exit 0).
- **Test**: CLI test `error_format_space` — fails (exit code mismatch: C++ 0, Go 1; stderr mismatch).
- **Root cause**: `parseArgs()` in cli.go only has a `strings.HasPrefix(arg, "--error_format=")` handler and no `arg == "--error_format"` handler to consume the next arg. Same pattern as other space-separated flag bugs (Runs 178-182).
- **C++ protoc**: `protoc --error_format msvs --descriptor_set_out=/dev/null -I testdata/01_basic_message testdata/01_basic_message/basic.proto` → exit 0, no errors.
- **Go protoc-go**: Same command → stderr: `Missing value for flag: --error_format`, exit 1.
- **Fix hint**: Add a handler for `arg == "--error_format"` that consumes `args[i+1]` as the value, same pattern as other space-separated flag handlers.

### Run 186 — allow_alias = false error message mismatch (VICTORY)
- **Bug**: Go's validation doesn't handle the case where `option allow_alias = false;` is explicitly set on an enum that has aliased values. C++ protoc detects this specific case and reports `"Status" declares 'option allow_alias = false;' which has no effect. Please remove the declaration.` Go ignores the explicit `false` setting and reports the standard duplicate value error instead.
- **Test**: `500_allow_alias_false` — all 10 profiles fail (error mismatch).
- **Root cause**: Go's enum alias validation (in cli.go or descriptor validation) doesn't check whether `allow_alias` is explicitly set to `false`. C++ protoc specifically checks if `allow_alias` is explicitly set to `false` and reports a distinct error telling the user to remove the pointless declaration. Go just treats `allow_alias = false` the same as `allow_alias` not being set at all.
- **C++ protoc**: `test.proto:12:1: "Status" declares 'option allow_alias = false;' which has no effect. Please remove the declaration.`
- **Go protoc-go**: `test.proto:9:13: "aliasf.RUNNING" uses the same enum value as "aliasf.ACTIVE". If this is intended, set 'option allow_alias = true;' to the enum definition. The next available enum value is 2.`
- **Fix hint**: In enum validation, check if `EnumOptions.GetAllowAlias()` is explicitly set (not just false by default). If explicitly set to false AND there are aliased values, emit the C++ error message. The tricky part is distinguishing "explicitly set to false" from "not set at all" — need to check if the option was actually parsed and set, not just check `GetAllowAlias() == false`.

### Run 187 — Duplicate --descriptor_set_in flag accepted by Go but rejected by C++ (VICTORY)
- **Bug**: C++ protoc rejects duplicate `--descriptor_set_in` flags with `--descriptor_set_in may only be passed once. To specify multiple descriptor sets, pass them all as a single parameter separated by ':'.` and exits 1. Go's `parseArgs()` silently overwrites the previous value and continues, exiting 0. Same class of bug as Run 181 (duplicate `--descriptor_set_out`).
- **Test**: CLI test `cli@dup_dsi` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` at line 1529 handles `--descriptor_set_in=` by simply assigning `cfg.descriptorSetIn = ...`. If the flag appears twice, the second value overwrites the first without any error. C++ protoc checks if the value is already set and emits an error.
- **C++ protoc**: `--descriptor_set_in may only be passed once. To specify multiple descriptor sets, pass them all as a single parameter separated by ':'.` on stderr, exit 1.
- **Go protoc-go**: Silently accepts both, uses the last value, exit 0.
- **Fix hint**: Before assigning `cfg.descriptorSetIn`, check if it's already non-empty. If so, emit the same error as C++: `return nil, fmt.Errorf("--descriptor_set_in may only be passed once. To specify multiple descriptor sets, pass them all as a single parameter separated by ':'.")`.
- **Also affects**: Same validation likely missing for `--dependency_out`, `--encode`, `--decode`, `--error_format`, `--direct_dependencies`, `--direct_dependencies_violation_msg`.

### Run 188 — Duplicate --dependency_out flag accepted by Go but rejected by C++ (VICTORY)
- **Bug**: C++ protoc rejects duplicate `--dependency_out` flags with `--dependency_out may only be passed once.` and exits 1. Go's `parseArgs()` silently overwrites the previous value and continues, exiting 0. Same class of bug as Run 181 (duplicate `--descriptor_set_out`) and Run 187 (duplicate `--descriptor_set_in`).
- **Test**: CLI test `cli@dup_depout` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` at line ~1557 handles `--dependency_out=` by simply assigning `cfg.dependencyOut = ...`. If the flag appears twice, the second value overwrites the first without any error. C++ protoc checks if the value is already set and emits an error.
- **C++ protoc**: `--dependency_out may only be passed once.` on stderr, exit 1.
- **Go protoc-go**: Silently accepts both, uses the last value, exit 0.
- **Fix hint**: Before assigning `cfg.dependencyOut`, check if it's already non-empty. If so, emit: `return nil, fmt.Errorf("--dependency_out may only be passed once.")`.
- **Also affects**: Same validation likely missing for `--encode`, `--decode`, `--error_format`, `--direct_dependencies`, `--direct_dependencies_violation_msg`.

### Run 189 — Duplicate --encode flag accepted by Go but rejected by C++ (VICTORY)
- **Bug**: C++ protoc rejects duplicate `--encode` flags with `Only one of --encode and --decode can be specified.` and exits 1. Go's `parseArgs()` silently overwrites the previous value and continues, exiting 0. Same class of bug as Runs 181, 187, 188 (duplicate flag detection).
- **Test**: CLI test `cli@dup_encode` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` handles `--encode=` by simply assigning `cfg.encodeType = ...`. If the flag appears twice, the second value overwrites the first without any error. C++ protoc checks if the mode is already set to encode/decode and emits the mutual exclusion error.
- **C++ protoc**: `Only one of --encode and --decode can be specified.` on stderr, exit 1.
- **Go protoc-go**: Silently accepts both, uses the last value, exit 0.
- **Fix hint**: Before assigning `cfg.encodeType`, check if it's already non-empty. If so, emit: `return nil, fmt.Errorf("Only one of --encode and --decode can be specified.")`.
- **Also affects**: Same validation likely missing for duplicate `--decode`, duplicate `--direct_dependencies`, duplicate `--direct_dependencies_violation_msg`.

### Run 190 — Duplicate --direct_dependencies flag accepted by Go but rejected by C++ (VICTORY)
- **Bug**: C++ protoc rejects duplicate `--direct_dependencies` flags with `--direct_dependencies may only be passed once. To specify multiple direct dependencies, pass them all as a single parameter separated by ':'.` and exits 1. Go's `parseArgs()` silently overwrites the previous value and continues, exiting 0. Same class of bug as Runs 181, 187-189 (duplicate flag detection).
- **Test**: CLI test `cli@dup_direct_deps` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` handles `--direct_dependencies=` by simply assigning `cfg.directDependencies`. If the flag appears twice, the second value overwrites the first without any error. C++ protoc checks if the value is already set and emits an error.
- **C++ protoc**: `--direct_dependencies may only be passed once. To specify multiple direct dependencies, pass them all as a single parameter separated by ':'.` on stderr, exit 1.
- **Go protoc-go**: Silently accepts both, uses the last value, exit 0.
- **Fix hint**: Before assigning `cfg.directDependencies`, check if it's already been set. If so, emit: `return nil, fmt.Errorf("--direct_dependencies may only be passed once. To specify multiple direct dependencies, pass them all as a single parameter separated by ':'.")`.
- **Also affects**: Same validation likely missing for duplicate `--direct_dependencies_violation_msg`, duplicate `--error_format`.

### Run 191 — --print_free_field_numbers not mutually exclusive with --decode/--encode (VICTORY)
- **Bug**: C++ protoc treats `--print_free_field_numbers` as a "mode" mutually exclusive with `--encode` and `--decode`. When combined with `--decode=TYPE`, C++ rejects with `Only one of --encode and --decode can be specified.` and exits 1. Go ignores the mode conflict, executes `--print_free_field_numbers`, and exits 0.
- **Test**: CLI test `cli@pfn_decode_mutex` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: Go's `parseArgs()` sets both `cfg.printFreeFieldNumbers = true` and `cfg.decodeType = "basic.Person"`. The mutual exclusion check in Go only checks `--encode` vs `--decode`, not `--print_free_field_numbers` vs either. C++ treats `--print_free_field_numbers` as equivalent to an encode/decode mode and checks for conflicts.
- **C++ protoc**: `Only one of --encode and --decode can be specified.` on stderr, exit 1.
- **Go protoc-go**: Prints free field numbers and exits 0. `--decode` flag is silently ignored.
- **Fix hint**: Add a mutual exclusion check in `parseArgs()` or `Run()`: if `cfg.printFreeFieldNumbers` is true AND (`cfg.decodeType != ""` OR `cfg.encodeType != ""` OR `cfg.decodeRaw`), emit the error `Only one of --encode and --decode can be specified.`. C++ groups all four modes under the same umbrella.
- **Also affects**: `--print_free_field_numbers` + `--encode` has the same bug (Go runs pfn and ignores encode). `--print_free_field_numbers` + `--decode_raw` has a different behavior (both fail, but with different error messages).

### Run 192 — Trailing comment after closing brace incorrectly attached to entity in SCI (VICTORY)
- **Bug**: Go's source code info attaches trailing comments that appear after a closing `}` brace as `trailing_comments` on the entity that was just closed. C++ protoc does NOT attach these comments as trailing comments — they are detached or become leading comments for the next entity.
- **Test**: `501_brace_trailing_comment` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: The Go parser's comment tracking logic attaches `// after nested enum brace` as a trailing comment to the nested `Status` enum. C++ protoc recognizes that a comment after `}` on the same line is NOT a trailing comment for the entity because `}` ends the entity — the comment belongs to the parent scope or next entity.
- **C++ protoc**: No trailing comment on the nested enum. The `// after nested enum brace` comment is detached.
- **Go protoc-go**: Attaches `trailing_comments: " after nested enum brace\n"` to the nested enum's SCI location.
- **Fix hint**: In the parser's comment tracking, when a `}` closes an entity (message, enum, service, etc.), do not attach any same-line comment as a trailing comment to that entity. The comment should be treated as a leading or detached comment for the next statement in the parent scope.
- **Also affects**: Same bug occurs for top-level enums (`// after color brace`), nested messages, services — any entity whose `}` is followed by a same-line comment. Run 164 found a similar but different comment bug.

### Run 193 — Go does not emit json_name conflict warning for proto2 fields (VICTORY)
- **Bug**: C++ protoc emits a warning when two fields in the same message have conflicting default JSON names (e.g., `my_name` → `myName` vs field literally named `myName`). Go does not detect or emit this warning at all. Both compilers exit 0 and produce identical binary output, but C++ writes the warning to stderr while Go is silent.
- **Test**: CLI test `cli@json_name_conflict` — stderr mismatch (1 test fails). Test data: `testdata/502_json_name_conflict/test.proto`.
- **Root cause**: Go's descriptor validation does not check for conflicting auto-generated JSON names across fields in the same message. C++ protoc's `DescriptorPool::CrossLinkMessage` validates JSON name uniqueness and emits `warning: The default JSON name of field "X" ("Y") conflicts with the default JSON name of field "Z".` when conflicts exist.
- **C++ protoc**: `test.proto:5:18: warning: The default JSON name of field "myName" ("myName") conflicts with the default JSON name of field "my_name".` (stderr, exit 0).
- **Go protoc-go**: No output (exit 0).
- **Fix hint**: After building all field descriptors in a message, compute the default JSON name for each field (snake_case → camelCase) and check for duplicates. Emit a warning (not error) for proto2 files. For proto3, this is already an error (both compilers reject it).
- **Also affects**: Any proto2 message with fields whose auto-generated JSON names collide. Also affects editions files with `features.json_format = ALLOW`.

### Run 194 — Encode mode missing specific error for negative unsigned field value (VICTORY)
- **Bug**: Go's encode mode (`--encode`) does not emit the specific text format parse error when a negative value is provided for a `uint32` field. C++ protoc reports `input:1:8: Expected integer, got: -` before the generic `Failed to parse input.` summary. Go only reports the summary.
- **Test**: CLI test `encode_neg_uint` — uses `testdata/503_encode_neg_uint/test.proto` with input `value: -1 label: "ok"` for `encneguint.Record`.
- **Root cause**: `reformatProtoTextErrors()` at cli.go:9802 only handles two error patterns from Go's `prototext.Unmarshal`: "unknown field" and "missing field separator". When `prototext.Unmarshal` rejects a negative value for an unsigned field, the error message doesn't match either pattern, so no specific error is printed — just the generic "Failed to parse input." summary.
- **C++ protoc**: `input:1:8: Expected integer, got: -\nFailed to parse input.` (stderr, exit 1).
- **Go protoc-go**: `Failed to parse input.` (stderr, exit 1). Missing the specific error line.
- **Fix hint**: Add a new regex pattern in `reformatProtoTextErrors` to match Go's prototext error for invalid negative unsigned values and reformat it to match C++ format: `input:LINE:COL: Expected integer, got: -`.
- **Also affects**: Any encode mode input where a negative literal is used for `uint32`, `uint64`, `fixed32`, or `fixed64` fields.

### Run 195 — Encode mode checkNegUintFields does not recurse into nested messages (VICTORY)
- **Bug**: Go's `checkNegUintFields()` only scans top-level fields of the encoded message for negative unsigned values. When a negative value appears in a nested sub-message field (e.g., `inner { count: -5 }`), Go doesn't produce the detailed `input:1:16: Expected integer, got: -` error. C++ protoc's text format parser detects the issue at any nesting depth.
- **Test**: CLI test `encode_nested_neg_uint` — uses `testdata/504_encode_nested_neg_uint/test.proto` with input `inner { count: -5 } label: "ok"` for `encnestedneguint.Outer`. Stderr mismatch: C++ has two lines, Go has one.
- **Root cause**: `checkNegUintFields()` at cli.go:10088 builds `uintFields` only from `msgDesc.Fields()` at the top level. It scans the text format input linearly for `fieldName: -value` patterns but has no concept of `{` `}` scoping — it never recurses into nested message bodies to check their unsigned fields.
- **C++ protoc**: `input:1:16: Expected integer, got: -\nFailed to parse input.` (stderr, exit 1).
- **Go protoc-go**: `Failed to parse input.` (stderr, exit 1). Missing the specific error line.
- **Fix hint**: Either (1) make `checkNegUintFields` track `{` `}` nesting and maintain a stack of message descriptors to check unsigned fields at each level, or (2) move negative-uint checking into `reformatProtoTextErrors` pattern matching.
- **Also affects**: Any depth of nesting — double-nested, triple-nested, etc. Also affects `uint64`, `fixed32`, `fixed64` fields in nested messages.

### Run 196 — dependency_out fails on /dev/fd/1 when stdout is redirected (VICTORY)
- **Bug**: Go's `writeDependencyOut()` uses `os.Create()` which opens the file with `O_RDWR|O_CREATE|O_TRUNC` flags. On macOS, when stdout is redirected to a file, opening `/dev/fd/1` with `O_RDWR` fails with "Permission denied". C++ protoc uses `fopen("w")` which uses `O_WRONLY`, which works fine.
- **Test**: CLI test `dependency_out_stdout` — uses `--dependency_out=/dev/fd/1 --descriptor_set_out=/dev/null -I testdata/01_basic_message testdata/01_basic_message/basic.proto`. Exit code mismatch: C++ exits 0, Go exits 1 with "Permission denied".
- **Root cause**: `writeDependencyOut()` at cli.go:945 calls `os.Create(depPath)`, which is equivalent to `os.OpenFile(depPath, O_RDWR|O_CREATE|O_TRUNC, 0666)`. When `depPath` is `/dev/fd/1` and stdout is redirected to a file (as the test harness does), macOS refuses the `O_RDWR` open on the fd device node. C++ protoc's `fopen(path, "w")` uses `O_WRONLY` which succeeds.
- **C++ protoc**: Exit 0, writes dependency output to stdout via `/dev/fd/1`.
- **Go protoc-go**: Exit 1, stderr: `/dev/fd/1: Permission denied`.
- **Fix hint**: Use `os.OpenFile(depPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)` instead of `os.Create(depPath)`.
- **Secondary bug**: Even if the open is fixed, the dependency output content differs — Go writes basenames (e.g., `basic.proto`), C++ writes the full command-line path (e.g., `testdata/01_basic_message/basic.proto`). This is in `orderedFiles` passed to `writeDependencyOut`.

### Run 197 — Encode mode deduplicates duplicate map keys, C++ preserves them (VICTORY)
- **Bug**: Go's `proto.Marshal` deduplicates map entries with the same key during serialization (last-wins semantics), while C++ protoc's `Message::SerializeToString()` preserves all entries including duplicates. When encoding text format input with duplicate map keys like `items { key: "a" value: 1 } items { key: "a" value: 2 }`, Go outputs 7 bytes (one entry) while C++ outputs 14 bytes (two entries).
- **Test**: CLI test `encode_map_dup_key` — uses `--encode=encmapdupkey.Record` with input containing duplicate map keys. stdout mismatch (binary output differs).
- **Root cause**: `runEncode()` at cli.go:9676 uses `proto.MarshalOptions{Deterministic: true}` which calls Go's protobuf library `proto.Marshal`. Go's map marshaling iterates the `map[K]V` (which already deduplicated keys during `prototext.Unmarshal`), producing only one entry per unique key. C++ protoc preserves the raw repeated field entries for map fields (map is syntactic sugar for `repeated MapEntry`).
- **C++ protoc**: Exit 0, stdout = 14 bytes (two MapEntry messages for key "a").
- **Go protoc-go**: Exit 0, stdout = 7 bytes (one MapEntry message for key "a", value = 2).
- **Fix hint**: This is fundamental to how Go's protobuf library handles maps vs C++'s representation. To match C++, Go would need to avoid using native Go maps and instead preserve the repeated field semantics. Could potentially use `dynamicpb` to manually construct repeated entries, or patch `prototext.Unmarshal` to preserve duplicates in the underlying repeated field rather than the Go map.

### Run 198 — Trailing comma in aggregate option list accepted by Go but rejected by C++ (VICTORY)
- **Bug**: Go's `consumeAggregate()` accepts trailing commas in list values inside aggregate options (`values: [1, 2, 3,]`), while C++ protoc rejects them with "Expected integer, got: ]". C++ requires each comma to be followed by another value. Go silently accepts the trailing comma and produces a valid descriptor. This causes all profiles to fail because C++ errors out (exit 1) while Go succeeds (exit 0).
- **Test**: `506_trailing_comma_aggregate` — all 10 profiles fail.
- **Root cause**: Go's aggregate option list parsing in `consumeAggregate()` likely loops on commas without checking if the next token is the closing `]`. After consuming a comma, it should check if the next token is `]` and either reject (to match C++) or handle it. Currently, it seems to loop back, see `]`, and exit the loop normally.
- **C++ protoc**: `test.proto:17:20: Error while parsing option value for "opts": Expected integer, got: ]` — rejects trailing comma. Exit 1.
- **Go protoc-go**: Accepts trailing comma, produces valid descriptor. Exit 0.
- **Fix hint**: In `consumeAggregate()` list parsing, after consuming a `,`, check if the next token is `]`. If so, either (1) emit an error "Expected integer, got: ]" to match C++ behavior, or (2) break out of the loop. Option 1 matches C++ exactly.

### Run 199 — Bare `-I` flag (no value, last arg) produces wrong error message (VICTORY)
- **Bug**: When `-I` is the last argument with no value, C++ protoc detects the missing flag value and reports "Missing value for flag: -I" with exit 1. Go's protoc-go silently adds an empty string to `protoPaths` and then reports "Missing input file." with exit 1. The error message is wrong — it should report the missing flag value, not the missing input file.
- **Test**: CLI test `bare_i_flag` — stderr mismatch. 1 test fails.
- **Root cause**: Go's `-I` flag parsing at cli.go:1418-1423 does `path := arg[2:]` which yields `""`, then checks `if path == "" && i+1 < len(args)` to consume the next arg as the value. If `-I` is the last arg, `i+1 >= len(args)` so it falls through with `path = ""` — silently accepting an empty proto_path. C++ protoc specifically detects this case and reports the missing value.
- **C++ protoc**: `Missing value for flag: -I` (exit 1).
- **Go protoc-go**: `Missing input file.` (exit 1).
- **Fix hint**: After the `if path == "" && i+1 < len(args)` block, add: `if path == "" { return cfg, fmt.Errorf("Missing value for flag: -I") }`.

### Run 200 — Space-separated --direct_dependencies flag not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--direct_dependencies basic.proto` (space-separated) syntax, but Go's `parseArgs()` only handles `--direct_dependencies=VALUE` (equals-sign form). When `--direct_dependencies basic.proto` is used with a space, Go treats `--direct_dependencies` as a flag with no value and reports "Missing value for flag: --direct_dependencies" with exit 1. C++ correctly consumes the next argument as the flag's value.
- **Test**: CLI test `cli@direct_deps_space` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` only checks `strings.HasPrefix(arg, "--direct_dependencies=")`. When `arg == "--direct_dependencies"` (no `=`), Go falls through to the "Missing value for flag" error handler. Same class of bug as Runs 178-185 (space-separated flags for `--proto_path`, `--descriptor_set_out`, `--descriptor_set_in`, `--dependency_out`, `--error_format`), but affecting `--direct_dependencies`.
- **C++ protoc**: Parses `--direct_dependencies` + `basic.proto` as `--direct_dependencies=basic.proto`. Succeeds. Exit 0.
- **Go protoc-go**: Sees `--direct_dependencies` alone, reports "Missing value for flag: --direct_dependencies". Exit 1.
- **Fix hint**: Add a handler for `arg == "--direct_dependencies"` that consumes `args[i+1]` as the value, same pattern as other space-separated flag handlers.
- **Also affects**: `--direct_dependencies_violation_msg` and `--plugin` likely have the same space-separated flag bug.

### Run 201 — Go accepts built-in options on extension ranges that C++ rejects (VICTORY)
- **Bug**: Go's parser accepts built-in options like `deprecated` and `packed` on extension range declarations (`extensions 100 to 200 [deprecated = true]`), but C++ protoc correctly rejects them because `ExtensionRangeOptions` does not have these fields. Go treats built-in options generically across all option types without validating that the option belongs to the correct options message type.
- **Test**: `507_extrange_builtin_option` — all 10 profiles fail (C++ errors with exit 1, Go succeeds with exit 0).
- **Root cause**: Go's parser has special handling for built-in options (`deprecated`, `packed`, `json_name`, etc.) that accepts them on any declaration type. It doesn't validate that the built-in option is actually a field of the appropriate options message (`FieldOptions`, `MessageOptions`, `EnumOptions`, `ExtensionRangeOptions`, etc.). For extension ranges, the only valid built-in options would be those that exist in `ExtensionRangeOptions` — which does NOT include `deprecated`, `packed`, or other field-level options.
- **C++ protoc**: `test.proto:11:26: Option "deprecated" unknown. Ensure that your proto definition file imports the proto which defines the option (i.e. via import option after edition 2024).`
- **Go protoc-go**: Accepts silently, produces a valid descriptor with exit 0.
- **Fix hint**: In the parser's extension range option handling, validate that the option name is a valid field of `ExtensionRangeOptions`. Built-in options like `deprecated`, `packed`, `json_name`, `jstype`, `ctype`, `lazy`, `unverified_lazy`, `weak` are `FieldOptions` fields and should be rejected on extension ranges. Alternatively, check against the known set of `ExtensionRangeOptions` fields (which are primarily `uninterpreted_option`, `declaration`, `features`, and `verification`).
- **Also affects**: Other built-in options that are field-specific (like `packed`, `json_name`, `ctype`, `jstype`, `lazy`, etc.) are likely also incorrectly accepted on extension ranges, enum values, services, methods, and other declaration types where they don't belong.

### Run 202 — Space-separated --plugin flag not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--plugin protoc-gen-dump=path/to/plugin` (space-separated) syntax, but Go's `parseArgs()` only handles `--plugin=VALUE` (equals-sign form). When `--plugin value` is used with a space, Go treats `--plugin` as a flag with no value and reports "Missing value for flag: --plugin" with exit 1. C++ correctly consumes the next argument as the flag's value.
- **Test**: CLI test `cli@plugin_space` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go` only checks `strings.HasPrefix(arg, "--plugin=")`. When `arg == "--plugin"` (no `=`), Go falls through to the "Missing value for flag" error handler. Same class of bug as Runs 178-185, 200 (space-separated flags for `--proto_path`, `--descriptor_set_out`, `--descriptor_set_in`, `--dependency_out`, `--error_format`, `--direct_dependencies`), but affecting `--plugin`.
- **C++ protoc**: Parses `--plugin` + `protoc-gen-dump=path` as `--plugin=protoc-gen-dump=path`. Succeeds. Exit 0.
- **Go protoc-go**: Sees `--plugin` alone, reports "Missing value for flag: --plugin". Exit 1.
- **Fix hint**: Add a handler for `arg == "--plugin"` that consumes `args[i+1]` as the value, same pattern as other space-separated flag handlers.

### Run 203 — Encode mode missing detailed parse error for numeric field name (VICTORY)
- **Bug**: Go's encode mode (text format parser) omits the detailed parse error message when stdin contains an integer where a field name is expected. C++ protoc outputs `input:1:1: Expected identifier, got: 1` followed by `Failed to parse input.`, while Go only outputs `Failed to parse input.` without the location-specific error.
- **Test**: CLI test `cli@encode_field_num` — stderr mismatch (1 test fails).
- **Root cause**: Go's encode mode uses the `prototext` library from `google.golang.org/protobuf` for text format parsing. When parsing fails, the library returns an error, but the Go CLI only prints `Failed to parse input.` without surfacing the detailed error from the text format parser. C++ protoc's text format parser calls `AddError` with location info which gets printed before the generic failure message.
- **C++ protoc**: `input:1:1: Expected identifier, got: 1\nFailed to parse input.`
- **Go protoc-go**: `Failed to parse input.`
- **Fix hint**: In the encode mode handler, after `prototext.Unmarshal` fails, print the error returned by the library (which should contain location and detail info like `Expected identifier, got: 1`) before printing `Failed to parse input.`. Format it as `input:LINE:COL: MESSAGE` to match C++ output.

### Run 204 — Space-separated --option_dependencies flag not supported by Go (VICTORY)
- **Bug**: C++ protoc supports `--option_dependencies basic.proto` (space-separated) syntax, but Go's `parseArgs()` only handles `--option_dependencies=VALUE` (equals-sign form). When `--option_dependencies value` is used with a space, Go treats `--option_dependencies` as a flag with no value and reports "Missing value for flag: --option_dependencies" with exit 1. C++ correctly consumes the next argument as the flag's value.
- **Test**: CLI test `cli@option_deps_space` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: `parseArgs()` in `cli.go:1683` only checks `strings.HasPrefix(arg, "--option_dependencies=")`. When `arg == "--option_dependencies"` (no `=`), Go falls through to the "Missing value for flag" error handler. Same class of bug as Runs 178-185, 200, 202 (space-separated flags), but affecting `--option_dependencies`.
- **C++ protoc**: Parses `--option_dependencies` + `basic.proto` as `--option_dependencies=basic.proto`. Succeeds. Exit 0.
- **Go protoc-go**: Sees `--option_dependencies` alone, reports "Missing value for flag: --option_dependencies". Exit 1.
- **Fix hint**: Add a handler for `arg == "--option_dependencies"` that consumes `args[i+1]` as the value, same pattern as other space-separated flag handlers.

### Run 205 — Missing warning for --include_imports without --descriptor_set_out (VICTORY)
- **Bug**: C++ protoc outputs a warning to stderr when `--include_imports` is used without `--descriptor_set_out`: `--include_imports only makes sense when combined with --descriptor_set_out.` Go silently ignores the flag with no warning. Both exit 0 (success), but stderr differs.
- **Test**: CLI test `cli@include_imports_warn` — stderr mismatch (1 test fails).
- **Root cause**: Go's `parseArgs()` in `cli.go` parses `--include_imports` and sets the flag, but nowhere does it check whether `descriptorSetOut` is set and emit the corresponding warning. C++ protoc checks this condition after argument parsing and emits the warning to stderr.
- **C++ protoc**: Outputs `--include_imports only makes sense when combined with --descriptor_set_out.` to stderr. Exit 0.
- **Go protoc-go**: No stderr output. Exit 0.
- **Fix hint**: After argument parsing, check if `includeImports` is true but `descriptorSetOut` is empty, and if so, output the warning to stderr. Same issue affects `--include_source_info` and `--retain_options` flags.
- **Also affects**: `--include_source_info` (warning: `--include_source_info only makes sense when combined with --descriptor_set_out.`) and `--retain_options` (same pattern).

### Run 206 — Missing warning for --include_source_info without --descriptor_set_out (VICTORY)
- **Bug**: C++ protoc outputs a warning to stderr when `--include_source_info` is used without `--descriptor_set_out`: `--include_source_info only makes sense when combined with --descriptor_set_out.` Go silently ignores the flag with no warning. Both exit 0, but stderr differs.
- **Test**: CLI test `cli@include_source_info_warn` — stderr mismatch (1 test fails).
- **Root cause**: Same pattern as Run 205 (`--include_imports`). Go's `parseArgs()` parses `--include_source_info` and sets the flag, but never checks whether `descriptorSetOut` is set to emit the corresponding warning.
- **C++ protoc**: Outputs `--include_source_info only makes sense when combined with --descriptor_set_out.` to stderr. Exit 0.
- **Go protoc-go**: No stderr output. Exit 0.
- **Fix hint**: After argument parsing, check if `includeSourceInfo` is true but `descriptorSetOut` is empty, and emit the warning. Same pattern as `--include_imports` fix.
- **Also affects**: `--retain_options` flag has the same missing warning bug (still untested).

### Run 207 — Missing warning for --retain_options without --descriptor_set_out (VICTORY)
- **Bug**: C++ protoc outputs a warning to stderr when `--retain_options` is used without `--descriptor_set_out`: `--retain_options only makes sense when combined with --descriptor_set_out.` Go silently ignores the flag with no warning. Both exit 0, but stderr differs.
- **Test**: CLI test `cli@retain_options_warn` — stderr mismatch (1 test fails).
- **Root cause**: Same pattern as Run 205 (`--include_imports`) and Run 206 (`--include_source_info`). Go's `parseArgs()` parses `--retain_options` and sets the flag, but never checks whether `descriptorSetOut` is set to emit the corresponding warning.
- **C++ protoc**: Outputs `--retain_options only makes sense when combined with --descriptor_set_out.` to stderr. Exit 0.
- **Go protoc-go**: No stderr output. Exit 0.
- **Fix hint**: After argument parsing, check if `retainOptions` is true but `descriptorSetOut` is empty, and emit the warning. Same fix pattern as `--include_imports` and `--include_source_info`.

### Run 208 — --deterministic_output without --encode not rejected by Go (VICTORY)
- **Bug**: C++ protoc rejects `--deterministic_output` when it's not combined with `--encode`, printing `Can only use --deterministic_output with --encode.` and exiting with code 1. Go silently accepts `--deterministic_output` in any mode (with `--decode`, `--decode_raw`, `--descriptor_set_out`, `--print_free_field_numbers`) without any error.
- **Test**: CLI test `cli@deterministic_no_encode` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: Go's `parseArgs()` in `cli.go` parses `--deterministic_output` and sets `cfg.deterministicOutput = true`, but never validates that `--encode` is also specified. There is no post-parse check that enforces the `--deterministic_output` + `--encode` requirement.
- **C++ protoc**: `Can only use --deterministic_output with --encode.` Exit 1.
- **Go protoc-go**: No error. Exit 0 (silently succeeds).
- **Fix hint**: After argument parsing, check if `cfg.deterministicOutput` is true but `cfg.encodeType` is empty. If so, print `Can only use --deterministic_output with --encode.` to stderr and exit 1. This check should be placed alongside similar post-parse validation checks.

### Run 209 — Encode mode checkDupFields does not recurse into nested submessages (VICTORY)
- **Bug**: Go's `checkDupFields` in `cli.go:10182` only checks top-level fields for duplicates in `--encode` mode. When a non-repeated field is duplicated inside a nested submessage (e.g., `sub { name: "first" name: "second" }`), Go doesn't detect the duplicate and emits only "Failed to parse input." without the specific error message. C++ protoc correctly detects the nested duplicate and reports the exact field name and location.
- **Test**: CLI test `cli@encode_nested_dup` — stderr mismatch (1 test fails).
- **Root cause**: `checkDupFields()` at cli.go:10182 iterates top-level field names but never recurses into submessage blocks. When it encounters a field name followed by `{` or `<`, it calls `skipTextFormatValue()` which skips the entire block. C++ protoc's `TextFormat::Parser::MergeField` recursively checks each submessage scope for duplicates.
- **C++ protoc**: `input:1:25: Non-repeated field "name" is specified multiple times.` + `Failed to parse input.` Exit 1.
- **Go protoc-go**: `Failed to parse input.` Exit 1 (missing the specific "Non-repeated field" error line).
- **Fix hint**: Add recursion to `checkDupFields`. When a field name is followed by `{` or `<`, look up whether it corresponds to a message-type field and, if so, recurse into the submessage block with a fresh `seenFields` map and the submessage's descriptor. Similar to how `checkNegUintFieldsInner` already recurses into submessages.
- **Also affects**: Deeply nested structures (nested 3+ levels) would also miss duplicate detection.

### Run 210 — Absolute import path (leading `/`) resolved differently by Go (VICTORY)
- **Bug**: Go's importer resolves import paths starting with `/` (e.g., `import "/dep.proto"`) by stripping the leading slash and searching relative to proto_path roots. C++ protoc treats the leading `/` as indicating an absolute filesystem path and fails to find the file since `/dep.proto` doesn't exist at the filesystem root.
- **Test**: `510_absolute_import` — all 10 profiles fail (C++ errors with exit 1, Go succeeds with exit 0).
- **Root cause**: Go's `SourceTree` in `importer/importer.go` likely joins each root with the import path using `filepath.Join(root, importPath)`. When `importPath` is `/dep.proto`, `filepath.Join(".", "/dep.proto")` returns `dep.proto` (Go's `filepath.Join` cleans the path). C++ protoc's `DiskSourceTree` treats absolute paths differently — it doesn't prepend the proto_path root to an absolute import path, so it looks for `/dep.proto` literally on the filesystem.
- **C++ protoc**: `/dep.proto: File not found.` + `test.proto:3:1: Import "/dep.proto" was not found or had errors.` + `test.proto:5:3: "Base" is not defined.` Exit 1.
- **Go protoc-go**: Resolves `/dep.proto` as `dep.proto` relative to proto_path, finds the file, succeeds. Exit 0.
- **Fix hint**: In the importer's file resolution, check if the import path starts with `/`. If so, either (1) treat it as an absolute path (don't prepend proto_path roots), matching C++ behavior, or (2) reject it with an explicit error. The C++ behavior is to search literally for the absolute path, not to strip the leading slash.

### Run 211 — --decode combined with --descriptor_set_out not rejected by Go (VICTORY)
- **Bug**: C++ protoc rejects combining `--decode` (or `--encode`) with `--descriptor_set_out`, printing `Cannot use --encode or --decode and generate descriptors at the same time.` and exiting with code 1. Go silently accepts the combination and exits 0, ignoring the `--descriptor_set_out` flag entirely.
- **Test**: CLI test `cli@decode_with_dso` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: Go's `parseArgs()` in `cli.go` doesn't validate mutual exclusion between encode/decode modes and descriptor set output. After parsing, it processes `--decode` mode first (before `--descriptor_set_out` output), so the descriptor set output is never reached. C++ protoc validates this combination after argument parsing and rejects it upfront.
- **C++ protoc**: `Cannot use --encode or --decode and generate descriptors at the same time.` Exit 1.
- **Go protoc-go**: No error output. Exit 0.
- **Fix hint**: After argument parsing, check if `(cfg.decodeType != "" || cfg.encodeType != "" || cfg.decodeRaw)` AND `cfg.descriptorSetOut != ""`. If both conditions are true, emit: `return nil, fmt.Errorf("Cannot use --encode or --decode and generate descriptors at the same time.")`.
- **Also affects**: `--encode` + `--descriptor_set_out` has the same bug. `--decode_raw` + `--descriptor_set_out` likely too.

### Run 212 — Encode mode checkClosedEnumValues doesn't recurse into nested messages (VICTORY)
- **Bug**: Go's `checkClosedEnumValues()` in `cli.go:10300` only checks top-level fields of the message being encoded. When a nested sub-message contains a proto2 closed enum field with an unknown numeric value, C++ protoc rejects it with `Unknown enumeration value of "99" for field "status"`, but Go silently accepts it and produces binary output.
- **Test**: CLI test `cli@encode_nested_closed_enum` — C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: `checkClosedEnumValues()` iterates `msgDesc.Fields()` to find enum fields, but never recurses into `TYPE_MESSAGE` fields to check their enum fields. When `inner { status: 99 }` is in the text input, Go's check misses the `status` field because it's inside a nested message.
- **C++ protoc**: `input:1:20: Unknown enumeration value of "99" for field "status".` + `Failed to parse input.` Exit 1.
- **Go protoc-go**: No error output. Exit 0. Silently encodes the unknown enum value.
- **Fix hint**: Make `checkClosedEnumValues` recursive — when encountering a `TYPE_MESSAGE` field, look for its `{...}` block in the text input and recursively check the sub-message's fields. Similar to how `checkNegUintFieldsInner` and `checkDupFieldsInner` recurse.
- **Also affects**: Map fields with proto2 enum values (e.g., `map<string, SomeProto2Enum>`) likely have the same issue — the enum check doesn't descend into map entry messages.

### Run 213 — Encode mode checkOneofConflicts doesn't recurse into nested messages (VICTORY)
- **Bug**: Go's `checkOneofConflicts()` in `cli.go:10517` only scans top-level fields of the text format input. When a nested sub-message contains a oneof conflict (e.g., `inner { name: "hello" id: 42 }`), C++ protoc detects it and prints a specific error message, but Go misses it entirely. Go still fails because `prototext.Unmarshal` rejects the conflict, but the specific diagnostic message is lost.
- **Test**: CLI test `cli@encode_nested_oneof` — 1 test fails (stderr mismatch).
- **Root cause**: `checkOneofConflicts()` reads field names at the top level of the text format input and checks against `msgDesc.Fields()`. When it encounters a submessage block `{ ... }`, it calls `skipTextFormatValue` to skip over it instead of recursing into the block to check the nested message's oneofs. Same non-recursion pattern as `checkClosedEnumValues` (Run 212), `checkDupFields` (Run 209), and `checkNegUintFields` (Run 195).
- **C++ protoc**: `input:1:25: Field "id" is specified along with field "name", another member of oneof "choice".` + `Failed to parse input.` Exit 1.
- **Go protoc-go**: `Failed to parse input.` Exit 1. Missing the specific oneof conflict diagnostic.
- **Fix hint**: Make `checkOneofConflicts` recursive. When encountering a submessage field (identified by `{ }` block in text format), look up the field descriptor, get its message type, and recurse into the block to check that message's oneofs. Similar to how `checkClosedEnumValuesInner`, `checkDupFieldsInner`, and `checkNegUintFieldsInner` recurse.

### Run 214 — --print_free_field_numbers not mutually exclusive with --descriptor_set_out (VICTORY)
- **Bug**: C++ protoc treats `--print_free_field_numbers` as an encode/decode-like mode that is mutually exclusive with `--descriptor_set_out`. When combined, C++ rejects with `Cannot use --encode or --decode and generate descriptors at the same time.` and exits 1. Go silently accepts the combination, runs `--print_free_field_numbers` mode, ignores `--descriptor_set_out`, and exits 0.
- **Test**: CLI test `cli@pfn_dso_mutex` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: Go's `parseArgs()` and post-parse validation do not check for the combination of `cfg.printFreeFieldNumbers` and `cfg.descriptorSetOut`. C++ protoc groups `--print_free_field_numbers` under the same "mode" umbrella as `--encode`/`--decode`/`--decode_raw` and validates mutual exclusion with `--descriptor_set_out`. Go only checks `--encode`/`--decode` vs `--descriptor_set_out` (via Run 211's fix), but never includes `--print_free_field_numbers` in that check.
- **C++ protoc**: `Cannot use --encode or --decode and generate descriptors at the same time.` Exit 1.
- **Go protoc-go**: Prints `basic.Person    free: 4-INF` to stdout. Exit 0.
- **Fix hint**: In the post-parse validation, extend the check for `--descriptor_set_out` mutual exclusion to also include `cfg.printFreeFieldNumbers`. Something like: `if cfg.descriptorSetOut != "" && (cfg.decodeType != "" || cfg.encodeType != "" || cfg.decodeRaw || cfg.printFreeFieldNumbers) { return nil, fmt.Errorf("Cannot use --encode or --decode and generate descriptors at the same time.") }`.
- **Also affects**: `--print_free_field_numbers` combined with plugin output flags (`--X_out`) likely has similar missing validation.

### Run 215 — --print_free_field_numbers not mutually exclusive with --X_out plugin flags (VICTORY)
- **Bug**: C++ protoc rejects combining `--print_free_field_numbers` with any `--X_out` plugin flag, printing `Cannot use --encode, --decode or print .proto info and generate code at the same time.` and exiting with code 1. Go silently accepts the combination, runs `--print_free_field_numbers` mode, ignores the plugin output flag, and exits 0.
- **Test**: CLI test `cli@pfn_plugin_mutex` — exit code mismatch: C++ exit 1, Go exit 0 (1 test fails).
- **Root cause**: Go's post-parse validation does not check for mutual exclusion between `cfg.printFreeFieldNumbers` and plugin output flags (`cfg.plugins`). C++ protoc groups `--print_free_field_numbers` under the "print .proto info" mode and validates it is not combined with any code generation output. Go's validation at line 488-493 only checks `len(cfg.plugins) == 0 && cfg.descriptorSetOut == "" && !cfg.printFreeFieldNumbers && ...` to ensure at least ONE output mode is specified, but never checks that ONLY ONE is specified when `printFreeFieldNumbers` is true.
- **C++ protoc**: `Cannot use --encode, --decode or print .proto info and generate code at the same time.` Exit 1.
- **Go protoc-go**: Prints `basic.Person    free: 4-INF` to stdout. Exit 0.
- **Fix hint**: After argument parsing, check if `cfg.printFreeFieldNumbers` is true AND `len(cfg.plugins) > 0`. If both conditions are true, emit: `return nil, fmt.Errorf("Cannot use --encode, --decode or print .proto info and generate code at the same time.")`. Same check should apply to `--encode` and `--decode` with plugin output flags.
- **Also affects**: `--encode` + `--X_out` and `--decode` + `--X_out` likely have the same missing validation bug.

### Run 216 — --decode_raw not mutually exclusive with --X_out plugin flags (VICTORY)
- **Bug**: Go's post-parse validation does not check for mutual exclusion between `--decode_raw` and plugin output flags (`--X_out`). C++ protoc groups `--decode_raw` under the "decode" mode and validates it cannot be combined with any code generation output. Go allows both and happily decodes stdin while ignoring or running the plugin.
- **Test**: CLI test `decode_raw_plugin_mutex` — `--decode_raw --dump_out=/tmp/test -I testdata/01_basic_message testdata/01_basic_message/basic.proto` with stdin `" "`.
- **Root cause**: Go's argument validation does not treat `cfg.decodeRaw` as part of the encode/decode mode that should be mutually exclusive with code generation output. The validation at line 488-493 checks `len(cfg.plugins) == 0 && cfg.descriptorSetOut == "" && !cfg.printFreeFieldNumbers && ...` to ensure at least ONE output mode, but never checks that decode_raw precludes plugin output.
- **C++ protoc**: `Cannot use --encode, --decode or print .proto info and generate code at the same time.` Exit 1.
- **Go protoc-go**: `4: 10` (raw decode output). Exit 0. Silently processes both.
- **Fix hint**: After argument parsing, check if `cfg.decodeRaw` is true AND (`len(cfg.plugins) > 0` OR `cfg.descriptorSetOut != ""`). If so, emit the mutual exclusion error.
- **Also affects**: `--decode_raw` + `--descriptor_set_out` likely has the same missing validation bug.

### Run 217 — Encode mode missing detailed error message for invalid bool value (VICTORY)
- **Bug**: Go's `--encode` mode only outputs `Failed to parse input.` when encountering an invalid boolean value like `TRUE` (all caps). C++ protoc additionally outputs a detailed error with location info: `input:1:12: Invalid value for boolean field "flag". Value: "TRUE".` before the generic failure message.
- **Test**: `cli@encode_bool_TRUE` — CLI test fails (stderr mismatch).
- **Root cause**: Go's text format parser in encode mode does not emit detailed error messages for invalid boolean values. When parsing `TRUE` as a bool field value, Go's parser rejects it but only propagates a generic "Failed to parse input." error to stderr. C++ protoc's text format parser emits a specific error message including the field name, the invalid value, and the line:column location.
- **C++ protoc**: `input:1:12: Invalid value for boolean field "flag". Value: "TRUE".\nFailed to parse input.`
- **Go protoc-go**: `Failed to parse input.`
- **Fix hint**: In the encode mode text format parser, when a bool field receives an unrecognized identifier (not `true`, `false`, `True`, `t`, `f`, `0`, `1`), emit a specific error: `input:LINE:COL: Invalid value for boolean field "FIELD". Value: "VALUE".` before the generic failure. Look at `parseTextFormatField` or similar encode-mode parsing functions in cli.go.
- **Also affects**: Other invalid values for bool fields (like `yes`, `Yes`, `YES`, `no`, `No`, `NO`) likely have the same missing detailed error.

### Run 218 — Encode mode reformatProtoTextErrors uses top-level message type for nested unknown fields (VICTORY)
- **Bug**: Go's `reformatProtoTextErrors()` in `cli.go:10093` always uses the top-level `msgTypeName` parameter when formatting "has no field named" errors. When an unknown field is inside a nested submessage (e.g., `inner { bad_field: "hello" }`), Go reports `Message type "Outer" has no field named "bad_field"` instead of `Message type "Inner"`. C++ protoc correctly identifies the nested message type.
- **Test**: CLI test `cli@encode_nested_unknown` — stderr mismatch (1 test fails).
- **Root cause**: `reformatProtoTextErrors()` receives Go's prototext error which says `unknown field: bad_field` but doesn't include which message type the unknown field was attempted on. The function unconditionally uses the top-level `msgTypeName` parameter, which is always the outer message type. Go's prototext library error format `(line L:C): unknown field: NAME` doesn't specify the message context, so the reformatter can't determine the correct nested message type.
- **C++ protoc**: `input:1:18: Message type "encnestedunknown.Inner" has no field named "bad_field".`
- **Go protoc-go**: `input:1:18: Message type "encnestedunknown.Outer" has no field named "bad_field".`
- **Fix hint**: Instead of always using `msgTypeName`, determine which nested message the field belongs to by walking the text format input. Parse the nesting structure (track `identifier { ... }` blocks) to find which message context the unknown field is in. At the error's line:col position, walk backwards through the text to find the enclosing field name, look up its type in the message descriptor, and use that type name for the error message. Or, parse Go's prototext error more carefully — if newer versions include the message type.

### Run 219 — Import path with ".." not validated by Go importer (VICTORY)
- **Bug**: Go's importer does NOT validate import paths for disallowed components like `..`, `.`, backslashes, or consecutive slashes. When a proto file has `import "../outside.proto"`, C++ protoc rejects the virtual path with a specific validation error: `Backslashes, consecutive slashes, ".", or ".." are not allowed in the virtual path`. Go's importer skips this validation and simply tries to find the file, reporting a generic `File not found.` error when it can't locate it.
- **Test**: `515_dotdot_import` — all 10 profiles fail (error mismatch).
- **Root cause**: C++ protoc's `DiskSourceTree::VirtualFileToDiskFile()` calls `IsValidVirtualPath()` which checks for backslashes, consecutive slashes, `.` components, and `..` components in the import path. Go's `SourceTree` importer lacks this validation step entirely — it goes straight to file resolution via `filepath.Join` which may silently normalize paths (e.g., stripping `..` or `.` components).
- **C++ protoc**: `../outside.proto: Backslashes, consecutive slashes, ".", or ".." are not allowed in the virtual path` + `test.proto:5:1: Import "../outside.proto" was not found or had errors.` Exit 1.
- **Go protoc-go**: `../outside.proto: File not found.` + `test.proto:5:1: Import "../outside.proto" was not found or had errors.` Exit 1.
- **Fix hint**: Add a `ValidateVirtualPath(path string) error` function to the importer that checks: (1) no backslash characters, (2) no consecutive slashes `//`, (3) no `.` path components (split by `/` and check each component), (4) no `..` path components. Call it before attempting file resolution. If validation fails, emit the error: `Backslashes, consecutive slashes, ".", or ".." are not allowed in the virtual path`.
- **Also affects**: Import paths with `.` (e.g., `import "./dep.proto"`), consecutive slashes (e.g., `import "dir//file.proto"`), and backslashes (though backslashes would be rejected by the tokenizer as invalid escape sequences in string literals).

### Run 220 — --decode with --descriptor_set_in requires no proto files, but Go demands them (VICTORY)
- **Bug**: Go's argument validation requires at least one `.proto` input file on the command line even when using `--decode` with `--descriptor_set_in`. C++ protoc correctly allows `--decode` with `--descriptor_set_in` and no proto files — the message type info comes from the descriptor set, not from parsing proto files.
- **Test**: CLI test `cli@decode_dsi_no_proto` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: Go's `parseArgs()` in `cli.go` checks for missing input files and returns `"Missing input file."` error before considering that `--descriptor_set_in` provides all needed type information. C++ protoc's `CommandLineInterface::Run()` skips the input file requirement when `--descriptor_set_in` is specified.
- **C++ protoc**: Loads types from descriptor set, reads stdin, decodes successfully. `15: 10` on stdout. Exit 0.
- **Go protoc-go**: `Missing input file.` on stderr. Exit 1.
- **Fix hint**: In `parseArgs()`, when checking for missing input files, also check if `cfg.descriptorSetIn != ""` AND (`cfg.decodeType != ""` OR `cfg.encodeType != ""`). If a descriptor set is provided for encode/decode mode, input proto files should not be required.
- **Also affects**: `--encode` with `--descriptor_set_in` (no proto files) likely has the same bug — Go would require proto files when C++ doesn't.

### Run 221 — Encode mode reformatProtoTextErrors missing handler for integer overflow (VICTORY)
- **Bug**: Go's `reformatProtoTextErrors` in `--encode` mode doesn't handle "integer out of range" errors from `prototext.Unmarshal`. When encoding a uint32 field with value `4294967296` (exceeds uint32 max), C++ protoc prints `input:1:6: Integer out of range (4294967296)` before `Failed to parse input.`. Go only prints `Failed to parse input.` — the detailed error is silently dropped.
- **Test**: CLI test `cli@encode_int_overflow` — stderr mismatch (1 test fails). Proto in `testdata/516_encode_int_overflow/test.proto`.
- **Root cause**: `reformatProtoTextErrors()` only handles 4 error patterns: unknown field, field by number, missing separator, invalid bool value. It has no handler for integer range errors from Go's prototext library. The prototext error for integer overflow doesn't match any of the existing regex patterns, so the function returns without printing anything specific, and only the generic "Failed to parse input." message appears on stderr.
- **C++ protoc**: stderr: `input:1:6: Integer out of range (4294967296)\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed integer range error.
- **Fix hint**: Add a regex in `reformatProtoTextErrors` to match Go's prototext error for integer overflow (likely something like `(line L:C): invalid value for uint32 type` or `value out of range`) and reformat it to match C++ format: `input:L:C: Integer out of range (VALUE).`
- **Also affects**: int32 overflow, sint32 overflow, sfixed32 overflow — any 32-bit integer field with an out-of-range value in `--encode` mode will have the same missing error message.

### Run 222 — Encode mode reformatProtoTextErrors missing handler for double type mismatch (VICTORY)
- **Bug**: Go's `reformatProtoTextErrors` in `--encode` mode doesn't handle "expected double" errors from `prototext.Unmarshal`. When encoding a double field with a string value (e.g., `dval: "not_a_number"`), C++ protoc prints `input:1:7: Expected double, got: "not_a_number"` before `Failed to parse input.`. Go only prints `Failed to parse input.` — the detailed error is silently dropped.
- **Test**: CLI test `cli@encode_double_type_error` — stderr mismatch (1 test fails). Proto in `testdata/517_encode_double_type_error/test.proto`.
- **Root cause**: `reformatProtoTextErrors()` only handles 5 error patterns: unknown field, field by number, missing separator, invalid bool value, integer overflow. It has no handler for type mismatch errors (like a string provided for a double/float field). The prototext error doesn't match any of the existing regex patterns, so the function returns without printing anything specific, and only the generic "Failed to parse input." message appears on stderr.
- **C++ protoc**: stderr: `input:1:7: Expected double, got: "not_a_number"\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed type mismatch error.
- **Fix hint**: Add a regex in `reformatProtoTextErrors` to match Go's prototext error for type mismatch (likely something like `(line L:C): invalid value for double type: "VALUE"`) and reformat it to match C++ format: `input:L:C: Expected double, got: "VALUE".`
- **Also affects**: float fields with string values, int32 fields with string values, and other type mismatch scenarios in `--encode` mode will have the same missing error message.

### Run 223 — Encode mode reformatProtoTextErrors missing handler for string type mismatch (VICTORY)
- **Bug**: Go's `reformatProtoTextErrors` in `--encode` mode doesn't handle "invalid value for string type" errors from `prototext.Unmarshal`. When encoding a string field with an integer value (e.g., `name: 42`), C++ protoc prints `input:1:7: Expected string, got: 42` before `Failed to parse input.`. Go only prints `Failed to parse input.` — the detailed error is silently dropped.
- **Test**: CLI test `cli@encode_string_type_error` — stderr mismatch (1 test fails). Uses existing `testdata/01_basic_message/basic.proto` with `name: 42` input.
- **Root cause**: `reformatProtoTextErrors()` has no regex to match Go's prototext error for string type mismatch (probably `(line L:C): invalid value for string type: 42`). The double/float handler uses `(?:double|float)` in its regex, which doesn't match `string`. The error falls through all patterns without printing anything, and only the generic "Failed to parse input." appears.
- **C++ protoc**: stderr: `input:1:7: Expected string, got: 42\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed type mismatch error.
- **Fix hint**: Add a regex in `reformatProtoTextErrors` like `reString := regexp.MustCompile(\`\(line (\d+):(\d+)\): invalid value for string type: (.+)\`)` and reformat to `input:L:C: Expected string, got: VALUE`.
- **Also affects**: bytes fields with integer values, and possibly other type mismatch scenarios where a non-string value is provided for a string/bytes field.

### Run 224 — Double overflow in custom option value rejected by Go (VICTORY)
- **Bug**: Go's `encodeCustomOptionValue` rejects `1e309` as a custom double option value because `strconv.ParseFloat("1e309", 64)` returns `err = ErrRange`. C++ protoc accepts it and encodes the value as IEEE 754 positive infinity. The Go code at cli.go:8340-8341 checks `if err != nil` and returns "invalid double value: 1e309" without checking whether the value overflowed to infinity (a valid result).
- **Test**: `518_double_overflow_option` — all 10 profiles fail.
- **Root cause**: `encodeCustomOptionValue()` in cli.go for `TYPE_DOUBLE` calls `strconv.ParseFloat(value, 64)`. When the value overflows double range, Go returns `(+Inf, ErrRange)`. The code checks `if err != nil { return nil, fmt.Errorf("invalid double value: %s", value) }` — it doesn't distinguish overflow (valid: encode as infinity) from truly invalid syntax (invalid).
- **C++ protoc**: Accepts `1e309`, encodes as positive infinity (0x7FF0000000000000), produces valid descriptor.
- **Go protoc-go**: Rejects with `error encoding custom option: invalid double value: 1e309`, exit 1.
- **Fix hint**: After `strconv.ParseFloat`, check `if err != nil && !math.IsInf(v, 0) { return error }`. When `IsInf(v, 0)` is true, the overflow is valid — just encode the infinity bits. Same fix needed for `TYPE_FLOAT` with `3.5e38` (float32 overflow).
- **Also affects**: `TYPE_FLOAT` custom option with `3.5e38` (exceeds FLT_MAX). Same pattern: `ParseFloat(value, 32)` returns `ErrRange`, rejected as "invalid float value". Negative overflow (e.g., `-1e309`) would also be rejected.

### Run 225 — Encode mode reformatProtoTextErrors missing handler for enum type mismatch (VICTORY)
- **Bug**: Go's `reformatProtoTextErrors` in `--encode` mode doesn't handle "invalid value for enum type" errors from `prototext.Unmarshal`. When encoding an enum field with a quoted string value (e.g., `status: "ACTIVE"` instead of `status: ACTIVE`), C++ protoc prints `input:1:9: Expected integer or identifier, got: "ACTIVE"` before `Failed to parse input.`. Go only prints `Failed to parse input.` — the detailed error is silently dropped.
- **Test**: CLI test `cli@encode_enum_type_error` — stderr mismatch (1 test fails). Proto in `testdata/519_encode_enum_type_error/test.proto`.
- **Root cause**: `reformatProtoTextErrors()` handles 6 error patterns (unknown field, field by number, missing separator, int overflow, double/float type, string/bytes type, bool type) but has no handler for enum type mismatch. Go's prototext error for enum type mismatch (probably `(line L:C): invalid value for enum type: "VALUE"`) doesn't match any of the existing regex patterns, so the function returns without printing anything.
- **C++ protoc**: stderr: `input:1:9: Expected integer or identifier, got: "ACTIVE"\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed enum type error.
- **Fix hint**: Add a regex in `reformatProtoTextErrors` like `reEnum := regexp.MustCompile(\`\(line (\d+):(\d+)\): invalid value for enum type: (.+)\`)` and reformat to `input:L:C: Expected integer or identifier, got: VALUE`. Note: C++ says "integer or identifier" for enums, not just "identifier".
- **Also affects**: Any enum field in `--encode` mode where the value is provided as a quoted string instead of a bare identifier will have the same missing error message.

### Run 226 — SCI location ordering for enum value options with mixed custom/standard options (VICTORY)
- **Bug**: When an enum value has both a custom option and a standard option (`[(label) = "default", deprecated = true]`), Go emits the SCI location entries in field-number order (standard option `deprecated` field 1 first, custom option `label` field 50001 second). C++ protoc emits them in source order (custom option first, standard option second).
- **Test**: `520_enum_val_option_sci_order` — 6 profiles fail (descriptor_set_src, descriptor_set_full, plugin, plugin_param, multi_plugin, plugin_descriptor).
- **Root cause**: Go's parser processes standard enum value options (like `deprecated`) before custom options when generating SCI entries, rather than preserving source order. The SCI path `[5,0,2,0,3,50001]` (custom option) should come before `[5,0,2,0,3,1]` (deprecated) because that's the source order, but Go outputs them reversed.
- **C++ protoc**: SCI locations for enum value options are in source order: custom option path first, standard option path second. Produces 624-byte descriptor.
- **Go protoc-go**: SCI locations for enum value options are in field-number order: standard option first, custom option second. Same 624-byte descriptor but binary differs at byte 428.
- **Fix hint**: In the parser's enum value option handling, SCI entries for options within `[...]` should be emitted in the order they appear in source, not grouped by standard-vs-custom.

### Run 227 — --experimental_editions flag not accepted by Go (VICTORY)
- **Bug**: C++ protoc silently accepts `--experimental_editions` as a no-op flag (editions are now stable in v33.4). Go's `parseArgs()` doesn't recognize this flag at all, falling through to the "Missing value for flag" error handler and exiting 1.
- **Test**: CLI test `cli@experimental_editions` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: Go's `parseArgs()` in `cli.go` has handlers for `--experimental_allow_proto3_optional` (line 1582: just `continue`) and other deprecated no-op flags, but no handler for `--experimental_editions`. When Go encounters `--experimental_editions`, it doesn't match any known flag pattern and falls through to the error path.
- **C++ protoc**: `protoc --experimental_editions --descriptor_set_out=/dev/null -I testdata/01_basic_message testdata/01_basic_message/basic.proto` → exit 0, no errors.
- **Go protoc-go**: Same command → stderr: `Missing value for flag: --experimental_editions`, exit 1.
- **Fix hint**: Add `if arg == "--experimental_editions" { continue }` alongside the other no-op flag handlers in `parseArgs()`.

### Run 228 — --descriptor_set_in with overlapping files causes duplicate definition errors in Go (VICTORY)
- **Bug**: When `--descriptor_set_in=file1.pb:file2.pb` is used and both files contain the same FileDescriptorProto (e.g., `file2.pb` was compiled with `--include_imports` and includes `file1.pb`'s contents), C++ protoc silently skips the duplicate, while Go fails with "already defined" errors.
- **Test**: CLI test `cli@dsi_overlap` — exit code mismatch: C++ exit 0, Go exit 1 (1 test fails).
- **Root cause**: In `cli.go:553-568`, when loading `--descriptor_set_in` files, the loop iterates over all files in each descriptor set and does `parsed[fd.GetName()] = fd` + `orderedFiles = append(orderedFiles, fd.GetName())`. If the same `fd.GetName()` appears in multiple descriptor sets (e.g., `base.proto` in both `base.pb` and `main_with_imports.pb`), `parsed` overwrites (fine) but `orderedFiles` gets a duplicate entry. Later validation then processes `base.proto` twice via `orderedFiles`, causing "already defined" errors for all symbols.
- **C++ protoc**: `--descriptor_set_in=base.pb:main_with_imports.pb --decode=main.MainMsg` → exit 0, no errors.
- **Go protoc-go**: Same command → stderr: `base.proto: "value" is already defined in "base.BaseMsg".\nbase.proto: "BaseMsg" is already defined in "base".`, exit 1.
- **Fix hint**: Before adding to `orderedFiles`, check if `fd.GetName()` is already in `parsed`. If so, skip it (don't add to `orderedFiles` again): `if _, exists := parsed[fd.GetName()]; !exists { orderedFiles = append(orderedFiles, fd.GetName()) } parsed[fd.GetName()] = fd`.

### Run 229 — Encode mode reformatProtoTextErrors missing handler for message field type mismatch (VICTORY)
- **Bug**: Go's `reformatProtoTextErrors` in `--encode` mode doesn't handle "expected message opening brace" errors from `prototext.Unmarshal`. When encoding a message field with a scalar value (e.g., `inner: 42` instead of `inner { ... }`), C++ protoc prints `input:1:8: Expected "{", found "42".` before `Failed to parse input.`. Go only prints `Failed to parse input.` — the detailed error is silently dropped.
- **Test**: CLI test `cli@encode_msg_type_error` — stderr mismatch (1 test fails). Proto in `testdata/522_encode_msg_type_error/test.proto`.
- **Root cause**: `reformatProtoTextErrors()` handles 7 error patterns (unknown field, field by number, missing separator, int overflow, double/float type, string/bytes type, enum type, bool type) but has no handler for message type mismatch. Go's prototext error for providing a scalar to a message field doesn't match any existing regex, so the function returns without printing the detailed error.
- **C++ protoc**: stderr: `input:1:8: Expected "{", found "42".\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed message type error.
- **Fix hint**: Add a regex in `reformatProtoTextErrors` to match Go's prototext error for message type mismatch (likely `(line L:C): invalid value for message type: VALUE`) and reformat to match C++ format: `input:L:C: Expected "{", found "VALUE".`
- **Also affects**: Any message/group field in `--encode` mode where a scalar is provided instead of `{ ... }` will have the same missing error message.

### Run 230 — Encode mode reformatProtoTextErrors missing handler for integer field type mismatch (VICTORY)
- **Bug**: Go's `reformatProtoTextErrors` in `--encode` mode doesn't handle errors when an integer field receives a string value. When encoding `id: "hello"` where `id` is `int32`, C++ protoc prints `input:1:5: Expected integer, got: "hello"` before `Failed to parse input.`. Go only prints `Failed to parse input.` — the detailed error is silently dropped.
- **Test**: CLI test `cli@encode_int_type_error` — stderr mismatch (1 test fails). Uses existing `testdata/01_basic_message/basic.proto` (has `int32 id = 2`).
- **Root cause**: `reformatProtoTextErrors()` has a `reIntOverflow` regex that matches `invalid value for ... type: (-?\d+)` — note the capture group `(-?\d+)` only matches numeric values. When Go's prototext reports `invalid value for int32 type: "hello"`, the quoted string `"hello"` does NOT match `\d+`, so this handler is skipped. None of the other handlers match either (`reDouble` requires float/double type, `reString` requires string/bytes type, `reEnum` requires enum type, `reBool` requires bool type). The error falls through all patterns and is silently dropped.
- **C++ protoc**: stderr: `input:1:5: Expected integer, got: "hello"\nFailed to parse input.` (exit 1).
- **Go protoc-go**: stderr: `Failed to parse input.` (exit 1). Missing the detailed type mismatch error.
- **Fix hint**: Change the `reIntOverflow` regex capture group from `(-?\d+)` to something broader like `(.+)` to capture non-numeric values too. Or add a separate handler that catches the "invalid value for int32 type" case with a string value and reformats it as `Expected integer, got: VALUE`.
- **Also affects**: Any integer type field (int32, int64, uint32, uint64, sint32, sint64, fixed32, fixed64, sfixed32, sfixed64) where a string value is provided in `--encode` mode will have the same missing error message.

### Run 231 — MSVS error format not applied to unused import warnings (VICTORY)
- **Bug**: Go's unused import warning output does NOT apply `--error_format=msvs` formatting. When `--error_format=msvs` is specified, C++ protoc formats warnings in MSVS format (`file(line) : warning in column=col: message`) but Go outputs warnings in GCC format (`file:line:col: message`).
- **Test**: CLI test `cli@msvs_unused_import_warn` — stderr mismatch (1 test fails).
- **Root cause**: In `cli.go:786-788`, unused import warnings are printed with `fmt.Fprintln(os.Stderr, mapErrorFilename(w, srcTree))`. This only maps the filename but does NOT check `cfg.errorFormat == "msvs"` to apply MSVS formatting. All error paths (collectErrors, resolveErrors, valErrors, etc.) check `cfg.errorFormat == "msvs"` and call `formatErrorsMSVS()`, but warnings are handled separately and skip this step.
- **C++ protoc**: `testdata/475_unused_import/test.proto(4) : warning in column=1: warning: Import dep.proto is unused.`
- **Go protoc-go**: `testdata/475_unused_import/test.proto:4:1: warning: Import dep.proto is unused.`
- **Fix hint**: (1) Before printing warnings, check `cfg.errorFormat == "msvs"` and format them. (2) Note: C++ uses `warning in column=` (not `error in column=`) for warnings in MSVS mode. The current `formatErrorLineMSVS` always uses `error in column=`. Would need a variant that uses `warning in column=` for warning messages. Could detect if the message contains `warning:` prefix and use `warning in column=` accordingly.
- **Also affects**: Any other warning messages output via the same path (e.g., `--include_imports` / `--include_source_info` / `--retain_options` warnings used with `--print_free_field_numbers`).
