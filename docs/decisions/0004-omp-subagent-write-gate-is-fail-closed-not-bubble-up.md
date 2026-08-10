# ADR-0004: omp subagent write-tool gating is fail-closed rejection, not bubble-up approval

**Status**: Accepted

## Context

omp hard-forces every subagent into `tools.approvalMode: yolo`, regardless of the primary session's own mode — confirmed as a literal, unconditional `hasUI: false` at the subagent construction call site in `packages/coding-agent/src/task/executor.ts` (omp v17.2.11), not derived from any setting. Under yolo, `resolveApproval()` (`packages/coding-agent/src/tools/approval.ts`) auto-approves any tool tier when the tool has no *explicit* policy — every MCP tool declares only a bare `"write"` tier (`packages/coding-agent/src/mcp/tool-bridge.ts`), never an explicit policy object — so an MCP write tool absent from `tools.approval` silently resolves to `allow` inside a subagent, no matter what the primary session's `tools.approvalMode` is set to.

This is a known, already-filed upstream limitation: **oh-my-pi#3091**, "Bring up approval requests from subagents / Don't bypass configured tool.approvalMode in subagents" (open, labeled `enhancement`/`triaged`, filed 2026-06-20, still unresolved as of this ADR). The maintainer-side investigation in that thread (comment by `@roboomp`) is the primary source for everything below; it is more authoritative than this repo's own re-derivation, because it was verified against `packages/coding-agent/test/tools/approval-mode.test.ts` and a live scratch session, not just source reading.

Two designs were available to close the resulting exposure (a subagent silently sending a Slack message, creating a Jira issue, sharing a Drive file, etc., with zero signal to the human):

## Decision

**Use omp's confirmed, already-working mechanism — an exact `tools.approval.<tool>: prompt` entry — to make the subagent's call fail as a tool-call error, instead of building or waiting for anything that surfaces a real approval prompt to a human.**

Per oh-my-pi#3091's discussion, an exact `tools.approval` override *is* honored even in forced-yolo mode (`resolveApproval`'s `mode === "yolo"` branch still checks `userPolicy` when the tool itself has no explicit policy). What changes headless is only the *consequence* of a `"prompt"` resolution: `ExtensionToolWrapper` has no UI to render the prompt against, so it throws `Tool "<name>" requires approval but no interactive UI available` back to the model as a tool result, rather than blocking on a human's answer. That is what this repo's fix in `renderToolsApprovalCommand` (see ADR-0003 for the wire-shape half of this same fix) produces: every MCP tool an agent's `mcp: ask` pattern names gets an exact `tools.approval` entry set to `"prompt"`, so a subagent's call to it now rejects with that error instead of silently succeeding under yolo's tier-default `allow`.

This is deliberately **fail-closed rejection, not fail-open silence, and not a real approval prompt.** The subagent's model sees a tool error, the same way it would see any other tool failure — it can retry, try a different tool, or give up and report back that it couldn't complete the action. No human is asked, and no human is told unless the subagent's own final report happens to mention the failure.

## Alternatives considered

### A — Build (or wait for) a bubble-up mechanism that surfaces the prompt in the parent's real UI, like opencode's session-permission inheritance
This is the actual fix `oh-my-pi#3091` is asking for, and it is the only design that gives a human an actual chance to approve the action rather than just being denied it. Rejected for this repo's purposes, for two independent reasons:

