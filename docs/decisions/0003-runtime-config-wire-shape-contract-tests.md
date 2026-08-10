# ADR-0003: Renderer output consumed by a harness at runtime needs a decode/schema-based contract test, not a golden-output test

**Status**: Accepted

## Context

`internal/render/omp/omp.go`'s `renderBashPatternsCommand` compiled the registry's global bash policy and serialized each rule as `{"pattern": "...", "decision": "..."}` for `omp config set bash.patterns <json>`.

omp's `BashTool.approval()` (`packages/coding-agent/src/tools/bash.ts`'s `getBashApprovalPatternRules()`) reads `settings.get("bash.patterns")` expecting `{"match": "...", "approval": "allow"|"deny"|"prompt"}` objects — confirmed against omp v17.2.11's published source, its bundled `docs/tools/bash.md`, and its `docs/settings.md` ("Bash command approval patterns" section), all three independently agreeing on `match`/`approval`. `getBashApprovalPatternRules` silently drops any item where `record.match` isn't a string:

```ts
if (typeof record.match !== "string") return undefined;
```

`{pattern, decision}` objects have no `match` key, so every rule decoded to `undefined` and was dropped — `bash.patterns` compiled to an always-empty rule set, and every non-hardcoded-critical bash command fell through to omp's `harnesses.omp.extra.tools.approval.bash: "allow"` (set on the assumption `bash.patterns` independently gated dangerous commands beneath it). Every configured guardrail (`git commit*: ask`, `git push*: ask`, `sudo *: ask`, `chezmoi apply*: ask`, …) was a silent no-op; reproduced live — `git commit`/`git push origin main` executed with zero approval prompt in a real `always-ask`-mode omp session.

`TestRender_LeadAndBuildProducesFourOutputs` already asserted the exact `{pattern, decision}` JSON string as `wantArgv` and passed the whole time — the test never decoded anything or consulted an independent source; it compared the renderer's output against a second hand-typed copy of the same field-name assumption its own author held, so it could never disagree with the bug.

### Two follow-up questions this ADR also answers

