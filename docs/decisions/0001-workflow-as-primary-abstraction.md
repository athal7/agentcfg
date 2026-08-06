# ADR-0001: Workflow as the primary registry abstraction, subsuming standalone agents

**Status**: Proposed

## Context

agentcfg's registry currently models `agents` as the primary authoring unit (`examples/registry/agents.yaml`: `name`, `mode: primary|subagent`, `permissions`, `mcp`, `class`, `steps`). Each agent is rendered per-target largely 1:1 — opencode gets a standalone agent file with enforced `permission` blocks; omp gets a standalone `~/.omp/agent/agents/<name>.md`.

Three converging findings this session showed this model doesn't actually achieve harness-neutral behavior, it just *looks* neutral in the schema while leaking one harness's vocabulary in practice:

- **Issue #14**: a custom omp agent literally named `plan` hangs indefinitely — `plan` collides with omp's own reserved `modelRoles` name and interactive plan-mode toggle. The example registry's `build`/`plan` names are opencode's own built-in mode names (`build` = full read/write, `plan` = restricted/read-only), not neutral — they only accidentally work on opencode because opencode defines them too.
- **Issue #9**: there is no mechanism (today) to enforce the same "primary must delegate file writes" contract on omp that opencode gets natively via `permission.edit/write: deny` — `--tools=` doesn't actually gate `write`/`edit`.
- **omp docs research** (`docs/vibe-mode.md`, `docs/magic-keywords.md`, fetched directly from `github.com/can1357/oh-my-pi`): omp's own native delegation primitive, `/vibe` (director + `fast`/`good` worker tiers with real enforced tool-stripping), is an interactive-TUI-only toggle — not renderable, not launch-flag-controlled, explicitly rejects session forking/handoff. It cannot statically close #9's gap. omp's `workflowz` magic keyword, by contrast, *is* renderable (a plain prose word, gated on `eval`+`task` both being active) and maps well onto issue #3 (structured workflows) — already documented via a comment on #3.

The user's stated goal, restated directly: *"I want the user to be able to design an abstract workflow, and that workflow to play out in the different harnesses correctly for both, using their primitives when possible."* Combined with an explicit preference for agentcfg being **opinionated rather than flexible**, this reframes the project: the thing a registry author designs should be a workflow (e.g. plan→implement→review), not a per-harness agent list that merely happens to render consistently.

The user was asked directly whether the existing agent registry should stay as a standalone concept (workflows layered on top, additive) or whether workflows should **subsume** agents entirely (agents become a derived, per-harness rendering detail of a workflow step, not a standalone registry entry). The user's answer, verbatim: **"subsume, and yes plan."**

This ADR documents that decision's reasoning and its concrete consequences, and this ADR is being authored directly (not via the `plan` subagent) because that subagent's model routing in this repo is currently pinned to an overloaded local backend (org on_launch hook issue, tracked separately, not an agentcfg concern).

## Decision

**Workflows become the top-level, primary authoring unit in the registry; the standalone `agents:` concept is removed and replaced by agent *roles* that exist only as an implementation detail of workflow steps.**

A registry author writes a workflow as an ordered sequence of steps — v1 scopes to linear ordering only; DAG-shaped branching (step IDs, inter-step dependencies, cycle validation, branch/join semantics, failure propagation) is out of scope and deferred to a follow-up ADR once a real multi-branch use case exists. Each step declares *what discipline it needs* (e.g. "restricted to reading + planning," "full read/write," "isolated/independently-dispatchable tool call") rather than *which named agent to use*. agentcfg compiles each step, per target harness, to that harness's native primitive for expressing that discipline:

- **opencode**: a step needing "restricted planning" discipline compiles to a standalone subagent with `permission: {edit: deny, write: deny}` (opencode's own built-in `plan`-shaped mode, but agentcfg no longer needs to call it `plan` in the registry — the harness-facing name is a compilation detail); a step needing "full read/write, delegated" compiles to a standalone subagent with full permissions (opencode's `build`-shaped mode).
- **omp**: a step needing "restricted planning/review discipline for the primary" compiles to content composed into the primary's `APPEND_SYSTEM.md` (per #13's already-prototyped `compose_into_primary` direction) rather than a standalone agent file. This is a partial behavioral mitigation for #9, not tool-level enforcement — a system-prompt instruction cannot deny `write`/`edit` at the tool-call layer, and `internal/render/omp/omp.go` still renders non-primary omp agents with those tools live unless their permissions are explicitly denied; closing #9 fully needs a separate tool gate. A step needing "independently dispatchable tool" (research, QA-shaped work) still compiles to a standalone `~/.omp/agent/agents/*.md` file, since that shape genuinely fits omp's `task` tool — the compiler must reject or rename any compiler-generated role name that collides with omp's reserved vocabulary (`plan`, `build`, `default`, `smol`, `slow`, etc.), the same invariant #14 exists to fix, so this step-shape never reintroduces the collision it's meant to route around.

This directly resolves the root cause common to #14 (name collision) and the build/plan-leaks-opencode-vocabulary problem: the registry author never writes a harness-reserved name into the schema at all. It also gives #2 (commands) and #3 (workflows) a single underlying implementation — a command is a one-step workflow.

