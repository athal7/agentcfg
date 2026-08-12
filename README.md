# agentcfg

`agentcfg` compiles one YAML registry — your agents, model classes, bash
policy, and MCP servers — into native configuration for multiple
coding-agent CLI harnesses (`opencode`, `omp`, `codex`, and `claude` in v1). It's
harness-agnostic by design: nothing in the registry format is specific to
any one tool, and where a harness genuinely can't express something the
registry describes, `agentcfg` documents that fidelity gap explicitly
(`agentcfg doctor`) rather than silently dropping it or pretending every
harness is equivalent.

## Install

The primary install path, once a first tagged release has run through
goreleaser (this tap formula doesn't exist yet — this is the intended path,
not something that works today):

```sh
brew install athal7/tap/agentcfg
```

Until then, or as an alternative on any platform with Go installed:

```sh
go install github.com/athal7/agentcfg/cmd/agentcfg@latest
```

## Quickstart

```sh
agentcfg init                 # scaffold a minimal, valid registry
agentcfg validate             # check it for errors/warnings
agentcfg render --explain     # preview what would be written, with no side effects
agentcfg apply                # actually write native config for every registered harness
```

`apply`'s exit code reflects whether *every* targeted harness's own steps
succeeded — a renderer that also runs that harness's own CLI as part of
applying (e.g. `omp` syncing its bash policy via `omp config set ...`)
needs that CLI actually installed; `apply` still writes everything else it
can and reports each target's outcome, but returns non-zero if any step
failed. Use `--target opencode` (or `--target omp`, `--target codex`, `--target claude`) to scope a run to just
one harness. See `agentcfg apply --help` and `docs/wiring.md` for the full
flag set (`--scope`, `--context`, `--strict`, ...).

## Managing your registry

The registry is just a directory of YAML files at `~/.config/agentcfg/`. Edit the files directly, or manage them with whatever you already use for config files — a dotfiles manager like chezmoi, a plain git checkout, symlinks — `agentcfg` neither knows nor cares which.

## Documentation

- [`docs/schema.md`](docs/schema.md) — the registry file layout and every
  field it supports, generated from the actual Go schema.
- [`docs/capabilities.md`](docs/capabilities.md) — a generated matrix of
  which renderer supports which registry feature. A cell is `✓`
  (implemented), `✗` (this harness genuinely has no equivalent — see the
  gap notes below the table for why), or `≈` (the same underlying feature
  is expressed through a different harness-native mechanism, e.g. omp's
  appended system prompt instead of opencode's default-agent key — not a
  gap, just a different shape).
- [`docs/wiring.md`](docs/wiring.md) — two ways to trigger `agentcfg apply`
  (a Makefile target, a CI step).

## License

MIT — see [`LICENSE`](LICENSE).
