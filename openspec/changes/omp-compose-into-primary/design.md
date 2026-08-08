## Context

`internal/render/omp/omp.go`'s `renderAgentFiles` unconditionally writes one
`~/.omp/agent/agents/<name>.md` per non-primary agent targeting `omp`
(`internal/render/omp/omp.go:151-168`). Separately, `Render` writes the
primary agent's prompt verbatim to `~/.omp/agent/APPEND_SYSTEM.md`
(`internal/render/omp/omp.go:82-92`). There is no path today from "a
non-primary agent's prompt" to "content appended to `APPEND_SYSTEM.md`".

`docs/decisions/0001-workflow-as-primary-abstraction.md` (status: Proposed,
not yet accepted) proposes a much larger rewrite — workflows subsume the
whole `agents:` concept, and a workflow step's "discipline" compiles to
omp's compose-into-primary behavior as one possible per-harness shape. That
ADR explicitly calls its own field-level schema (how "discipline" is named,
whether it's a closed enum, how a step binds a harness role) **undecided,
needing its own follow-up design pass before implementation starts**
(ADR-0001, Consequences, last bullet). This change is that narrower,
immediately-actionable slice: add the compose-into-primary mechanism at
today's `agents:` schema level, per issue #13's own proposed shape, without
waiting on or presupposing ADR-0001's acceptance. If ADR-0001 is later
accepted, `compose_into_primary` becomes exactly the per-step compilation
target it already describes for omp — nothing here needs to be thrown away.

## Goals / Non-Goals

**Goals:**
- Let one registry agent entry render differently per harness: standalone
  dispatchable file where that fits (opencode, or omp for
  independently-dispatchable tools), spliced into the primary's own prompt
  where standalone-but-unenforced doesn't (omp, for build/plan-shaped
  discipline).
- Keep the field harness-neutral in the schema even though, today, only
  `omp` implements it — matching the project's existing `targets:`
  precedent (one field, multiple renderers interpret it differently) and
  ADR-0001's stated aversion to harness-specific vocabulary leaking into
  the registry.
- Make the fallback behavior on a non-implementing renderer (opencode)
  exactly "render as if the flag were never set" — zero new behavior to
  reason about there, only a documentation-level gap.

**Non-Goals:**
- Re-litigating ADR-0001's workflow-subsumption model. This change doesn't
  rename `agents:`, doesn't introduce steps/discipline enums, and doesn't
  touch `mode: primary|subagent`.
- Solving #9 (omp has no tool-level enforcement for a standalone agent's
  permissions). Composing into the primary's prompt is a system-prompt-level
  behavioral nudge, not a tool gate — the design explicitly does not claim
  otherwise, matching ADR-0001's own framing of this as "a mitigation, not
  enforcement."
- DAG/branching composition, per-target-harness composition formats beyond
  omp's single Markdown `APPEND_SYSTEM.md` file, or a way to compose into
  something other than the one `mode: primary` agent — the registry allows
  at most one primary today, so "the primary" is unambiguous.

## Decisions

### Field shape: flat `compose_into_primary: bool` on `Agent`, not a nested per-harness block

Alternatives considered:
- **Nested under an `omp:` (or generic per-harness override) block**, e.g.
  `omp: { compose_into_primary: true }`. Rejected: it imports a
  harness-reserved key (`omp`) directly into the schema, the exact leakage
  ADR-0001 flags as the root problem behind #14 and the build/plan-naming
  issue. It would also need a parallel block per future harness that grows
  its own composition model, whereas one flag interpreted differently per
  renderer already has precedent (`targets:`).
- **A closed enum replacing `mode`**, e.g. `mode:
  primary|subagent|composed`. Rejected: `mode` already carries
  `primary`/`subagent` semantics or agentcfg-render orchestration meaning
  (exactly one primary, enforced by validation); overloading it with a
  render-strategy hint conflates two different axes (who owns the
  session vs. how a renderer expresses the agent) and would force
  `mode: composed` + `mode: subagent` to be mutually exclusive when in
  practice compose-into-primary is a per-renderer *treatment* of an
  otherwise perfectly normal subagent (it still renders standalone on
  opencode).
