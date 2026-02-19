# protoc-go

Port of the Protocol Buffers compiler (`protoc`) from C++ to Go.

## Status

Early development. The test harness is in place; the compiler is not yet implemented.

## Build & Test

```bash
# Run the test suite (compares Go protoc-go vs C++ protoc output)
scripts/test

# Summary only
scripts/test --summary
```

Requires: Go 1.21+, C++ `protoc` installed (e.g. `brew install protobuf`).

## How It Works

A fake plugin (`protoc-gen-dump`) captures the `CodeGeneratorRequest` that protoc sends to plugins. The test harness runs both C++ protoc and Go protoc-go on the same `.proto` files, then diffs the captured requests. If they match, the port is correct.

## Automated Development Loop

```bash
# Start the RALPH/NELSON adversarial loop
./ralph.sh
```

- **RALPH** (builder) fixes failing tests one at a time
- **NELSON** (adversarial tester) creates new tests to find bugs
- The loop continues until NELSON can't break it