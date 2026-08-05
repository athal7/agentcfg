.PHONY: build vet fmt test docs-capabilities check

build:
	go build ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

test:
	go test ./... -count=1

# Regenerates docs/capabilities.md from the live capability constants and
# examples/registry. Run this whenever internal/render/renderer.go's
# Capability set or a renderer's Capabilities() changes — CI's
# capabilities-diff job fails if this drifts from what's committed.
docs-capabilities:
	./scripts/gen-capabilities.sh

check: build vet test
	@test -z "$$(gofmt -l .)" || (echo "gofmt needs to be run on:"; gofmt -l .; exit 1)
