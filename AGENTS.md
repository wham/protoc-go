## Guidelines

- See below for how to run and test.
- Only add code comments for really tricky parts; otherwise keep it clean.
- Don't commit changes to `status.txt` — it's managed by ralph.sh.
- Use past tense in commit messages (e.g., "Fix bug" → "Fixed bug").
- Keep pull-request descriptions minimal — a single sentence, no bullet lists, no markdown headers.
- After a PR is merged, always reset the branch to latest main before starting new work. Never push additional commits to a branch whose PR is already merged — create a fresh branch or reset the existing one.
- When I prompt you to make changes that are radically different from what's documented here, please update this file accordingly.

## What This Is

This is a port of the Protocol Buffers compiler (`protoc`) from C++ to Go. The Go implementation must produce **identical CodeGeneratorRequest payloads** to the C++ protoc when invoking plugins. This means: same FileDescriptorProto structure, same source code info, same error messages.

## Directory Structure

```
/
├── cmd/protoc-go/          # Entry point (mirrors C++ compiler/main.cc)
├── compiler/
│   ├── cli/                # CommandLineInterface (mirrors command_line_interface.cc)
│   ├── parser/             # Parser (mirrors compiler/parser.cc)
│   ├── importer/           # Importer + source tree (mirrors compiler/importer.cc)
│   └── plugin/             # Plugin subprocess (mirrors compiler/subprocess.cc + plugin.cc)
├── io/tokenizer/           # Tokenizer (mirrors io/tokenizer.cc)
├── descriptor/             # DescriptorPool (mirrors descriptor.cc)
├── testdata/               # Test .proto fixtures (numbered)
├── tools/
│   ├── protoc-gen-dump/    # Fake plugin that captures CodeGeneratorRequest
│   └── protoc-bin/         # (reserved) Vendored C++ protoc if needed
├── scripts/
│   ├── test                # Test harness — compares C++ protoc vs Go protoc-go
│   └── find-protoc         # Locates system C++ protoc
├── RALPH.md                # Builder agent prompt (automated loop)
├── NELSON.md               # Adversarial tester prompt (automated loop)
├── ralph.sh                # Loop orchestrator
└── status.txt              # RALPH/NELSON communication
```

## How To Build and Test

```bash
# Build everything and run tests (compares Go protoc-go vs C++ protoc)
scripts/test

# Summary only (no diff output)
scripts/test --summary
```

The test harness:
1. Builds `cmd/protoc-go/` and `tools/protoc-gen-dump/`
2. For each `testdata/*/` directory × 5 profiles, runs both C++ protoc and Go protoc-go
3. Profiles: `plugin`, `plugin_param`, `descriptor_set`, `descriptor_set_src`, `descriptor_set_full`
4. Also runs CLI error tests (no args, missing files, bad flags)
5. Test names: `<case>@<profile>` (e.g., `01_basic_message@plugin`, `cli@no_args`)
6. Reports pass/fail with diffs

## Key Design Decisions

- **Comparison surface**: We compare CodeGeneratorRequest payloads sent to plugins. If both compilers send identical requests, the port is correct.
- **Go package layout mirrors C++**: Each Go package corresponds to a C++ source file/directory in the protobuf repo.
- **Reuse existing proto types**: We use `google.golang.org/protobuf/types/descriptorpb` for FileDescriptorProto etc.
- **No built-in generators**: The Go protoc is plugin-only. No C++/Java/Python code generators.
- **Fake plugin**: `protoc-gen-dump` captures what protoc sends to plugins. It writes JSON + binary + human-readable summary.

## C++ protoc Pipeline (What We're Porting)

```
.proto files → Tokenizer → Parser → Importer → DescriptorPool → CommandLineInterface → Plugin
                (lexer)    (AST)   (imports)   (validate/link)   (orchestrate)        (subprocess)
```

Key C++ files:
- `io/tokenizer.cc` — lexer (~1800 lines)
- `compiler/parser.cc` — parser (~2500 lines)
- `compiler/importer.cc` — import resolution (~500 lines)
- `descriptor.cc` — descriptor pool + validation (~9000 lines)
- `compiler/command_line_interface.cc` — CLI (~3000 lines)
- `compiler/subprocess.cc` — plugin subprocess (~300 lines)
