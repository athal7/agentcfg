# Operating agentcfg as a coding agent

This file is the canonical, agent-facing reference for how to edit an
`agentcfg` registry and apply the result. It stays intentionally short —
for the full field-level reference, see [`docs/schema.md`](docs/schema.md).

## Registry file layout

A registry is a directory of YAML files, resolved (in this precedence
order) via `--registry <dir>` → `$AGENTCFG_REGISTRY` →
`$XDG_CONFIG_HOME/agentcfg` → `~/.config/agentcfg`. Only two filenames are
special:

- **`agentcfg.yaml`** — the entry point. `agentcfg` always reads this file
  first; every other file is reached only through its `imports:` list.
- **`local.yaml`** — an *implicit* override file. If present at the
  registry root, it's loaded last, after every import, and each top-level
  key it sets replaces the merged value from every other file outright.
  It's never listed in `imports:` — this is where a personal, git-ignored
  override belongs.

Every other filename (`models.yaml`, `bash.yaml`, `workflow.yaml`, ...) is
just a convention: the loader only cares which top-level keys a file
declares (`harnesses`, `model_classes`, `bash`, `workflow`, `mcp_servers`,
`contexts`, `commands`), not what it's named. Split content across files
however you like, as long as `agentcfg.yaml`'s `imports:` lists them. See
[`docs/schema.md`](docs/schema.md) for every field each of those keys
accepts, its merge rules, and the bash policy compilation model.

## The validate → render --explain → apply sequence

Editing a registry always follows the same sequence:

```sh
agentcfg import               # (optional) import existing configs from opencode, omp, codex, claude
agentcfg validate             # 1. check the registry for errors/warnings
agentcfg render --explain     # 2. preview what apply would write, with no side effects
agentcfg apply                # 3. write native config for every registered harness
```

1. **`agentcfg validate`** loads the registry and reports every schema and
   consistency error and warning. Fix every error (warnings alone don't
   fail the command) before moving on.
2. **`agentcfg render --explain`** builds the same output `apply` would
   produce, purely as a dry run — it never writes anything. Read its
   output before applying to catch an unintended diff.
3. **`agentcfg apply`** writes native configuration for every targeted
   harness. Use `--target <id>` (`opencode`, `omp`, `codex`, `claude`) to
   scope any of the three commands to just one harness.

## Running inside an isolated sandbox

A harness running in an Agent of Empires-style isolated sandbox should
never read or write the host's real `~/.config/agentcfg` or other host
home-directory paths. `agentcfg` resolves its default registry location
and every output path through `$HOME` (directly, or via
`os.UserHomeDir()`), so setting `HOME` to a sandbox directory before
running any `agentcfg` command confines the default registry lookup and
every `HOME`-derived output path to that sandbox — no new flag,
environment variable, or mode is needed for that part of the workflow.
An explicit `AGENTCFG_REGISTRY` or `--registry` path is a separate
input: if it points outside the sandboxed `HOME`, `agentcfg` still reads
that registry from wherever it lives, so it remains an external input
the sandbox doesn't confine.

```sh
export HOME=/path/to/sandbox
agentcfg validate
agentcfg render --explain --target opencode
agentcfg apply --target opencode
```

`AGENTCFG_REGISTRY` (or `--registry`) can point at a registry directory
outside that sandboxed `HOME` — e.g. a registry checked into the
sandboxed project's own repo, resolved from a path `HOME` doesn't cover.
Doing so doesn't break *output* confinement: `apply`'s writes are
resolved against `HOME`, not the registry directory, so they still land
under the sandbox regardless of where the registry itself lives. The
registry read itself, though, is then an external input outside the
sandbox's control rather than something `HOME` confines. Still, prefer
a registry that lives inside the same sandbox directory as `HOME` when
you have the choice — it keeps every path this workflow touches, reads
and writes alike, confined to one directory you can inspect or discard
as a whole, for full confinement, rather than trusting that no future
read is added outside `HOME`.
