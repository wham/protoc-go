#!/bin/bash
# Installs the buf CLI that scripts/bench compares against.
#
# Unlike the C++ protoc, buf has no anchor in this repository: protoc-go is a
# port of protoc and declares which protoc release it targets, but nothing here
# targets a buf release. So the version is pinned right here rather than
# resolved at install time — an unpinned buf would let the published week-over-
# week numbers move because a third party shipped, with nothing in the diff to
# say so. scripts/bench records the version it measured in bench.json and
# bench.md, so a bump to this pin is visible in the results.
#
# Bumping it is a normal, expected change; there is no compatibility contract
# to keep, only a record to keep honest.
#
# Everything lands in /tmp — anything written into the workspace would show up
# as an unexpected change in the compliance workflow's commit guard.

set -euo pipefail

BUF_VERSION="${BUF_VERSION:-1.72.0}"

curl -fsSL "https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-Linux-x86_64.tar.gz" -o /tmp/buf.tar.gz
mkdir -p /tmp/buf-install
tar -xzf /tmp/buf.tar.gz -C /tmp/buf-install --strip-components=1
/tmp/buf-install/bin/buf --version
echo "/tmp/buf-install/bin" >> "$GITHUB_PATH"
