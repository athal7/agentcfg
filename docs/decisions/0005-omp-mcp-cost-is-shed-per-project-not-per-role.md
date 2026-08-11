# ADR-0005: On omp, MCP cost is shed per project via `disabledExtensions`, keyed on the `default` class

**Status**: Accepted

## Context

ADR-0002 established that MCP tool-visibility is a per-role grant, and recorded as a Negative consequence that the guarantee "does not hold on omp": omp declares no `CapMCPToolGlobs` and "exposes every server it targets through one flat `mcpServers` map with no per-role filtering at all", so a role's `mcp:` grant "is silently ignored". It named making omp fail closed as follow-up work it did not decide.

That consequence has a second half ADR-0002 did not draw out, and which turns out to matter more in practice than the permission gap. Because omp mounts every targeted server into its **primary session** unconditionally, the union of every server's tool schemas is a fixed cost that every turn of every omp session pays, in every project. It is not merely a prompt-hygiene regression (ADR-0002's framing, borrowed from the opencode case where per-role `tools:` scoping already fixes it) — it is a floor under the session's context usage that no configuration inside omp can lower.

For a large-window cloud model that floor is affordable. For a small-window local model it can exceed the entire context window, and the failure mode is not graceful:

1. The session's very first request cannot fit the window.
2. omp resolves the configured model, fails, and follows its `retry.fallbackChains` to a cloud model.
3. Nothing reports an error. The session runs, correctly, on the wrong model.

Where the local-model routing exists to keep a class of work off a billed API key, that silent fallback breaches the boundary with no signal anywhere — the config still *says* local, and the session still works. A registry that can route a project's classes to a local model but cannot shed the MCP surface that makes the local model unusable is therefore not expressing a usable policy; it is expressing one that inverts itself at runtime.

Two further facts constrain the available mechanisms, both established against omp 17.2.11 by reading its source and confirming with `omp config get`:

- **omp does not read `<project>/.omp/mcp.json`.** Project-level MCP config is discovered from `<cwd>/mcp.json`, `<cwd>/.mcp.json`, and foreign-harness paths (`.cursor/`, `.vscode/`, `.claude.json`). A file written to `.omp/mcp.json` — the natural sibling of the `.omp/config.yml` this renderer already emits — is silently inert.
- **omp's own MCP capability assigns every server the extension id `mcp:<name>`** (`src/capability/mcp.ts`, `toExtensionId`), and the capability loader drops any item whose id appears in the `disabledExtensions` setting *before* dedupe or connection (`src/capability/index.ts`). That setting resolves through omp's normal settings layering, in which a project `.omp/config.yml` is merged over the user-scope config (`src/config/settings.ts`, `#rebuildMerged`), so a project-scoped list wins.

## Decision

**A server declares the models it is too expensive for (`MCPServer.ExcludeForModels`); omp's `RenderProject` sheds those servers for a project by emitting their `mcp:<name>` ids into the project config's `disabledExtensions`, matched against the `default` class alone.**

Three parts, each load-bearing:

**Declared on the server, not the context.** The cost is a property of the server (how many schemas it carries), and the affordability threshold is a property of the model. Putting the list on `MCPServer` keeps that judgement next to the `tools:` list it is judging, and means adding a context does not require restating exclusions.

**Shed via `disabledExtensions`, not `enabled: false` in an MCP config.** This is the only lever omp actually honors from a project scope (see Context). It is also the stronger of the two available semantics: `enabled: false` merely *suppresses* a server while it still claims its dedupe key, whereas a `disabledExtensions` hit drops the item outright, so the schemas never reach the system prompt at all.

**Keyed on `default`, not on any class.** `disabledExtensions` is session-wide, and the cost it sheds is the primary session's — which runs the `default` class. Matching *any* resolved class would strip the entire MCP surface from a project whose primary model is a large cloud model merely because its `smol` class points at a small local one. This is not hypothetical: a registry whose defaults set `default` to a cloud model and `smol` to a local one has exactly that shape, so every project in it would lose every excludable server.

Only omp consumes `ExcludeForModels`. opencode needs no equivalent: its per-role `tools`/`permission` layer already scopes each server's visibility, so a role that never lists a server pays nothing for it — the exact asymmetry ADR-0002 documented, now with a cost consequence attached rather than only a permission one.

## Decision matrix

| Criterion | A — Do nothing (status quo) | B — `ExcludeForModels` → project `disabledExtensions` (chosen) | C — Project `.omp/mcp.json` with `enabled: false` | D — Shrink the global server set |
|---|:---:|:---:|:---:|:---:|
| Local-model project can hold a session | ❌ Floor exceeds the window | ✅ Floor drops below it | ❌ File is inert — omp never reads it | ✅ But for every project |
| Cloud-model projects keep their full surface | ✅ | ✅ Keyed on `default` | ✅ | ❌ All projects lose it |
| Uses a mechanism omp honors | N/A | ✅ Verified against 17.2.11 | ❌ No | ✅ |
| Schemas kept out of the system prompt | ❌ | ✅ Dropped before connection | ⚠️ Suppressed, still claims dedupe key | ✅ |
| Declared once, near the cost it describes | N/A | ✅ On `MCPServer` | ✅ | ❌ Not expressible per project |
| Secrets/URLs duplicated into project files | N/A | ✅ None — ids only | ❌ Rewrites every server's full config per project | ✅ None |

