## 1. Registry schema and validation

- [x] 1.1 Add `ComposeIntoPrimary bool` (`yaml:"compose_into_primary,omitempty"`) to `registry.Agent` in `internal/registry/schema.go`.
- [x] 1.2 In `internal/registry/validate.go`'s `validateAgents`, error when an agent sets both `mode: primary` and `compose_into_primary: true`.
- [x] 1.3 In the same pass, error when any agent sets `compose_into_primary: true` and the registry's primary count is 0.
- [x] 1.4 Add `registry_test` cases: primary+compose_into_primary rejected, compose_into_primary with no primary rejected, compose_into_primary with exactly one primary accepted (happy path, no error).

## 2. Capability and gap detection

- [x] 2.1 Add `CapComposeIntoPrimary` to the `Capability` constants in `internal/render/renderer.go`.
- [x] 2.2 Add `detectComposeIntoPrimaryGaps` to `internal/render/gaps.go` (GapReduction per composing agent when the renderer doesn't declare the capability), wire it into `DetectGaps`.
- [x] 2.3 Add `internal/render/gaps_test.go` cases mirroring the existing primary-agent-gap tests: gap reported when undeclared, suppressed when declared, no gap when no agent composes.
- [x] 2.4 Add `render.CapComposeIntoPrimary` to `internal/cli/doctor.go`'s `allCapabilities`.

## 3. omp renderer

- [x] 3.1 In `internal/render/omp/omp.go`'s `renderAgentFiles`, skip agents with `ComposeIntoPrimary: true` (no standalone file).
- [x] 3.2 In `Render`, after building the primary's `APPEND_SYSTEM.md` body, iterate `reg.Agents` in declaration order and append a labeled section (`## <name>[: <description>]` + prompt body) for every non-primary, omp-targeting agent with `ComposeIntoPrimary: true`.
- [x] 3.3 Add `render.CapComposeIntoPrimary` to `omp`'s declared `Capabilities()`.
- [x] 3.4 Add `internal/render/omp/omp_test.go` cases: composed agent excluded from the rebuilt agent directory; `APPEND_SYSTEM.md` contains the primary body followed by the composed section; multiple composed agents appear in declaration order; an agent with `compose_into_primary: true` but `targets` excluding omp is unaffected (still excluded from both the standalone dir and the compose splice, no gap); `TestCapabilities_OnlyDeclaresWhatIsBuilt` updated for the new capability.

## 4. Docs and example registry

- [x] 4.1 Document `compose_into_primary` in `docs/schema.md`'s `agents:` field table and validation summary.
- [x] 4.2 Add a `plan`-shaped agent with `compose_into_primary: true` to `examples/registry/agents.yaml`, matching issue #13's own worked example.
- [x] 4.3 Regenerate `docs/capabilities.md` via `make docs-capabilities` and confirm the new capability row and the opencode `GapReduction` gap entry for the new example agent appear.

## 5. Verification

- [x] 5.1 `go build ./... && go vet ./... && go test ./... -count=1`.
- [x] 5.2 Confirm `gofmt -l .` is clean.
