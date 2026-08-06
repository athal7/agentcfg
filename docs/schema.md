# Registry schema

An agentcfg registry is a directory of YAML files. This document describes
every field `agentcfg` actually reads, straight from
`internal/registry/schema.go`, `internal/registry/resolve.go`,
`internal/registry/load.go`, `internal/registry/merge.go`,
`internal/registry/validate.go`, and `internal/bashpolicy/compile.go`. If a
field isn't listed here, the loader doesn't read it.

The default registry location is resolved in this order (same for every
command except `init`, see the `--registry` flag): `--registry <dir>` →
`$AGENTCFG_REGISTRY` → `$XDG_CONFIG_HOME/agentcfg` → `~/.config/agentcfg`.

## Directory layout and file discovery

Only two filenames are special:

- **`agentcfg.yaml`** — the entry point. `agentcfg` always reads this file
  first; every other file is reached only via its `imports:` list.
- **`local.yaml`** — an *implicit* override file. If it exists at the
  registry root, it's loaded last, after every import, with different merge
  semantics (see below). It's never listed in `imports:`.

Every other filename (`models.yaml`, `bash.yaml`, `agents.yaml`, `mcp.yaml`,
`contexts.yaml`, `bash.d/*.yaml`, ...) is just a convention — the loader
doesn't care what a file is named, only what top-level keys it declares.
Split content across files however you like as long as `agentcfg.yaml`'s
`imports:` lists them.

Every registry YAML file (including `agentcfg.yaml` itself) can declare any
of these top-level keys — a single "union" shape backs every file:

| key | YAML type | Go field |
|---|---|---|
| `version` | int | `Registry.Version` |
| `imports` | list of strings | (consumed only from `agentcfg.yaml`; ignored elsewhere) |
| `harnesses` | map of string → harness config | `Registry.Harnesses` |
| `model_classes` | map of string → string | `Registry.ModelClasses` |
| `bash` | bash policy object | `Registry.Bash` |
| `agents` | list of agent objects | `Registry.Agents` |
| `mcp_servers` | list of MCP server objects | `Registry.MCPServers` |
| `contexts` | list of context objects | `Registry.Contexts` |

### `imports:`

Each entry is a path relative to the registry root. An entry containing `*`
is expanded as a glob (`filepath.Glob`) and the matches are sorted
alphabetically before merging — this is what makes a `bash.d/*.yaml` split
deterministic. A non-glob entry that doesn't exist is a hard load error.
Imports are **non-recursive**: a file reached via `imports:` is merged into
the registry, but any `imports:` it declares is ignored — only the
top-level `agentcfg.yaml`'s `imports:` list is honored.

### Merge rules

For every key except `bash`, merging is "declare it in exactly one
non-local file": if `agentcfg.yaml` and an imported file (or two imported
files) both declare `agents:`, that's a validation error naming both files
— no silent last-write-wins for `harnesses`, `model_classes`, `agents`,
`mcp_servers`, or `contexts`.

`bash:` is the one key with finer-grained merging, so a `bash.yaml` +
`bash.d/*.yaml` split can each contribute independently:

- `default_lists` may be declared by exactly one file.
- Each named entry under `lists:` may be declared by exactly one file (two
  different files can each declare a *different* list name).
- Each named entry under `profiles:` may be declared by exactly one file.

Colliding on any of the above (same list name, same profile name, or
`default_lists` declared twice) is a validation error naming both files.

**`local.yaml` is different.** If present, it's loaded last and each
top-level key it sets *replaces* the merged value from every other file
outright — no per-list/per-profile merge for `bash:` here; setting `bash:`
in `local.yaml` replaces the entire compiled `Registry.Bash`, not just one
list or profile. This makes `local.yaml` the place for a personal,
git-ignored override (e.g. a different model class purely on one machine)
without touching the shared files.

### `version`

Plain `int`. Must appear in exactly one non-local file (same collision rule
as every other top-level key). `local.yaml` may override it unconditionally.

## `harnesses:`

```yaml
harnesses:
  opencode:
    out: ~/.config/opencode/opencode.json
  omp:
    agents_dir: ~/.omp/agent/agents
    bash_profile: lead
```

A map from harness/renderer ID (`opencode`, `omp`) to a `HarnessConfig`:

