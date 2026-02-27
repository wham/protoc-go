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
- Same case-sensitivity issue may exist in custom option value parsing (e.g., `option (my_opt) = Infinity;` — does Go accept it?)
- `simpleFtoa` edge case: find a specific float32 value where Go's `FormatFloat(float64(v), 'g', 6, 64)` differs from C++'s `snprintf(buf, "%.6g", f)` due to the float64 bit width parameter