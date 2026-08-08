## Purpose

Lets a registry author declare that a subagent's prompt content is
discipline the primary agent should apply when it works directly or
dispatches a task, rendering it spliced into the primary's own
whole-session prompt on a harness that supports that, instead of always
producing an independently dispatchable standalone agent file.

## ADDED Requirements

### Requirement: Agent schema accepts compose_into_primary
An agent entry in the registry SHALL accept an optional boolean field
`compose_into_primary`. When absent, it SHALL behave identically to
`compose_into_primary: false` — today's always-standalone rendering.

#### Scenario: Field omitted preserves current behavior
- **WHEN** an agent entry does not set `compose_into_primary`
- **THEN** every renderer treats the agent exactly as it did before this
  capability existed (a standalone agent file where the harness supports
  one)

### Requirement: A primary agent cannot compose into itself
`agentcfg validate` SHALL reject a registry where an agent sets both
`mode: primary` and `compose_into_primary: true`.

#### Scenario: Primary agent sets compose_into_primary
- **WHEN** an agent has `mode: primary` and `compose_into_primary: true`
- **THEN** `agentcfg validate` reports a validation error naming that
  agent, and no output is rendered

### Requirement: compose_into_primary requires a primary agent to exist
`agentcfg validate` SHALL reject a registry where at least one agent sets
`compose_into_primary: true` but the registry has no `mode: primary`
agent.

#### Scenario: No primary agent defined
- **WHEN** an agent sets `compose_into_primary: true` and the registry
  defines zero `mode: primary` agents
- **THEN** `agentcfg validate` reports a validation error naming the
  composing agent, since there is nothing for its content to be composed
  into

### Requirement: A harness that supports composition splices content into the primary's prompt
A renderer that declares support for this capability SHALL, for every
non-primary agent targeting that renderer with `compose_into_primary:
true`: omit that agent's standalone dispatchable agent file, and append
the agent's prompt content as a distinctly labeled section onto the
primary agent's whole-session system-prompt output, positioned after the
primary agent's own prompt content. When more than one agent composes
into the same primary, their sections SHALL appear in the registry's
agent declaration order, each individually labeled by that agent's name.

#### Scenario: Single composed agent
- **WHEN** rendering a registry with one primary agent and one subagent
  targeting omp with `compose_into_primary: true`
- **THEN** no standalone agent file is produced for the composing
  subagent, and omp's system-prompt output contains the primary's own
  prompt followed by a labeled section containing the subagent's prompt

#### Scenario: Multiple composed agents keep declaration order
- **WHEN** rendering a registry with one primary agent and two subagents,
  both targeting omp with `compose_into_primary: true`, declared in a
  specific order
- **THEN** omp's system-prompt output contains both subagents' labeled
  sections in that same declared order, after the primary's own prompt

### Requirement: A harness without composition support falls back to a standalone agent
A renderer that does not declare support for this capability SHALL render
an agent with `compose_into_primary: true` exactly as it would if the
field were unset — a standalone, independently dispatchable agent file —
and SHALL be reported as a capability gap (a reduction, not a silent
drop: the agent's content is still fully expressed, just not spliced into
the primary's prompt) for any such agent.

#### Scenario: Unsupported harness still emits a standalone file
- **WHEN** rendering a registry for a harness that has not implemented
  this capability, where an agent has `compose_into_primary: true`
- **THEN** that harness's output still includes a standalone dispatchable
  file for the agent, and a capability gap is reported noting the
  reduction