| field | yaml tag | type | meaning |
|---|---|---|---|
| `Out` | `out` | string | (currently informational; renderers hardcode their own output paths — see `docs/capabilities.md` for what each renderer actually writes) |
| `AgentsDir` | `agents_dir` | string | (currently informational, same caveat as `out`) |
| `BashProfile` | `bash_profile` | string | which bash profile `agentcfg explain bash` treats as this harness's relevant profile (defaults to `"global"` if unset) |

**Known asymmetry, documented rather than hidden:** both renderers
currently compile the hardcoded profile named `"global"` when actually
rendering (`bashpolicy.Compile(reg.Bash, "global")` in both
`internal/render/opencode` and `internal/render/omp`) — `bash_profile`
here is read by `agentcfg explain bash` (and only that command) to decide
which profile's resolution to display for a given target. A registry must
always define a `"global"` bash profile for `render`/`apply`/`doctor` to
work at all (`agentcfg init` scaffolds one for you).

## `model_classes:`

```yaml
model_classes:
  default: anthropic/claude-sonnet-4-5
  smol: anthropic/claude-haiku-4-5
  big: anthropic/claude-opus-4-1
```

A flat map of class name → literal model identifier string
(`map[string]string`, no further structure). Two class names are
*reserved* and, if `model_classes` is declared at all, both must be
present: `default` and `smol`. Any number of additional class names
(`big`, whatever you want) may be added; agents and contexts reference
class names, never literal model strings, directly.

## `bash:`

```yaml
bash:
  default_lists: [guardrails]
  lists:
    guardrails:
      "rm -rf /*": ask
      "sudo *": ask
    git:
      "git commit*": ask
      "git status*": allow
  profiles:
    global:
      base: allow
    lead:
      base: allow
      lists: [git]
```

| field | yaml tag | type |
|---|---|---|
| `DefaultLists` | `default_lists` | `[]string` |
| `Lists` | `lists` | `map[string]map[string]Decision` |
| `Profiles` | `profiles` | `map[string]BashProfile` |

A `Decision` is one of exactly three string values: `allow`, `deny`, `ask`.
Every place a decision appears — a list's pattern values, a profile's
`base`, an agent's `permissions.external_directory` values — is validated
against this same three-value set.

A `BashProfile` (one entry under `profiles:`):

| field | yaml tag | type | meaning |
|---|---|---|---|
| `Base` | `base` | `Decision` | the fallback decision for anything not matched by a list — always compiles to the `"*"` pattern |
| `Lists` | `lists` | `[]string` | names of `bash.lists` entries this profile pulls in, in the order given |
| `DefaultLists` | `default_lists` | `*bool` | `nil`/omitted (or `true`) applies `bash.default_lists`'s chain after the profile's own `Lists`; explicit `false` suppresses it entirely |

### Compilation precedence (`bashpolicy.Compile`)

`Compile(policy, profileName)` flattens a named profile into a single
`map[pattern]Decision`. Precedence is **first-wins**, in this order:

1. `profile.Lists`, in the order declared — each list's patterns are added
   if that exact pattern string hasn't already been set by an earlier list
   in this same chain.
2. `policy.DefaultLists`, in the order declared (skipped entirely if the
   profile sets `default_lists: false`) — same first-wins rule against
   whatever's already been set by step 1.
3. `profile.Base`, added as the `"*"` pattern only if `"*"` wasn't already
   set by a list in steps 1–2.

A profile referencing a list name that doesn't exist in `bash.lists`, or a
`profiles:` entry naming a profile `agentcfg explain bash`/an agent's
`permissions.bash.profile` can't find, is a validation/compile error.

### Consuming a compiled policy

`bashpolicy` exposes the same compiled map two ways, matching how each
harness actually resolves overlapping patterns:

- **`AsMap`** — returns the compiled `map[pattern]Decision` unchanged. A
  harness that resolves patterns by its own most-specific-match logic
  (opencode) uses this shape directly.
- **`AsOrderedList`** — converts the same map into a `[]Rule` ordered
  most-specific-pattern-first, for a harness that resolves overlapping
  patterns by walking an ordered list and taking the first match (omp).

