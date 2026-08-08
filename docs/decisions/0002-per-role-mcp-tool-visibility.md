# ADR-0002: MCP tool-visibility is a per-role grant, not harness-wide by default

**Status**: Accepted

## Context

agentcfg's registry declares MCP servers once, globally, harness-neutral: `mcp_servers:` (`internal/registry/schema.go`'s `MCPServer`, L223-236; `docs/schema.md` L290-315) lists each server's name, transport (`remote`+`url` or `local`+`command`), headers, and optional tool allowlist. Separately, each `Agent` in `agents:` carries its own `mcp: []AgentMCP` list (`schema.go` L117-120, L132) naming which of those globally-declared servers it may use, plus an `Ask` glob-pattern list for tools that require confirmation before use — see `examples/registry/agents.yaml`'s `build` agent (L30-32), which is granted `context7` with an `ask` pattern on `resolve-library-id`.

This two-layer shape — one global server catalog, plus a scoped grant list on top of it — was shipped in the initial v1 build (commit `caedf32`) with no separate commit or design note explaining why the scoped layer exists at all, rather than every agent simply seeing every registered server. This ADR backfills that reasoning.

The opencode renderer (`internal/render/opencode/opencode.go`) is where the distinction becomes concrete and is what motivates it:

- `Render` (L59-114) resolves **every** `MCPServer` targeting opencode into the top-level `mcp:` object (L94-104) — opencode's own native config key telling the running opencode process which server processes to spawn or URLs to connect to. This happens unconditionally, regardless of which agents' `mcp:` lists reference a given server.
- The same `Render` sets a harness-wide default in the top-level `tools:` object (L88-92): every registered server's tools start disabled (`<server>_*: false`).
- `renderAgent` (L192-236) is the only place `AgentMCP` is consumed: for each entry in an agent's own `mcp:` list, it re-enables that server's tools for that one agent (`tools[server+"_*"] = true`, L216) in the agent's own `tools` object, and turns any `Ask` pattern into a per-tool `ask` permission entry scoped to that one agent (L217-219).
- `TestRender_LeadAndBuildWithOneMCPServer` (`opencode_test.go` L26-135) confirms the resulting shape: `lead` (no `mcp:` entries) gets no `tools` key of its own at all — it silently inherits the harness-wide `github_*: false` — while `build` (`mcp: [{server: github, ask: [create_*]}]`) gets its own `tools: {github_*: true}` plus `permission.github_create_*: ask`. Every server, including `github`, still appears once in the shared top-level `mcp:` block regardless of which agent(s) reference it.

opencode has no per-role MCP connection lifecycle: `mcp:` is a single, harness-wide list read once at process startup, not something one rendered agent's config can scope independently. There is no way for agentcfg to make opencode start a server only when a particular agent runs — every server declared for opencode is connected at startup whether or not any agent's `mcp:` list ever references it. The only per-agent surface opencode actually exposes is the tool-permission layer (`tools:` glob enablement + `permission.<server>_<pattern>`), which is exactly what `renderAgent` targets.

