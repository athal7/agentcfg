## Why

Issue #13: `internal/render/omp/omp.go` always renders every non-primary
registry agent as a standalone `~/.omp/agent/agents/<name>.md` file. That's
the wrong default for an agent whose role is "discipline the primary should
apply when it does the work itself or dispatches via `task`" (a
build/implementer or plan/architect persona) rather than "a specialized,
independently dispatchable tool" (research, browser QA, etc.) — omp has no
tool-level enforcement surface for a standalone agent's permissions (#9), so
a build/plan-shaped standalone omp agent file is either an
always-dispatchable-but-unenforced no-op or actively confusing, while the
same content spliced into the primary's own `APPEND_SYSTEM.md` genuinely
changes what the primary does when it works directly or briefs a `task`
dispatch. This registry has already worked around the gap by hand (chezmoi
`{{ include }}` composing an omp-only prompt variant) — the schema should
express this natively per-agent instead of requiring an out-of-band build
step.

## What Changes

- New per-agent registry field `compose_into_primary: bool` (default
  `false`) on `agents[]`. When true, the omp renderer omits that agent's
  standalone `~/.omp/agent/agents/<name>.md` file and instead appends its
  prompt content as a labeled section onto the primary agent's
  `APPEND_SYSTEM.md` output, after the primary's own prompt body. Multiple
  composed agents are appended in registry declaration order, each under
  its own `## <name>[: <description>]` heading.
- New validation errors in `registry.Validate`:
  - an agent may not set both `mode: primary` and `compose_into_primary:
    true` (an agent cannot compose into itself).
  - `compose_into_primary: true` requires the registry to have exactly one
    `mode: primary` agent (nothing to compose into otherwise).
- New render capability `render.CapComposeIntoPrimary`. `omp` declares it
  and implements the composition. `opencode` does not declare it — an
  agent with `compose_into_primary: true` still renders as a normal
  standalone opencode subagent (opencode's own `permission` block already
  enforces the discipline these agents exist to express), and
  `render.DetectGaps` reports a `GapReduction` (not a `GapSkip`: the
  content isn't lost, just expressed as an independent file instead of a
  composed section) for any renderer that doesn't declare the capability.
- `docs/schema.md`: document the new field, its validation rules, and the
  render-time behavior.
- `docs/capabilities.md`: regenerated (`make docs-capabilities`) to show
  the new capability row and, once `examples/registry/agents.yaml` gains a
  `compose_into_primary` example agent, the resulting opencode
  `GapReduction` entry.
- `examples/registry/agents.yaml`: add a `plan`-shaped subagent with
  `compose_into_primary: true` so the feature is exercised by the same
  registry that drives `docs/capabilities.md` and the example-registry
  tests.

No breaking changes: the field is optional and defaults to `false`,
matching today's always-standalone behavior exactly when omitted.

## Capabilities

### New Capabilities
- `omp-compose-into-primary`: a registry agent can declare
  `compose_into_primary: true` so a harness that supports it (omp) splices
  the agent's prompt into the primary agent's whole-session system prompt
  instead of emitting a standalone dispatchable agent file, while a
  harness that doesn't support it (opencode) falls back to rendering the
  agent as a normal standalone subagent.

### Modified Capabilities
(none — this is a net-new, additive schema field; no existing capability's
requirements change)

## Impact

- `internal/registry/schema.go`: `Agent.ComposeIntoPrimary bool` field.
- `internal/registry/validate.go`: two new validation errors.
- `internal/render/renderer.go`: new `CapComposeIntoPrimary` constant.
- `internal/render/gaps.go`: new `detectComposeIntoPrimaryGaps`, wired into
  `DetectGaps`.
- `internal/render/omp/omp.go`: `renderAgentFiles` excludes
  composed-into-primary agents; `Render` appends their content onto the
  primary's `APPEND_SYSTEM.md` body; `Capabilities()` declares
  `CapComposeIntoPrimary`.
- `internal/cli/doctor.go`: `allCapabilities` gains the new constant so the
  capability matrix stays complete.
- `docs/schema.md`, `docs/capabilities.md`, `examples/registry/agents.yaml`.
- No changes to `internal/render/opencode` — it needs no new code; the gap
  detector alone documents its fallback behavior.