- **Chosen: flat boolean, default false.** Cheapest schema surface,
  reads directly as issue #13's own proposed shape, composes cleanly with
  `targets:` (an agent can be `compose_into_primary: true` and
  `targets: [omp]` if it should ONLY ever exist for omp, or omit `targets`
  to also render standalone on opencode with the same prompt content).

### Validation: hard errors, not warnings, for the two structural contradictions

`compose_into_primary: true` on the primary agent itself, and
`compose_into_primary: true` with zero primary agents in the registry, are
both unambiguous authoring mistakes with no reasonable rendering — not "a
renderer can't fully express this" (that's what `Gap` is for) but "this
registry doesn't make sense on any renderer." Precedent: `validateAgents`
already hard-errors on the structurally analogous "more than one primary
agent" case rather than warning. A renderer-target-exclusion case (an
agent sets `compose_into_primary: true` but `targets:` excludes every
renderer that would honor it, e.g. `targets: [opencode]`) is deliberately
**not** a validation error: `registry.Validate` is renderer-agnostic by
design (it doesn't know which renderer IDs exist, and existing gap
detectors like `detectExternalDirectoryGaps` never cross-check
`targets:`) — that case degrades to "the flag is inert for this agent,"
which is exactly what the `CapComposeIntoPrimary` gap-detection path
already reports.

### Capability + gap semantics: `GapReduction`, not `GapSkip`

`GapSkip` means content is dropped; `GapReduction` means the same content
survives in an alternative form. An agent with `compose_into_primary: true`
rendered by a renderer that doesn't support composition still gets a full
standalone file with its complete prompt — nothing is lost, it's just not
spliced into the primary's own prompt. This matches `detectPrimaryAgentGap`,
which uses `GapReduction` for the analogous "no default-agent key, primary's
prompt appended as a system-prompt file instead" case.

### Splice format: ordered, individually labeled Markdown sections, declaration order

Alternatives considered:
- **Concatenate with no labels.** Rejected: with more than one composed
  agent (issue #13's own worked example composes both a `plan`-shaped and
  a `build`-shaped role into one omp prompt), an unlabeled concatenation
  gives the primary no way to refer back to "the planning discipline"
  vs. "the implementation discipline" in its own reasoning — defeats the
  stated purpose (informing what the primary bakes into a `task` dispatch
  brief).
- **Alphabetical or explicit-priority ordering.** Rejected: adds a second
  field (an explicit order/priority) for a case with no evidence it's
  needed yet — registry declaration order is already deterministic (one
  file may declare `agents:`, per `docs/schema.md`'s merge rules) and lets
  an author control order today, for free, by writing entries in the
  order they want them read.
- **Chosen:** each composed agent's section is `## <name>[: <description>]`
  followed by its prompt body, appended after the primary's own prompt in
  registry declaration order, separated by a blank line. Plain Markdown
  headings need no new vocabulary in `APPEND_SYSTEM.md` (already a plain
  prompt file) and give the primary an addressable anchor per composed
  role.

## Risks / Trade-offs

- [A future third renderer implements composition differently — e.g. XML
  tags instead of Markdown headings] → Not a risk yet: `Capability` is
  renderer-owned, so each renderer decides its own splice syntax
  independently; only the *decision to compose vs. render standalone* is
  schema-level.
- [`compose_into_primary` reads as omp-specific despite being schema-level]
  → Mitigated by `docs/schema.md` documenting it generically (any renderer
  MAY declare `CapComposeIntoPrimary`) and by opencode's explicit
  `GapReduction` fallback proving the fallback path is exercised, not just
  theoretical.
- [This ships ahead of ADR-0001's acceptance and the two later diverge] →
  Low risk: this change touches only `agents:`-level schema exactly as
  ADR-0001 already assumes for its own omp compilation target; if
  ADR-0001 is accepted, the workflow layer compiles down to setting this
  same flag on the derived per-harness agent, not a competing mechanism.

## Migration Plan

Purely additive; no existing registry sets `compose_into_primary` (the
field doesn't exist yet), so every current registry's rendered output is
byte-for-byte unchanged. No migration steps or rollback beyond a normal
revert.