`Agent` is, today, the only registry construct that compiles 1:1 to a distinct opencode role (a standalone entry under opencode's native `agent:` object with its own `permission`/`tools` block). That is why the grant list lives on `Agent` and is named `AgentMCP` in the schema as shipped — not because the underlying reasoning is inherently about "agents" as an authoring concept, but because `Agent` is currently the only node in the registry that produces one of the per-role slots opencode's `tools:`/`permission` layer scopes independently. ADR-0001 (`0001-workflow-as-primary-abstraction.md`, Proposed, open PR #15) proposes moving the *authoring* unit from standalone `agents:` to workflow steps, with per-harness roles compiled from a step rather than declared directly — but ADR-0001's own decision text still compiles each step to "a standalone subagent with permission blocks" on opencode. The per-role slot this ADR's mechanism depends on survives that change unchanged; only which registry node originates it moves.

## Decision

**Any registry construct that compiles to a distinct per-harness role carries its own MCP grant list, scoped independently of the global server catalog — because opencode has no per-role lazy connect, that grant can only act on the tool-visibility layer, never on the connection layer.**

Every server targeting opencode connects at startup regardless of any role's grants, so scoping the grant at the "which servers get created" layer would be a no-op for opencode — it would still connect to everything either way. The only place agentcfg can act is the permission/visibility surface opencode already exposes per role, so that's where the grant is applied: default-deny every server's tools harness-wide, then re-enable only the servers a given role explicitly lists, with per-tool `ask` on top.

Today, `Agent` is the only registry node that produces one of these per-harness roles, so the grant is expressed as `Agent.mcp: []AgentMCP` — the decision is about the mechanism (global catalog + per-role visibility grant), not about `Agent` specifically being the permanent owner of that field. Whatever registry construct produces a per-harness role next carries the same grant unchanged.

Doing this per-role (rather than making every registered server visible to every role by default) earns its keep two ways, independent of the fact that it can't reduce opencode's connection cost:

1. **Permission boundary, on a renderer that implements the mechanism (opencode today)** — a role that omits a server from its `mcp:` list is provably unable to invoke that server's tools (`<server>_*: false` at the harness level, never overridden for that role). Without this, every role — including a narrowly-scoped one with `bash: deny` — would still be able to discover and call any registered server's full tool surface, which is exactly the kind of shell-out-adjacent bypass `docs/schema.md`'s validation warning about MCP-proxy agents already flags for the opposite direction (an agent granted MCP without `bash: deny`). This guarantee is renderer-specific, not harness-universal — see Consequences for where it doesn't hold today.
2. **Context preservation** — each role's own `tools` object in the rendered `opencode.json` enumerates only the servers it actually needs. A role's tool-choice surface and prompt context stay free of tool schemas for servers it will never use, instead of every role inheriting the union of every registered server's tools.

## Decision matrix

| Criterion | A — Global-only visibility (every role gets every server) | B — Per-role `mcp:` grant list (chosen) | C — Per-role connection provisioning (lazy-start per role) |
|---|:---:|:---:|:---:|
| Reduces opencode's startup connection cost | N/A — opencode connects to the full global `mcp:` block either way | ❌ No — same global `mcp:` block either way | ✅ Would help, if it existed |
| Implementable given opencode's actual architecture | ✅ Trivial | ✅ Already shipped — pure permission-layer feature | ❌ Not possible — opencode has no per-role-session MCP lifecycle to hook into |
| Permission boundary (role can't call a server it wasn't granted) | ❌ None — every role sees every server's tools | ✅ Default-deny + per-role allow + per-tool ask | ✅ Would also get this, if it existed |
| Context/prompt hygiene (role's tool list shows only relevant tools) | ❌ Every role's tools list includes every registered server | ✅ Each role's `tools:` object lists only its own servers | ✅ Same, if it existed |
| Survives a change to the authoring-level unit (e.g. ADR-0001's workflow steps) | ✅ Trivially — there's nothing to reattach | ✅ Grant reattaches to whatever node compiles to the role next | ⚠️ Would also reattach, if it existed |
| Schema/implementation complexity | ✅ Simplest — no grant struct needed | ⚠️ One extra struct + a scoped list, reusing existing `MCPServer` declarations | ❌ Highest — would require agentcfg to manage MCP server lifecycles itself, outside opencode's config format |

## Alternatives considered

### A — Global-only visibility
The simplest possible schema: one `mcp_servers:` list, no second scoped layer, every role implicitly gets every registered server's tools. Ruled out because opencode's renderer has a real per-role permission surface (`tools:` glob + `permission`) that this would leave entirely unused. Every role — including ones with restrictive `permissions.bash`/`edit`/`write` — would see every server's full tool surface in its own `tools` and `permission` blocks: a blast-radius regression (a role meant to be read-only could still discover and call a filesystem-write or GitHub-write MCP tool nobody intended it to have) and a prompt-hygiene regression (every role's context carries tool schemas for servers it never touches).

### C — Per-role connection provisioning
Would be the ideal outcome if opencode supported it: only connect a server's process/URL when a role that actually needs it runs, cutting both startup latency and idle resource use. Ruled out because opencode has no such hook — its native `mcp:` config key is a single, harness-wide list resolved once at process startup; there is no per-role connection lifecycle agentcfg could target without opencode itself changing. Building this in agentcfg would mean managing MCP server processes independently of opencode's own config format (e.g., a proxy layer opencode always connects to, which itself lazily spins up the real server) — a materially larger infrastructure commitment to work around a limitation in a tool agentcfg doesn't control.

## Consequences

- Positive: every role's `tools`/`permission` block in the rendered `opencode.json` names only the servers it was actually granted — verified by `TestRender_LeadAndBuildWithOneMCPServer` (`lead` has no `tools` key at all; `build` has exactly `github_*: true` plus its `ask` pattern).
- Positive: on a renderer that implements the mechanism (opencode today), default-deny at the harness level means a role that omits a server from its `mcp:` list is provably unable to call that server's tools, independent of what any other role in the same registry is granted.
- Positive: because the decision is stated at the "per rendered role" level rather than tied to the `Agent` noun specifically, it composes with ADR-0001 rather than conflicting with it — ADR-0001 relocates the authoring-level unit from standalone `agents:` to workflow steps, but its own compiled output for opencode is still a standalone subagent with a scoped `permission` block per step. That compiled subagent is the same per-role slot this ADR's grant mechanism targets; only the registry node that originates the grant would need to change (from `Agent.mcp:` to whatever field ADR-0001's workflow-step schema introduces), not the mechanism itself.
- Positive: the same grant concept is available to every renderer, even though today only opencode consumes it for tool-visibility scoping — the schema doesn't foreclose another harness gaining equivalent fidelity later.
- Negative: the grant doesn't reduce opencode's own startup cost or resource footprint — every server declared for opencode connects at process start regardless of whether any role ends up using it. An author who assumes `mcp:` attachment means "this server only starts when this role runs" will be wrong; it is strictly a visibility/permission control layered on top of an always-on connection.
- Negative: the permission-boundary guarantee above does not hold on omp. omp currently declares neither `CapMCPToolGlobs` nor any per-agent-ask capability (`internal/render/omp/omp.go`'s `Capabilities()`) and exposes every server it targets through one flat `mcpServers` map with no per-role filtering at all — a role's `mcp:` grant, or its omission, is silently ignored, so the guarantee in Decision point 1 is opencode-only, not a cross-harness property. `internal/render/gaps.go`'s `detectMCPToolGlobsGaps` only fires on an `MCPServer.Tools` allowlist — not on a role's grant or `Ask` list — so an author relying on the grant as a permission boundary on omp gets no warning that it's a no-op there. Making omp fail closed, or adding a dedicated capability gap for unenforced per-role MCP grants, is follow-up work this ADR identifies but does not decide.
- Negative: if ADR-0001 is accepted, the concrete field names this ADR points at (`Agent.MCP`, `AgentMCP`, `renderAgent`) will need a follow-up amendment to reference whatever workflow-step-derived construct carries the grant instead — this ADR documents the mechanism precisely enough to survive that, but the code pointers themselves are a snapshot of the schema as shipped today. **Update:** ADR-0001 is now Accepted and implemented; `Agent.MCP`/`AgentMCP`/`renderAgent` were preserved unchanged — only the container (`workflow.steps` replacing flat `agents:`) and the `Mode`/`ComposeIntoPrimary` fields (replaced by `Role`) changed, so this ADR's grant mechanism and code pointers below still hold as written, apart from the doc/example paths noted in Links.

## Links

- `internal/registry/schema.go`: `AgentMCP` (L117-120), `Agent.MCP` (L132), `MCPServer` (L223-236)
- `internal/render/opencode/opencode.go`: `Render` (L59-114, global `mcp:`/`tools:` blocks), `renderAgent` (L192-236, per-role `tools:`/`ask` permission)
- `internal/render/opencode/opencode_test.go`: `TestRender_LeadAndBuildWithOneMCPServer` (L26-135)
- `internal/render/omp/omp.go`: `Capabilities()` (L43-54, no per-role MCP visibility declared), `Render` (L96-116, flat `mcpServers` map)
- `docs/schema.md`: `workflow:`/`mcp:` (see `## workflow:` and `## mcp_servers:` sections)
- `examples/registry/workflow.yaml`: `build` step's `mcp:` entry
- Issue #4 (MCP audit) — a related, separate follow-up on cross-harness MCP capability-gap fidelity; not the origin of this design
- ADR-0001 (workflow as primary registry abstraction, open PR #15) — complementary, not conflicting: ADR-0001 relocates the authoring-level unit from standalone agents to workflow steps, but preserves per-harness role compilation for opencode (see its Decision section); this ADR's grant mechanism attaches to whatever registry node compiles to that role — `Agent` today, a workflow step's compiled role under ADR-0001
