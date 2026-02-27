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

## Notes

- `compiler/parser/parser.go`: `consumeAggregate()` and `consumeAggregateAngle()` now handle `/` in extension names inside `[...]` brackets, supporting Any type URL syntax like `[type.googleapis.com/pkg.Msg]`.
- `compiler/cli/cli.go`: `encodeAggregateFields()` detects Any type URL expansion when parent type is `google.protobuf.Any` and field name contains `/`. Encodes `type_url` (field 1) as string, resolves message type from URL, serializes sub-fields into `value` (field 2) as bytes.
- `io/tokenizer/tokenizer.go`: `readLineCommentText()` only appends `\n` when a newline character is actually present in the input. Files ending with a line comment and no trailing newline now produce the correct comment text (without spurious `\n`).
- `compiler/cli/cli.go`: Double NaN encoding uses canonical C++ bit pattern `0x7FF8000000000000` instead of Go's `math.NaN()` (`0x7FF8000000000001`). Float32 NaN uses `0x7FC00000`. Both `nan` and `-nan` string values are handled specially since Go's `strconv.ParseFloat` rejects `-nan`.
- `compiler/cli/cli.go`: Sub-field options (e.g. `[(ext).lo = -40, (ext).hi = 85]`) produce SEPARATE entries in the original FileDescriptorProto (used for `source_file_descriptors`). `cloneWithMergedExtUnknowns` creates a clone with MERGED entries for `proto_file`. The function now handles both FileOptions and FieldOptions (recursively through messages).
- Parser inconsistency: Field/Message/Enum/EnumValue/Service/Method/Oneof option parsers include `-` in `opt.Value` AND set `opt.Negative=true`. The SubFieldPath blocks in the CLI must NOT add another `-` prefix for these option types.
- Run tests with `scripts/test` or `scripts/test --summary`.