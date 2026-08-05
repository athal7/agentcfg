# Wiring `agentcfg apply` into your workflow

`agentcfg` doesn't assume how or when it gets run. These are six equally
valid places to trigger `agentcfg apply` — pick whichever fits how you
already work. None of these is the "one true way"; `agentcfg` itself has
no opinion about which caller invokes it.

The flags used below (`--scope`, `--context`, `--best-effort`, `--strict`)
are real, existing flags on `agentcfg render`/`agentcfg apply` — see
`agentcfg apply --help` or `docs/capabilities.md`'s neighbor,
`docs/schema.md`, for the full flag list.

## 1. zsh `chpwd` hook

Re-project directory-scoped config every time you `cd`, in the background,
without ever blocking your shell:

```zsh
# ~/.zshrc
autoload -U add-zsh-hook

_agentcfg_chpwd() {
  agentcfg apply --scope project --best-effort &
}

add-zsh-hook chpwd _agentcfg_chpwd
```

## 2. direnv `.envrc`

Per-project, opt-in, and only runs when you `cd` into a directory with an
`.envrc` that allows it:

```bash
# .envrc
agentcfg apply --scope project --best-effort
```

## 3. A session manager's launch hook

Example: [agent-of-empires](https://github.com/athal7/agent-of-empires)'s
`on_launch` hook:

```toml
[hooks]
on_launch = ["agentcfg apply --scope project --best-effort"]
```

(This is one example of a session-manager integration among six recipes on
this page — `agentcfg` has no built-in knowledge of agent-of-empires or any
other session manager.)

## 4. git hook

Re-project after every checkout/branch switch, so config always matches
the checked-out branch's registry state:

```bash
#!/bin/sh
# .git/hooks/post-checkout
agentcfg apply --scope project --best-effort
```

(Remember to `chmod +x .git/hooks/post-checkout`.)

## 5. Makefile target

A deliberate, explicit invocation — you want to see it fail:

```makefile
.PHONY: agentcfg-apply
agentcfg-apply:
	agentcfg apply --scope all --strict
```

## 6. CI step

Confirm the registry renders cleanly (no gaps) before anything merges,
without writing any files:

```yaml
- name: Check agentcfg renders without gaps
  run: agentcfg render --scope global --strict
```

## `--best-effort` vs. not

Recipes 1–4 above are **ambient/automatic** invocations — they fire on
every shell prompt, every `cd`, every checkout, every session launch,
often many times a day, often when you're not looking. Nobody wants a
typo'd registry or a missing git remote to spam their terminal or block a
`cd`. All four use `--best-effort`: on any failure, `agentcfg` prints
nothing and exits 0, full stop (set `AGENTCFG_DEBUG=1` if you ever need to
see why it's not doing anything).

Recipes 5–6 are **deliberate** invocations — a human or CI system chose to
run this command right now, specifically to find out whether the registry
is in good shape. Neither uses `--best-effort`: a broken registry, a
disagreeing renderer, or a real write failure should fail loudly, with a
real exit code and a real error message.
