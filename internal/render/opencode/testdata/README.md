# opencode-config.schema.json

Vendored snapshot of opencode's published config schema
(`https://opencode.ai/config.json`, fetched 2026-08-10). Used by
`TestRender_MatchesOpencodeConfigSchema` to validate rendered
`opencode.json` output against opencode's real, `additionalProperties:
false`-enforced schema — see
`docs/decisions/0003-runtime-config-wire-shape-contract-tests.md` for why a
decode/schema-based contract test catches wire-shape drift a golden-string
test cannot.

**One intentional edit**: every sibling `"$ref":
"https://models.dev/model-schema.json#/$defs/Model"` (on `model`,
`small_model`, and `AgentConfig.model`) was stripped. Draft 2020-12
validates a `$ref` alongside sibling keywords (unlike draft-4's
`$ref`-is-exclusive rule), so leaving it in would make every validation run
fetch `models.dev`'s schema over the network — the same CI-flake risk
ADR-0003 rejects for an omp-side network canary, for the same reason here.
Stripping the ref leaves the co-declared `"type": "string"` constraint
intact, which is the only thing this test suite has any business asserting
about a model id string.

Re-vendor by fetching `https://opencode.ai/config.json` again and
re-running the same strip when opencode's schema meaningfully changes and a
maintainer notices this file is stale — there is no automated staleness
check, per the same reasoning.