## Decision matrix

| Criterion | A — Status quo (agents primary, workflow layered on top, additive) | B — Workflow subsumes agents (chosen) | C — Hybrid (workflow optional, agents stay standalone for simple cases) |
|---|:---:|:---:|:---:|
| Matches stated goal (author workflow once, compiles correctly per-harness) | ❌ Workflow would just reference existing agents by name — doesn't fix the name-leak/collision problem | ✅ Directly addresses it — no harness-reserved name ever enters the schema | ⚠️ Only for registries that opt into workflows; simple registries keep the current name-collision exposure |
| Opinionated vs. flexible (user's explicit preference) | ❌ Most flexible, least opinionated | ✅ Most opinionated — agentcfg decides the per-harness shape | ⚠️ Split the difference, weakest signal either way |
| Migration cost for the one existing example registry | ✅ None | ⚠️ `examples/registry/agents.yaml` must be rewritten as a workflow | ✅ None required, but doesn't showcase the new model |
| Schema reversibility | ✅ No change needed | ❌ Hard to reverse once real registries exist (which is why an ADR was requested) | ⚠️ Two concepts to maintain long-term is its own lock-in |
| Solves #9 (omp tool enforcement) | ❌ No | ⚠️ Partially — compose-into-primary is a behavioral mitigation, not a tool-level gate; #9 isn't fully closed until omp gets a separate enforcement surface | ⚠️ Only for workflow-opted-in registries |
| Implementation surface (v1 already shipped) | ✅ Smallest — no schema break | ❌ Largest — registry schema, both renderers, validate, docs, examples, capabilities.md all change | ⚠️ Medium — additive schema, but two code paths to maintain in both renderers |

## Alternatives considered

### A — Status quo, additive workflow layer
Viable as the lowest-risk option: it would let `docs.md`/`agents.yaml` stand untouched and add a new `workflows:` top-level key referencing agent names by string. Ruled out because it doesn't touch the actual root cause — a workflow step referencing an agent named `plan` still collides with omp's reserved vocabulary, and still can't statically enforce "no writes" on omp's primary. It would ship a workflow feature that inherits every existing translation problem instead of fixing any of them.

### C — Hybrid, workflow optional
Viable as a middle ground that avoids a hard migration. Ruled out primarily on maintainability and on the user's explicit "opinionated, not flexible" preference: keeping two parallel first-class concepts (bare agents and workflow-derived agents) means every future renderer feature (permissions, MCP, bash policy) needs to be reasoned about twice, and a registry author has to decide up front which model they're in rather than getting one consistent, opinionated answer from agentcfg.

## Consequences

- Positive: eliminates the entire class of harness-reserved-name collisions (#14 and any not-yet-discovered sibling — the user's dotfiles session flagged `default`/`smol`/`slow` as unverified-but-worth-checking, which becomes moot under this model since no registry-authored name ever reaches omp's agent-file layer for supervised-discipline steps).
- Positive: gives omp a partial answer to #9 — supervised-discipline steps get a behaviorally-mitigated restriction via composition into the primary's own prompt instead of an unenforceable standalone file; this is a mitigation, not tool-level enforcement, until omp gets a separate enforcement surface (tracked as follow-up).
- Positive: unifies #2 (commands) and #3 (workflows) under one schema concept instead of two.
- Positive: matches the user's explicit "opinionated over flexible" direction for the whole project, not just this feature.
- Negative: v1's shipped schema, both renderers, `validate`, `doctor`/`capabilities.md`, `docs/schema.md`, and the one example registry all need a breaking rewrite — this is not additive. Migration path: the loader adds a required top-level schema version; a registry declaring the old `agents:` key without a version (or with a version predating this change) must fail `validate` with an actionable "migrate to workflows" error rather than being silently ignored or auto-converted — mixed `agents:`+`workflows:` input in the same registry is invalid.
- Negative: the omp "independently dispatchable tool" step-shape still has to solve the same standalone-agent-file problems v1 has today (symlink clobbering, #11/#8) — this ADR doesn't remove that surface, just shrinks it to steps that genuinely need it.
- Negative: exactly how "discipline" is named/classified in the schema (a small closed enum? a set of declarative capability requirements per step?), how model-class binding attaches to a step vs. a harness-specific role, and how existing single-step "just run a tool" registries degrade gracefully are all still open — this ADR settles the *shape* of the decision, not the field-level schema, which needs its own follow-up design pass before implementation starts.

## Links
- Issues: #2 (commands), #3 (workflows, workflowz comment: https://github.com/athal7/agentcfg/issues/3#issuecomment-5208583789), #4 (MCP audit), #9 (omp tool enforcement gap), #13 (compose-into-primary), #14 (reserved-name hang)
- Closed as duplicates during this investigation: #10→#7 (opencode `external_directory` drop), #8→#11 (symlink clobbering)
- omp docs referenced directly: `docs/vibe-mode.md`, `docs/magic-keywords.md` (github.com/can1357/oh-my-pi)
