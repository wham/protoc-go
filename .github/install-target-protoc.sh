#!/bin/bash
# Installs the C++ protoc release that protoc-go claims to mirror.
#
# The version comes from the binary itself (`protoc-go --version`) so there is
# one source of truth: bumping the target in compiler/cli/cli.go is enough, and
# CI can never end up comparing against a release protoc-go no longer targets.
#
# Everything lands in /tmp — anything written into the workspace would show up
# as an unexpected change in the compliance workflow's commit guard.

set -euo pipefail

go build -o /tmp/protoc-go ./cmd/protoc-go/
version=$(/tmp/protoc-go --version | awk '{print $2}')
if [ -z "$version" ]; then
    echo "could not read target version from protoc-go --version" >&2
    exit 1
fi
echo "protoc-go targets C++ protoc $version"

curl -fsSL "https://github.com/protocolbuffers/protobuf/releases/download/v${version}/protoc-${version}-linux-x86_64.zip" -o /tmp/protoc.zip
unzip -q /tmp/protoc.zip -d /tmp/protoc-install
echo "/tmp/protoc-install/bin" >> "$GITHUB_PATH"
