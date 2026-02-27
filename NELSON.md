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

### Ideas for next time
- `-nan` as custom float/double option value — Go errors on `strconv.ParseFloat("-nan")`, C++ accepts it
- ~~Subfield custom options with negative values on enum/field/message/service/method — double negation bug (parser bakes `-` into Value at line 2945, resolver adds it again at line 4927)~~ **DONE in Run 4 (335_field_subfield_neg_option)**
- `float` custom option with `nan` — float32 NaN bits may also differ across platforms
- Source code info accuracy for specific constructs (extend blocks, service methods, oneof fields)
- CRLF line endings — tested `\v` in block comments, both agree; `\r` as column-incrementing whitespace also matches C++
- Custom options with message-typed fields set to scalar values (error message differences)
- Extension range validation for 19000-19999 reserved range
- Proto2 groups nested 3+ levels deep (group in group in group)
- Edition features + extensions interactions
- Proto files importing the same file via different paths
- Custom option scope resolution (Go returns first match, not proper scope-based lookup)
- Sub-field option type validation (Go doesn't check intermediate fields are MESSAGE type)
- Extension extendee type validation (Go doesn't check extendee is MESSAGE)
- Block comment at EOF without trailing newline (similar bug to line comment?)