**Could `omp config get`/`config list --json` have caught this ("a behavior test that calls the real config accessor")?** No — verified empirically, not assumed. `omp config set bash.patterns '[{"pattern":...,"decision":...}]'` succeeds (the CLI validates only the *top-level* schema type — `bash.patterns` is typed `array`, and `[{...}]` is syntactically a valid array — per `docs/settings.md`'s "Value parsing" table). `omp config get bash.patterns --json` then echoes the exact wrong-shaped objects straight back with zero complaint: `omp config`'s type-check stops at "is this a JSON array/object," never validating the shape of items *inside* an array- or record-typed setting. A round-trip through the real `config get`/`config set` CLI is not a sufficient contract test for this bug class — omp has no CLI-level deep-schema validator to call.

**Does opencode have anything better?** Yes, materially: opencode's config file declares `"$schema": "https://opencode.ai/config.json"`, a real, hosted, `additionalProperties: false`-enforced JSON Schema (draft 2020-12). Vendored at `internal/render/opencode/testdata/opencode-config.schema.json` and exercised by the new `TestRender_MatchesOpencodeConfigSchema`, this **does** catch drift a golden map cannot — verified empirically against four cases: an unknown top-level key, a missing-required-field MCP entry, a bad MCP `type` enum value, and a bad permission enum value are all rejected. But it is not a universal solvent: `additionalProperties: false` is only declared where opencode's key set is genuinely fixed (`Config` itself, `ServerConfig`, `McpLocalConfig`/`McpRemoteConfig`'s own fields, model definitions). `AgentConfig` has no `additionalProperties` restriction at all — a typo'd key inside one agent's block (e.g. `"modee"` instead of `"mode"`) validates cleanly, also verified empirically. `permission`/`tools`/`agent` are dynamically-keyed maps by design (agent names, MCP server ids, glob-pattern permission leaves are genuinely open-ended), so the schema can't close that specific gap without becoming wrong about opencode's actual extensibility. The omp `bash.patterns` bug this ADR fixes is categorically different: `match`/`approval` are *fixed*, known field names on every rule object, not a dynamic key namespace — exactly the shape a schema (or a decode step keyed on the right field names) *can* fully pin down, which is why the omp-side fix uses a decode-and-resolve test rather than reaching for a schema (omp has none to vendor in the first place).

## Decision

**A renderer output that a harness parses at its own runtime (not merely validated by the harness's own "set config" command, which is shape-blind for nested/array-item schemas) needs a test that decodes or schema-validates that output against a source independent of the renderer under test, and — where the wire shape has a genuinely fixed field set — exercises representative inputs through matching semantics equivalent to the real consumer.** A test that only compares the renderer's serialized output against a second copy of the same author's assumption has demonstrated zero power to catch this bug class and is not sufficient on its own.

Two mechanisms landed alongside this ADR, one per harness, because the two harnesses expose genuinely different ground truth to test against:

- **omp** (`TestRender_BashPatternsCommand_MatchesOmpApprovalContract`): decodes `renderBashPatternsCommand`'s JSON into a struct tagged with the literal wire field names copied from omp's own source/docs (`match`, `approval`), then resolves two boundary commands (a guardrail-listed destructive command; an unmatched command hitting the profile's base decision) using `bashpolicy.MatchGlob` and asserts on the resulting *decision*. A `{pattern, decision}` regression decodes both fields to `""` for every rule, so no rule (not even the intended catch-all) ever fires, and both assertions fail.
- **opencode** (`TestRender_MatchesOpencodeConfigSchema`): validates the full rendered `opencode.json` `Object` against opencode's real, vendored, published schema. Catches top-level key drift and any fixed-shape nested object (MCP server entries, in particular) with the same rigor omp's decode test achieves for `bash.patterns`.

Neither mechanism replaces the existing golden/`reflect.DeepEqual` tests in either package — those still answer "did the renderer choose the *values* agentcfg intends" (business logic), which a schema or decode-and-resolve check has no opinion on (a schema-valid document can still contain the wrong model id or the wrong permission decision for the registry's actual intent). The two kinds of test check different properties and both stay. What changed, per the "simplify" question this ADR also settles: `TestRender_LeadAndBuildProducesFourOutputs`'s `bash.patterns` assertion, which existed *specifically* to pin down wire-shape correctness and had already failed at that job for its entire lifetime, was slimmed to a structural check (right argv prefix, right element count) — the payload-correctness question it was never actually answering is now owned solely by the dedicated contract test, so the same property isn't asserted twice, once accurately and once by coincidence.

## Keeping the omp-side contract from going stale

The decode-based omp test only guards against *this* wire shape drifting again once it's correct; it cannot detect omp changing its own schema in some future release. There is no compiler-enforced link to omp's schema — omp is a separate, cross-language (TypeScript/Bun) binary this repo does not import, and (per the question above) has no hosted JSON Schema of its own to vendor the way opencode does. Standing practice going forward for this specific gap:

1. **Cite the exact source consulted, pinned to a version**, in the code comment directly above the renderer function (see `renderBashPatternsCommand`'s doc comment: omp v17.2.11, `packages/coding-agent/src/tools/bash.ts`). `omp.go`'s `Capabilities()` doc comment already established this convention for the `primary_agent_tool_permission` capability boundary; this generalizes it to every runtime-parsed wire shape.
2. **Re-verify the cited claim whenever a maintainer next touches the surrounding code for an unrelated reason**, by re-fetching the cited file at the *current* released omp version and diffing the relevant function against what the comment describes.
3. **Prefer a decode-based test over a golden-output test for every runtime-parsed config surface that has no vendorable schema**, so that if the cited claim in (1) does go stale, CI fails the moment the renderer changes to match a real (but now-wrong-again) shape.

No automated, network-fetching-in-CI cross-repo schema check is adopted for the omp side: there is no published schema to fetch, and a step that scrapes arbitrary GitHub source on every CI run trades a rare, catchable-on-touch staleness risk for a common, unrelated CI-flake risk. The opencode side gets closer to automatic because a real schema exists to vendor — but even there, re-vendoring (`testdata/README.md`) is a manual, on-touch step, not a CI-enforced one, for the same flake-risk reason.

## Alternatives considered

### A — Golden-output test only (status quo before this ADR)
Ruled out: demonstrated above to have zero power to catch a wrong-but-self-consistent wire shape, which is exactly the failure mode that shipped.

### B — Vendor omp's TypeScript settings schema into agentcfg and type-check against it in CI
Not applicable for the reason discovered while answering "does omp have a validator": omp exposes no hosted/published schema and its own CLI (`omp config`) validates only coarse top-level types, not nested item shapes — there is nothing schema-shaped to vendor short of transcribing TypeScript source into Go by hand, which reintroduces the exact "second copy of the same assumption" failure mode this ADR is about. (Opencode is different — see the Decision section — because it *does* publish a real schema.)

### C — Network-fetching CI canary (fetch omp's source at a pinned SHA on every CI run, regex-check for the literal field names)
Rejected: trades a rare, catchable-on-touch staleness risk for a common, unrelated CI-flake risk.

## Consequences

- Positive: `bash.patterns` now round-trips through a decode step keyed on the real wire field names, closing the exact bug class that shipped silently for the lifetime of this renderer.
- Positive: `opencode.json` now validates against opencode's real published schema, catching top-level and fixed-shape nested drift automatically and offline (schema vendored, external `models.dev` `$ref` stripped — see `testdata/README.md`).
- Positive: the redundant, previously-inaccurate golden JSON string in `TestRender_LeadAndBuildProducesFourOutputs` is gone; the property it was trying (and failing) to assert now has exactly one accurate owner.
- Negative: both new tests still trust that *this* commit's citation of the real contract (omp's source, opencode's vendored schema) is accurate at the time it's written. Points (1)-(3) above are a process control, not a technical guarantee, for the omp side.
- Negative: the opencode schema test has a known, verified blind spot — `AgentConfig` and other dynamically-keyed objects (`permission`, `tools`, `agent`) have no `additionalProperties: false`, so a typo'd key *inside* one of those objects validates cleanly. This is a property of opencode's own schema (those namespaces are legitimately open-ended), not a gap in how the test is written; a future contract bug shaped like a typo'd `AgentConfig` key would need a decode-based test analogous to the omp one, not a stronger schema.

## Links

- `internal/render/omp/omp.go`: `renderBashPatternsCommand` (doc comment cites the exact omp v17.2.11 source consulted)
- `internal/render/omp/omp_test.go`: `TestRender_BashPatternsCommand_MatchesOmpApprovalContract`; `TestRender_LeadAndBuildProducesFourOutputs`'s slimmed structural check
- `internal/render/opencode/opencode_test.go`: `TestRender_MatchesOpencodeConfigSchema`; `leadAndBuildFixture` (shared with `TestRender_LeadAndBuildWithOneMCPServer`)
- `internal/render/opencode/testdata/opencode-config.schema.json` + `testdata/README.md`: vendored schema and the intentional `models.dev` `$ref`-stripping edit
- `internal/bashpolicy/match.go`: `MatchGlob` (the shared glob-matching oracle reused by the omp contract test)
- omp source (v17.2.11): `packages/coding-agent/src/tools/bash.ts`'s `getBashApprovalPatternRules`/`BashTool.approval`; `docs/tools/bash.md` and `docs/settings.md`'s `bash.patterns` sections
- opencode: `https://opencode.ai/config.json` (published schema); `opencode debug config` (resolved-config introspection, not a schema validator)
- `docs/capabilities.md`'s `primary_agent_tool_permission` row and `omp.go`'s `Capabilities()` doc comment — the pre-existing "cite the exact source + version investigated" convention this ADR generalizes