## Alternatives considered

### C — Project-scoped `.omp/mcp.json` with `enabled: false`
The shape this change was first built in, and the obvious one: omp's user scope already has a `mcp.json`, so a project-local sibling next to `.omp/config.yml` looks symmetric. Ruled out on a fact, not a preference — omp's project MCP discovery does not include that path, so the file is never read. It carried two independent problems besides: it required writing every server's **full** resolved config (URLs, resolved headers) into a per-project file to act as a complete override, duplicating material that is deliberately environment-resolved and never committed; and `enabled: false` only suppresses, which is weaker than the drop this needs.

### D — Shrink the global MCP server set
Cheapest possible fix: stop registering the expensive servers at all. Ruled out because it is not per-project — it would take the same servers away from the cloud-model projects that can afford them and actively use them. The registry's whole point is that one declaration serves projects with different constraints.

### A — Do nothing, and accept the fallback
Tenable only if the fallback were visible and harmless. It is neither: it is silent, and where local routing encodes a billing boundary it inverts the policy it was configured to enforce. A misconfiguration that keeps working while doing the opposite of what it says is worse than one that fails.

### Rejected sub-alternative — key the match on any resolved class
Simpler to write and initially shipped that way in this change; corrected before release. See the third Decision point for why it is wrong, and `TestRenderProject_ExcludeForModelsKeyedOnDefaultClassOnly` for the pin.

## Consequences

- Positive: a project whose `default` class routes to a small-window model gets a session that fits, so the routing that declared that model actually takes effect instead of silently reverting to a cloud model.
- Positive: the fix lives entirely in what agentcfg already writes (`.omp/config.yml`), needs no change to omp, and adds no new file to the project.
- Positive: emitted only when something matches, and `Managed` claims `disabledExtensions` only then — so a project on an unrestricted model gets no key, and apply never prunes a user's own `disabledExtensions` entries.
- Positive: the rendered list is sorted, so the output is stable across runs despite Go's randomized map iteration.
- Negative: exclusions are all-or-nothing per server. A server with 30 tools where 3 would fit the budget must be dropped whole; there is no per-tool trim, because omp offers no per-tool visibility surface to trim with (ADR-0002). `MCPServer.Tools` is a subagent-visibility allowlist, not a primary-session filter, so it cannot substitute.
- Negative: `disabledExtensions` is coarser than the MCP layer — it is omp's general extension denylist, keyed by `mcp:<name>` only by convention of `toExtensionId`. If omp ever renamed that id scheme, exclusions would silently stop applying. `docs/schema.md` records the dependency; a contract test in the spirit of ADR-0003 would be the durable guard and is not yet written.
- Negative: this addresses only the **cost** half of ADR-0002's omp gap. The **permission** half — a role's `mcp:` grant being silently unenforced on omp — remains open and undecided, exactly as ADR-0002 left it. Nothing here makes omp fail closed.
- Neutral: the threshold judgement (which servers are "too expensive", for which model) lives in the registry as data, not in agentcfg. agentcfg does not measure schema cost or validate that the surviving set actually fits the window; a registry can still declare an unusable combination.

## Links

- `internal/registry/schema.go`: `MCPServer.ExcludeForModels`
- `internal/render/omp/omp.go`: `RenderProject`, `excludedServerIDs`, `defaultClass`
- `internal/render/omp/omp_test.go`: `TestRenderProject_ExcludeForModelsMatchDisablesByExtensionID`, `TestRenderProject_ExcludeForModelsKeyedOnDefaultClassOnly`, `TestRenderProject_NoExcludeForModelsMatchOmitsDisabledExtensions`, `TestRenderProject_ExcludeForModelsIgnoresServersNotTargetingOmp`
- `internal/scope/project.go`: `Project` (context resolution + partial class merge that produces the `classes` map this keys on)
- `docs/schema.md`: `mcp_servers:` (`ExcludeForModels` row)
- ADR-0002 (per-role MCP tool visibility) — this ADR resolves the cost half of the omp gap ADR-0002's final Negative consequence identified; the permission half stays open
- ADR-0003 (runtime config wire-shape contract tests) — the pattern a future guard on the `mcp:<name>` id dependency should follow
- omp 17.2.11, for the behaviours this depends on: `src/capability/mcp.ts` (`toExtensionId`), `src/capability/index.ts` (disabled-id drop before dedupe), `src/config/settings.ts` (`#rebuildMerged`, project over user), `src/mcp/config.ts` (`enabled: false` suppression semantics)
