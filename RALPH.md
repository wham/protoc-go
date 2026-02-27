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

1. [DONE] Fix `cli@missing_plugin` — match C++ error format for plugin not found
2. [DONE] Fix `cli@output_bad_dir` — match C++ error format for descriptor_set_out write failure
3. [DONE] All 3045/3045 tests pass
4. [DONE] Fix `332_any_expansion_in_option` — handle Any type URL `[type.googleapis.com/...]` syntax in aggregate options. All 3054/3054 tests pass.
5. [DONE] Fix `333_comment_eof_no_newline` — tokenizer's `readLineCommentText()` was always appending `\n` to line comments even when file ends without trailing newline. Now only appends `\n` when the newline is actually present. All 3063/3063 tests pass.
6. [DONE] Fix `334_nan_custom_option` — Go's `math.NaN()` returns `0x7FF8000000000001` but C++ uses canonical NaN `0x7FF8000000000000`. Added NaN detection in double option encoding to use C++ bit pattern. All 3072/3072 tests pass.
7. [DONE] Fix `335_field_subfield_neg_option` — Fixed double-negation bug in SubFieldPath blocks and sub-field option merging. C++ protoc merges sub-field options in `proto_file` (field 15) but keeps them separate in `source_file_descriptors` (field 17). All 3081/3081 tests pass.
8. [DONE] Fix `336_neg_nan_option` — Go's `strconv.ParseFloat` rejects `-nan`. Added special case handling for `nan`/`-nan` in both float and double option encoding, using C++ canonical NaN bit patterns. All 3090/3090 tests pass.
9. [DONE] Fix `337_field_option_string_concat` — parser's `parseFieldOptions()` wasn't concatenating adjacent string literals in custom option values. Added string concatenation loop matching C++ behavior. All 3099/3099 tests pass.
10. [DONE] Fix `338_enum_val_option_string_concat` — enum value option parsing also lacked adjacent string literal concatenation. Added the same pattern. All 3108/3108 tests pass.
11. [DONE] Fix `339_ext_range_option_string_concat` — extension range custom option parsing also lacked adjacent string literal concatenation. Added the same pattern in `parseExtensionRange()`. All 3117/3117 tests pass.
12. [DONE] Fix `340_scalar_subfield_option` — when a sub-field path is used on a scalar (non-message) custom option, emit C++ error: `Option "(name)" is an atomic type, not a message.` with line/column info. Added check in all 9 option encoding blocks. All 3126/3126 tests pass.
13. [DONE] Fix `341_group_option_encoding` — group-type extensions (TYPE_GROUP) must use wire type 3 (StartGroup) and 4 (EndGroup) instead of wire type 2 (length-delimited). Fixed both `encodeAggregateOption` and `encodeAggregateFields`. All 3135/3135 tests pass.
14. [DONE] Fix `342_aggregate_positive_sign` — C++ TextFormat parser rejects `+` prefix on numeric values in aggregate options. Added `Positive` flag to `AggregateField`, handled `+` in `consumeAggregate`/`consumeAggregateAngle`, and added `aggregatePositiveSignError` with type-specific error messages. All 3144/3144 tests pass.
15. [DONE] Fix `343_repeated_with_subfield` — `mergeUnknownExtensions` was merging ALL BytesType entries with the same field number, but repeated scalar fields (e.g., `repeated string tags`) should keep entries separate. Modified `resolveCustomFileOptions` to track which field numbers have sub-field options, and `mergeUnknownExtensions` now only merges those field numbers. All 3153/3153 tests pass.
16. [DONE] Fix `344_field_repeated_merge` — Two issues: (1) For repeated extensions, C++ protoc appends an index to SCI paths (e.g., `[..., 50001, 0]` and `[..., 50001, 1]`). Added per-field repeated index tracking in `resolveCustomFieldOptions`. (2) `mergeFieldOptionsInMessages` was merging ALL field option entries including repeated scalars. Now only merges sub-field option extension numbers. `resolveCustomFieldOptions` returns per-file sub-field tracking. All 3162/3162 tests pass.

17. [DONE] Fix `345_msg_subfield_merge` — Message-level sub-field options (e.g., `option (msg_cfg).name = "hello"; option (msg_cfg).value = 42;`) were not being merged into single extension entries in `proto_file`. Added sub-field tracking to `resolveCustomMessageOptions`, added `mergeMessageOptionsInMessages` function, and updated `cloneWithMergedExtUnknowns` to handle MessageOptions. All 3171/3171 tests pass.

18. [DONE] Fix `348_method_subfield_merge` — Method-level sub-field options (e.g., `option (method_cfg).label = "search"; option (method_cfg).priority = 5;`) were not being merged in `proto_file`. Added sub-field tracking to `resolveCustomMethodOptions`, added `mergeMethodOptions` function that iterates services→methods, updated `cloneWithMergedExtUnknowns` to handle MethodOptions, and added `CustomMethodOptions` to `hasSubFieldCustomOpts`. All 3198/3198 tests pass.