Both projections are guaranteed to agree on every command (see
`internal/bashpolicy`'s differential test) — "most-specific match against
an unordered map" and "first match against the specificity-ordered list"
are two views of the same compiled policy, not two independent
implementations that could silently drift apart.

Specificity ordering (`Score`/`Less`/`Order`) ranks patterns by: no
wildcard beats any wildcard; more literal characters beats fewer; fewer
wildcard tokens (`*`, `?`, or a whole `[...]` bracket expression counts as
one token) beats more; an anchored pattern (not starting with a wildcard)
beats a leading-wildcard one; longer beats shorter; lexicographic order is
the final tiebreak. This is why `"git status"` (exact) always beats
`"git status*"` (prefix wildcard) even though both have the same number of
literal characters.

## `agents:`

```yaml
agents:
  - name: build
    description: "Implements changes"
    mode: subagent
    class: default
    steps: 40
    targets: [opencode, omp]
    prompt:
      text: "You implement code changes following TDD."
      # — or —
      # file: prompts/build.md
    permissions:
      task: deny
      edit: allow
      write: allow
      skill: allow
      bash:
        profile: lead
        # — or a bare decision instead of a profile —
        # bash: allow
      external_directory:
        "*": ask
        "~/code/**": allow
    mcp:
      - server: context7
        ask: ["resolve-library-id"]
```

Each entry is an `Agent`:

| field | yaml tag | type | notes |
|---|---|---|---|
| `Name` | `name` | string | required, must be unique across all agents |
| `Description` | `description` | string | optional |
| `Mode` | `mode` | string | `primary` or `subagent`; defaults to `subagent` if omitted. At most one agent registry-wide may be `primary` |
| `Class` | `class` | string | required; must name a key present in `model_classes` |
| `Prompt` | `prompt` | object | exactly one of `file` or `text` (see below) |
| `Targets` | `targets` | `[]string` | which renderer IDs this agent applies to; omitted/empty means "every renderer" |
| `Steps` | `steps` | `*int` | optional step budget; only some renderers can express this (see `docs/capabilities.md`) |
| `Permissions` | `permissions` | object | see below |
| `MCP` | `mcp` | `[]AgentMCP` | which MCP servers this agent may use |

`Prompt`:

| field | yaml tag |
|---|---|
| `File` | `file` — a path relative to the registry root; must exist on disk |
| `Text` | `text` — an inline string |

`Permissions`:

| field | yaml tag | type |
|---|---|---|
| `Task` | `task` | string (`allow`/`deny`) |
| `Edit` | `edit` | string (`allow`/`deny`) |
| `Write` | `write` | string (`allow`/`deny`) |
| `Skill` | `skill` | string (`allow`/`deny`) |
| `Bash` | `bash` | `BashPermission` — see below |
| `ExternalDirectory` | `external_directory` | `map[string]Decision` — path glob → `allow`/`deny`/`ask` |

`BashPermission` accepts two shapes in YAML:

- a bare string, `allow` or `deny`
- an object naming a profile: `{profile: lead}`, where `lead` must exist
  under `bash.profiles`

`AgentMCP` (one entry under `mcp:`):

| field | yaml tag | type |
|---|---|---|
| `Server` | `server` | string — must name an entry in `mcp_servers` |
| `Ask` | `ask` | `[]string` — tool-name/glob patterns this agent must be asked about before use |

## `mcp_servers:`

```yaml
mcp_servers:
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
    # headers:
    #   Authorization: { from: env, name: CONTEXT7_TOKEN, format: "Bearer {}" }
    # targets: [opencode]
    # tools: [resolve-library-id, get-library-docs]

  - name: local-fs
    transport: local
    command: ["mcp-server-filesystem", "--root", "/tmp"]
```

| field | yaml tag | type | notes |
|---|---|---|---|
| `Name` | `name` | string | required, unique |
| `Transport` | `transport` | string | `remote` (requires `url`) or `local` (requires `command`) |
| `URL` | `url` | `Value` | required when `transport: remote` |
| `Command` | `command` | `[]Value` | required (non-empty) when `transport: local` |
| `Targets` | `targets` | `[]string` | which renderer IDs this server applies to; omitted/empty means "every renderer" |
| `Headers` | `headers` | `map[string]Value` | HTTP headers for a remote server |
| `Tools` | `tools` | `[]string` | explicit tool-name allowlist, for a harness that enumerates tools individually rather than enabling a whole namespace by glob. Omitted means no such allowlist was declared (see the `mcp_tool_globs` capability in `docs/capabilities.md`) |

## `contexts:`

```yaml
contexts:
  - match:
      git_remote_owner: athal7
    model_classes:
      default: anthropic/claude-opus-4-1
```

Used by `agentcfg apply --scope project` / `--scope all` (via
`internal/scope.Project`): the directory's git `origin` remote is resolved
to a host/owner pair, matched against `contexts` in order (first match
wins), and the matched context's `model_classes` are overlaid — key by
key, not wholesale — on top of the registry's own `model_classes` before
being projected into a directory-local config file.

| field | yaml tag | type |
|---|---|---|
| `Match` | `match` | `ContextMatch` |
| `ModelClasses` | `model_classes` | `map[string]string` |

`ContextMatch`:

| field | yaml tag | type |
|---|---|---|
| `GitRemoteHost` | `git_remote_host` | string |
| `GitRemoteOwner` | `git_remote_owner` | string |

At least one of `git_remote_host`/`git_remote_owner` is required per
context entry. An unset match field acts as a wildcard (matches anything);
both set means both must match.

## The `Value` type: literal, env, file, command, format

Several fields (`mcp_servers[].url`, `mcp_servers[].command[]`,
`mcp_servers[].headers[*]`) are typed `Value` rather than a plain string,
so they can be resolved at render/apply time instead of stored in plain
text in the registry. A `Value` accepts two YAML shapes:

**A bare string** — treated as a literal:

```yaml
url: https://mcp.context7.com/mcp
```

**An object** naming how to resolve it:

```yaml
headers:
  Authorization:
    from: env          # "env" | "file" | "command"
    name: GITHUB_TOKEN  # from: env — the environment variable name
    # path: ~/.secrets/github-token   # from: file — read and trimmed
    # run: ["op", "read", "op://vault/github/token"]  # from: command — argv, stdout trimmed
    format: "Bearer {}" # optional: "{}" is replaced with the resolved value
```

| field | yaml tag | used by |
|---|---|---|
| `From` | `from` | selects the source: `env`, `file`, or `command` (empty means literal) |
| `Name` | `name` | `from: env` — the environment variable to read (`os.Getenv`; unset resolves to `""`, never an error) |
| `Path` | `path` | `from: file` — a path (supports a leading `~`); the file's contents, whitespace-trimmed |
| `Run` | `run` | `from: command` — an argv slice run directly (no shell), stdout captured and whitespace-trimmed |
| `Format` | `format` | applied after resolution from any source: every literal `{}` in `Format` is replaced with the resolved value |

Resolution (`Value.Resolve()`) is **not** performed during `Load` — it's
side-effecting (reads env/files, runs commands) and deliberately deferred
until a renderer actually needs the value, so `agentcfg validate` never
touches the network/filesystem/processes beyond the registry files
themselves.

## Validation summary

`agentcfg validate` (and every other command, which all call `Load`
internally) runs these checks; anything in the "errors" column fails the
command (non-zero exit) and anything in "warnings" is printed but doesn't:

**Errors:**
- `model_classes` declared but missing `default` or `smol`
- an agent with no `name`, or a duplicate `name`
- an agent with `mode` other than `primary`/`subagent`; more than one
  `primary` agent registry-wide
- an agent with no `class`, or a `class` not present in `model_classes`
- an agent whose `prompt` sets neither or both of `file`/`text`, or whose
  `prompt.file` doesn't exist on disk
- an agent's `permissions.bash.profile` naming a profile that doesn't
  exist, or a bare `permissions.bash` decision that isn't `allow`/`deny`
- an agent's `permissions.external_directory` value that isn't
  `allow`/`deny`/`ask`
- any `bash.lists`/`bash.profiles` decision that isn't `allow`/`deny`/`ask`
- an MCP server with no `name`, a duplicate `name`, an invalid
  `transport`, `transport: remote` with no `url`, or `transport: local`
  with no `command`
- a `contexts` entry with neither `match.git_remote_host` nor
  `match.git_remote_owner` set

**Warnings:**
- an agent that declares `mcp:` servers but doesn't set
  `permissions.bash: deny` (a possible shell-out bypass around an
  MCP-proxy-style agent)

See `examples/registry/` for a complete, validating registry exercising
most of the above (it's also what generates `docs/capabilities.md` — see
that file's header for how).

## Related docs

- [`docs/capabilities.md`](capabilities.md) — which renderer supports which
  registry feature, generated from the actual code.
- [`docs/wiring.md`](wiring.md) — how to trigger `agentcfg apply` from a
  shell hook, direnv, a session manager, a git hook, a Makefile, or CI.
