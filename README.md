# agentcfg

`agentcfg` compiles one YAML registry — your agents, model classes, bash
policy, and MCP servers — into native configuration for multiple
coding-agent CLI harnesses (`opencode` and `omp` in v1). It's
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
failed. Use `--target opencode` (or `--target omp`) to scope a run to just
one harness. See `agentcfg apply --help` and `docs/wiring.md` for the full
flag set (`--scope`, `--context`, `--strict`, `--best-effort`, ...).

## Managing your registry

The registry is just a directory of YAML files at `~/.config/agentcfg/`. Edit the files directly, or manage them with whatever you already use for config files — a dotfiles manager like chezmoi, a plain git checkout, symlinks — `agentcfg` neither knows nor cares which.

| feature | opencode | omp | notes |
|---|---|---|---|
| agent_definitions | ✓ | ✓ | |
| primary_agent | ✓ | ✗ | opencode-only concept |
| prompt_append | ✗ | ✓ | omp-only concept |
| prompt_file_reference | ✓ | ✓ | |
| agent_steps | ✓ | ✗ | omp has no step-budget mechanism |
| agent_task_permission | ✓ | ✗ | |
| model_literal_binding | ✓ | ✗ | omp uses model classes only |
| model_class_binding | ✗ | ✓ | opencode uses literal bindings only |
| model_alias_only | ✗ | ✗ | not possible in either harness |
| bash_unordered_map | ✓ | ✗ | opencode resolves by most-specific-match |
| bash_ordered_list | ✗ | ✓ | omp resolves by first-match on ordered list |
| bash_bucketed_lists | ✗ | ✗ | not possible in either harness |
| bash_coarse_mode | ✗ | ✗ | not possible in either harness |
| bash_interior_glob | ✓ | ✗ | |
| per_agent_bash_policy | ✓ | ✗ | omp applies global profile harness-wide |
| global_bash_policy | ✓ | ✓ | |
| external_directory_policy | ✓ | ✗ | omp has no external-directory access policy |
| mcp_local_transport | ✓ | ✓ | |
| mcp_tool_globs | ✓ | ✗ | omp exposes all tools without glob filtering |
| mcp_per_tool_ask | ✓ | ✗ | |
| project_model_policy | ✓ | ✓ | |

The table above is generated from the actual renderer code — see
[`docs/capabilities.md`](docs/capabilities.md) for the live, auto-generated
version (run `make docs-capabilities` to refresh).
## Documentation

- [`docs/schema.md`](docs/schema.md) — the registry file layout and every
  field it supports, generated from the actual Go schema.
- [`docs/capabilities.md`](docs/capabilities.md) — which renderer supports
  which registry feature, generated from the actual code.
- [`docs/wiring.md`](docs/wiring.md) — six ways to trigger `agentcfg apply`
  automatically (shell hook, direnv, a session manager, a git hook, a
  Makefile, CI).

## License

MIT — see [`LICENSE`](LICENSE).
