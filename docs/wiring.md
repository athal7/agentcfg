# Wiring `agentcfg apply` into your workflow

`agentcfg` doesn't assume how or when it gets run. These are two equally
valid places to trigger `agentcfg apply` — pick whichever fits how you
already work. None of these is the "one true way"; `agentcfg` itself has
no opinion about which caller invokes it.

The flags used below (`--scope`, `--context`, `--strict`) are real, existing
flags on `agentcfg render`/`agentcfg apply` — see `agentcfg apply --help` or
`docs/capabilities.md`'s neighbor, `docs/schema.md`, for the full flag list.

## 1. Makefile target

A deliberate, explicit invocation — you want to see it fail:

```makefile
.PHONY: agentcfg-apply
agentcfg-apply:
	agentcfg apply --scope all --strict
```

## 2. CI step

Confirm the registry renders cleanly (no gaps) before anything merges,
without writing any files:

```yaml
- name: Check agentcfg renders without gaps
  run: agentcfg render --scope global --strict
```

## Invocation

`agentcfg apply` always fails loudly on real errors — a broken registry, a
disagreeing renderer, or a write failure produces a real exit code and a real
error message. There is no `--best-effort` flag: `apply`'s work (read local
YAML, validate, render in-memory, write local files) is deterministic, so a
failure on one invocation fails the same way on every invocation. Silent
swallow-on-failure would hide a persistent bug with no record anywhere.