19. [DONE] Fix `347_svc_subfield_merge` — Service-level sub-field options (e.g., `option (svc_cfg).label = "search"; option (svc_cfg).priority = 5;`) were not being merged in `proto_file`. Added sub-field tracking to `resolveCustomServiceOptions`, added `mergeServiceOptions` function, updated `cloneWithMergedExtUnknowns` to handle ServiceOptions, and added `CustomServiceOptions` to `hasSubFieldCustomOpts`. All 3189/3189 tests pass.

20. [DONE] Fix `349_oneof_subfield_merge` — Oneof-level sub-field options (e.g., `option (oneof_cfg).label = "primary"; option (oneof_cfg).priority = 7;`) were not being merged in `proto_file`. Added sub-field tracking to `resolveCustomOneofOptions`, added `mergeOneofOptionsInMessages` function, updated `cloneWithMergedExtUnknowns` to handle OneofOptions, and added `CustomOneofOptions` to `hasSubFieldCustomOpts`. All 3207/3207 tests pass.

21. [DONE] Fix `350_enumval_subfield_merge` — Enum value-level sub-field options (e.g., `LOW = 0 [(val_cfg).label = "low", (val_cfg).weight = 1]`) were not being merged in `proto_file`. Added sub-field tracking to `resolveCustomEnumValueOptions`, added `mergeEnumValueOptions` and `mergeEnumValueOptionsInMessages` functions, updated `cloneWithMergedExtUnknowns` to handle EnumValueOptions, and added `CustomEnumValueOptions` to `hasSubFieldCustomOpts`. All 3216/3216 tests pass.

22. [DONE] Fix `351_extrange_subfield_merge` — Extension range sub-field options (e.g., `extensions 100 to 199 [(range_cfg).label = "primary", (range_cfg).priority = 5]`) had two issues: (1) parser's `parseExtensionRange` was expecting `=` before parsing sub-field path (`.label`), moved sub-field path parsing before `=` expect. (2) Sub-field options were not being merged in `proto_file`. Added sub-field tracking to `resolveCustomExtRangeOptions`, added `mergeExtRangeOptionsInMessages` function, updated `cloneWithMergedExtUnknowns` to handle ExtensionRangeOptions, and added `CustomExtRangeOptions` to `hasSubFieldCustomOpts`. All 3225/3225 tests pass.

23. [DONE] Fix `352_option_order` — C++ protoc emits custom extension options (unknown fields) sorted by field number in `proto_file` and `descriptor_set_out`, but preserves source order in `source_file_descriptors`. Added `sortUnknownFields` to sort raw protobuf unknown fields by field number, and `sortFDOptionsUnknownFields` to sort all Options messages in a FileDescriptorProto. Applied sorting only to `protoFiles` (for proto_file/descriptor_set), cloning when the fd shares a pointer with `parsed` to preserve source order in `source_file_descriptors`. All 3234/3234 tests pass.

24. [DONE] Fix `353_toplevel_ext_field_merge` — Top-level extensions (fd.Extension) with sub-field custom options on their FieldOptions were not being merged in `proto_file`. Added loop over `fdCopy.GetExtension()` in `cloneWithMergedExtUnknowns` to merge FieldOptions unknown fields for top-level extensions. All 3243/3243 tests pass.

25. [DONE] Fix `354_nested_ext_scope` — `findFileOptionExtension` had an overly-broad "bare name match" that would match any extension by field name regardless of scope. Replaced with proper C++ scope walking: tries `currentPkg.name`, then `parentPkg.name`, up to root scope. This correctly rejects `(nested_opt)` when the extension is nested inside a message (must use `(Container.nested_opt)` instead). All 3252/3252 tests pass.

26. [DONE] Fix `355_trailing_empty_stmt` — Top-level empty statement (`;` after `}`) was consumed by `p.tok.Next()` but `p.trackEnd()` was not called on the consumed token. This caused the file-level span (`path=[]`) to end at column 1 instead of column 2 (after the `;`). Added `p.trackEnd(semi)` call. All 3261/3261 tests pass.

27. [DONE] Fix `356_infinity_default` — C++ protoc only accepts `inf` and `nan` as identifier defaults for float/double fields, not `infinity`. Added identifier validation in `parseFieldOptions` that rejects non-inf/nan identifiers with "Expected number." error. Removed dead `infinity`/`-infinity` normalization code. All 3270/3270 tests pass.

