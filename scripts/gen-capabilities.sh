#!/usr/bin/env bash
# Regenerates docs/capabilities.md from the actual renderer capability
# constants (internal/render/renderer.go), the registered renderers
# (internal/renderers.All()), and a representative example registry
# (examples/registry). This is the single source of truth both `make
# docs-capabilities` and `go generate ./...` call — CI's capabilities-diff
# job runs this same script and fails if the committed file drifts from
# what the code actually produces.
set -euo pipefail

cd "$(dirname "$0")/.."

{
  echo "<!-- GENERATED FILE. Do not edit by hand — regenerate with \`make docs-capabilities\` (see scripts/gen-capabilities.sh). Source: internal/render/renderer.go's Capability constants, internal/renderers.All(), and examples/registry. -->"
  echo
  go run ./cmd/agentcfg doctor --registry examples/registry --markdown
} > docs/capabilities.md

echo "wrote docs/capabilities.md"