1. **It doesn't exist yet.** The issue is `triaged` but unimplemented; the maintainer-acknowledged blockers (default inherit-vs-opt-in, detached/persisted-revive subagents with no UI to prompt against at all, parallel/nested prompt queuing and attribution, AFK timeout policy, and ACP's session-scoped permission model not currently supporting sub-sessions) are all still open policy calls, not pending code. There is nothing to consume today.
2. **agentcfg cannot build it as a hook and ship it as part of this registry-driven renderer.** A custom omp hook (`packages/coding-agent/src/extensibility/hooks`) that relays a subagent's approval request to the parent session is exactly the kind of solution a single consumer repo (e.g. `athal7/dotfiles`) could hand-roll for itself — and one specific prior attempt at exactly that (a bespoke `subagent-permission-relay` hook, built to v2 with still-open design questions, never committed) is documented in that consumer's own history. But agentcfg's job is to compile a harness-neutral registry into *every* consumer's native config, not to ship a bespoke extension file alongside it that only works for whoever happens to install it the same way. A hook is not a renderer output agentcfg can declare, test, or guarantee present — it fails the same bar `docs/decisions/0002`'s Alternative C already rejected for a different reason (omp has no per-role connection lifecycle to hook into): the capability genuinely doesn't exist as a config-level lever, only as bespoke code a specific installation would have to maintain forever, independently, with no agentcfg involvement.

### B — Deny outright (`tools.approval.<tool>: deny`) instead of prompt
Considered and rejected in favor of `prompt`, matching the pattern the registry's own `ask:` field name already implies (a request for confirmation, not a blanket ban) and matching how the same tool id is treated for the *primary* session: under `always-ask`, that same exact `tools.approval.<tool>: prompt` entry produces a real interactive prompt for the primary, and the identical config value degrading gracefully to a hard rejection for a subagent (rather than requiring two different registry-level concepts, one for "ask primary" and a separate one for "deny subagent") is the simpler, single-source-of-truth outcome. Functionally, `deny` and `prompt` both stop the subagent's call today — the difference matters only if oh-my-pi#3091 ships and the primary session starts actually seeing these as real prompts, at which point `deny` would have permanently blocked an action a human might have wanted to approve, while `prompt` degrades gracefully into "ask for real" the moment omp gains the capability.

## Consequences

- Positive: closes real, live exposure — verified before/after against a real deployed session that every MCP write tool this fix targets (slack send/schedule, atlassian create/edit/transition, gmail send/reply, gcalendar respond/create, gdrive share, gdocs replace_text, gsheets set_row_visibility, linear delete/merge, and more) flips from silent `allow` to a rejected tool call inside a subagent.
- Positive: forward-compatible with oh-my-pi#3091 landing — the exact same registry `ask:` field and the exact same rendered `tools.approval: prompt` entries become real human-facing prompts the moment omp implements inheritance, with zero registry or renderer change required on this repo's side.
- Negative, accepted: a human is not asked and is not necessarily told. A subagent that hits this rejection may silently choose a different approach, or may report the failure only if its own summary happens to mention it — this is a strictly better outcome than silent success, but it is not equivalent to being asked and given the chance to say yes.
- Negative, accepted: this only protects tool ids the registry actually knows about (present in some server's `Tools:` list with a corresponding `ask:` pattern). A tool absent from every server's `Tools:` list is invisible to this renderer and stays fully ungated for any caller, subagent or primary — a separate, ongoing data-completeness problem (see the `athal7/dotfiles` consumer repo's own audit trail for the extent of that gap as of this fix, and its explicit note on which servers still need live re-verification).

## Links

- Upstream: `can1357/oh-my-pi#3091` — "Bring up approval requests from subagents / Don't bypass configured tool.approvalMode in subagents" (open)
- `packages/coding-agent/src/task/executor.ts` (omp v17.2.11): the hardcoded `hasUI: false` at the subagent spawn site
- `packages/coding-agent/src/tools/approval.ts`: `resolveApproval` — the yolo-mode branch that still checks an exact user policy
- `packages/coding-agent/test/tools/approval-mode.test.ts`: "per-tool prompt overrides can tighten yolo mode" — the test oh-my-pi#3091's discussion cites as proof this mechanism works
- `internal/render/omp/omp.go`: `renderToolsApprovalCommand` (this repo's fix — expands `ask:` glob patterns into exact `tools.approval: prompt` entries)
- `docs/decisions/0003-runtime-config-wire-shape-contract-tests.md`: the wire-shape half of the same fix (the `bash.patterns` field-name bug found in the same investigation)
- `docs/decisions/0002-per-role-mcp-tool-visibility.md`'s Alternative C: the prior precedent for rejecting a capability agentcfg cannot express as a renderer output