28. [DONE] Fix `357_float_overflow_default` — Go's `strconv.FormatFloat` produces `+Inf` for positive infinity, but C++ `SimpleFtoa`/`SimpleDtoa` produce `inf`. Added special-case handling in `simpleFtoa` and `simpleDtoa` for `±Inf` and `NaN` to return C++-compatible strings (`inf`, `-inf`, `nan`). All 3279/3279 tests pass.

## Notes

- `compiler/parser/parser.go`: `consumeAggregate()` and `consumeAggregateAngle()` now handle `/` in extension names inside `[...]` brackets, supporting Any type URL syntax like `[type.googleapis.com/pkg.Msg]`.
- `compiler/cli/cli.go`: `encodeAggregateFields()` detects Any type URL expansion when parent type is `google.protobuf.Any` and field name contains `/`. Encodes `type_url` (field 1) as string, resolves message type from URL, serializes sub-fields into `value` (field 2) as bytes.
- `io/tokenizer/tokenizer.go`: `readLineCommentText()` only appends `\n` when a newline character is actually present in the input. Files ending with a line comment and no trailing newline now produce the correct comment text (without spurious `\n`).
- `compiler/cli/cli.go`: Double NaN encoding uses canonical C++ bit pattern `0x7FF8000000000000` instead of Go's `math.NaN()` (`0x7FF8000000000001`). Float32 NaN uses `0x7FC00000`. Both `nan` and `-nan` string values are handled specially since Go's `strconv.ParseFloat` rejects `-nan`.
- `compiler/cli/cli.go`: Sub-field options (e.g. `[(ext).lo = -40, (ext).hi = 85]`) produce SEPARATE entries in the original FileDescriptorProto (used for `source_file_descriptors`). `cloneWithMergedExtUnknowns` creates a clone with MERGED entries for `proto_file`. The function now handles both FileOptions and FieldOptions (recursively through messages).
- Parser inconsistency: Field/Message/Enum/EnumValue/Service/Method/Oneof option parsers include `-` in `opt.Value` AND set `opt.Negative=true`. The SubFieldPath blocks in the CLI must NOT add another `-` prefix for these option types.
- `compiler/parser/parser.go`: `parseFieldOptions()` now concatenates adjacent string literal tokens in custom option values (e.g., `"hello" " " "world"` → `"hello world"`), matching C++ protoc's behavior.
- `compiler/cli/cli.go`: All 9 option encoding blocks (file, message, field, enum, enum_value, service, method, oneof, extension_range) now check `ext.GetType()` before sub-field resolution. If not `TYPE_MESSAGE`/`TYPE_GROUP`, emit `Option "(name)" is an atomic type, not a message.` with line/column info.
- `compiler/cli/cli.go`: `encodeAggregateOption()` and `encodeAggregateFields()` now check if the field is `TYPE_GROUP` and use `protowire.StartGroupType`/`protowire.EndGroupType` (wire types 3/4) instead of `protowire.BytesType` (wire type 2) for group encoding. This matches C++ protoc's group wire format.
- `compiler/parser/parser.go`: `consumeAggregate()` and `consumeAggregateAngle()` handle `+` prefix on values (similar to `-` handling). `AggregateField` has a `Positive` flag. In `encodeAggregateOption`/`encodeAggregateFields`, `Positive` values are rejected with type-specific C++ error messages (e.g., "Expected integer, got: +" for int types, "Expected double, got: +" for float types).
- `compiler/cli/cli.go`: `mergeUnknownExtensions` now accepts a `mergeableFields map[int32]bool` parameter. When non-nil, only field numbers in the set are merged; others are left as separate entries. This prevents repeated scalar extension fields from being incorrectly concatenated. `resolveCustomFileOptions` returns the per-file set of sub-field option field numbers.
- `compiler/cli/cli.go`: For repeated custom field options, SCI paths now include an index after the extension field number (e.g., `[..., 8, 50001, 0]` and `[..., 8, 50001, 1]`). `resolveCustomFieldOptions` tracks per-field repeated indices and returns per-file sub-field extension numbers. `mergeFieldOptionsInMessages` now accepts and uses `mergeableFields` to only merge sub-field option entries, not repeated scalars.
- `compiler/cli/cli.go`: Message-level sub-field options (e.g., `option (msg_cfg).name = "hello"`) produce separate entries in `source_file_descriptors` but are merged in `proto_file`. `resolveCustomMessageOptions` returns per-file sub-field tracking. `mergeMessageOptionsInMessages` recursively merges MessageOptions unknown fields. `cloneWithMergedExtUnknowns` now handles FileOptions, FieldOptions, MessageOptions, and EnumOptions.
- `compiler/cli/cli.go`: Enum-level sub-field options (e.g., `option (enum_cfg).label = "tracker"`) follow the same pattern. `resolveCustomEnumOptions` returns per-file sub-field tracking. `mergeEnumOptions` handles top-level enums, `mergeEnumOptionsInMessages` handles enums nested in messages. `hasSubFieldCustomOpts` now checks `CustomEnumOptions`.
- `compiler/cli/cli.go`: Service-level sub-field options (e.g., `option (svc_cfg).label = "search"`) follow the same pattern. `resolveCustomServiceOptions` returns per-file sub-field tracking. `mergeServiceOptions` handles services. `hasSubFieldCustomOpts` now checks `CustomServiceOptions`. `cloneWithMergedExtUnknowns` now handles FileOptions, FieldOptions, MessageOptions, EnumOptions, ServiceOptions, and MethodOptions.
- `compiler/cli/cli.go`: Method-level sub-field options (e.g., `option (method_cfg).label = "search"`) follow the same pattern. `resolveCustomMethodOptions` returns per-file sub-field tracking. `mergeMethodOptions` iterates services→methods to merge MethodOptions unknown fields. `hasSubFieldCustomOpts` now checks `CustomMethodOptions`.
- `compiler/cli/cli.go`: Oneof-level sub-field options (e.g., `option (oneof_cfg).label = "primary"`) follow the same pattern. `resolveCustomOneofOptions` returns per-file sub-field tracking. `mergeOneofOptionsInMessages` recursively handles oneofs in messages. `hasSubFieldCustomOpts` now checks `CustomOneofOptions`. `cloneWithMergedExtUnknowns` now handles FileOptions, FieldOptions, MessageOptions, EnumOptions, ServiceOptions, MethodOptions, OneofOptions, and EnumValueOptions.
- `compiler/cli/cli.go`: Enum value-level sub-field options (e.g., `LOW = 0 [(val_cfg).label = "low"]`) follow the same pattern. `resolveCustomEnumValueOptions` returns per-file sub-field tracking. `mergeEnumValueOptions` handles top-level enums, `mergeEnumValueOptionsInMessages` handles enums nested in messages. `hasSubFieldCustomOpts` now checks `CustomEnumValueOptions`.
- `compiler/parser/parser.go`: Extension range custom option sub-field path parsing (e.g., `(range_cfg).label`) must happen BEFORE expecting `=`, not after. The parser's `parseExtensionRange` was consuming `(range_cfg)` via `parseParenthesizedOptionName`, then expecting `=`, but the next token was `.`. Fixed by moving sub-field path loop before `=` expect.
- `compiler/cli/cli.go`: Extension range sub-field options follow the same merge pattern. `resolveCustomExtRangeOptions` returns per-file sub-field tracking. `mergeExtRangeOptionsInMessages` recursively handles extension ranges in messages. `hasSubFieldCustomOpts` now checks `CustomExtRangeOptions`. `cloneWithMergedExtUnknowns` now handles all option types including ExtensionRangeOptions.
- `compiler/cli/cli.go`: C++ protoc sorts unknown fields (custom extension options) by field number in `proto_file` and `descriptor_set_out`, but preserves source order in `source_file_descriptors`. `sortUnknownFields` parses raw protobuf bytes into field entries and stable-sorts by field number. `sortFDOptionsUnknownFields` applies sorting to all Options messages in a FileDescriptorProto. Sorting is applied in the `protoFiles` build loop, cloning from `parsed` when necessary to avoid modifying `source_file_descriptors`.
- `compiler/cli/cli.go`: `cloneWithMergedExtUnknowns` now also merges FieldOptions unknown fields for top-level extensions (`fd.Extension`), not just fields/extensions within messages. Previously only `mergeFieldOptionsInMessages` was called which handled message-level fields and nested extensions, but top-level extensions were missed.
- `compiler/cli/cli.go`: `findFileOptionExtension` now uses proper C++ scope walking instead of a broad "bare name match". For a name like `nested_opt` in package `scope_test`, it tries `scope_test.nested_opt` then `nested_opt` (root). This prevents matching extensions nested inside messages (e.g., `scope_test.Container.nested_opt`) when only the bare field name is used.
- Run tests with `scripts/test` or `scripts/test --summary`.- `compiler/parser/parser.go`: Top-level empty statement (`case ";"`) must call `p.trackEnd()` on the consumed semicolon token so that `p.lastLine`/`p.lastCol` (used for the file-level span `path=[]`) includes the trailing `;`. Without this, files like `};\n` would report the span ending at `}` instead of `;`.
- `compiler/parser/parser.go`: `simpleFtoa` and `simpleDtoa` now handle `±Inf` and `NaN` special values, returning C++-compatible strings (`inf`, `-inf`, `nan`) instead of Go's default formatting (`+Inf`, `-Inf`, `NaN`). This matters when float values overflow `float32` (e.g., `3.5e38` → `inf` not `+Inf`).
