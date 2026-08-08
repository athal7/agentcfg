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

Every other filename (`models.yaml`, `bash.yaml`, `workflow.yaml`, `mcp.yaml`,
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
| `workflow` | object (`steps:` list) | `Registry.Agents` (flattened from `workflow.steps`) |
| `mcp_servers` | list of MCP server objects | `Registry.MCPServers` |
| `contexts` | list of context objects | `Registry.Contexts` |
| `commands` | list of command objects | `Registry.Commands` |

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
files) both declare `workflow:`, that's a validation error naming both
files — no silent last-write-wins for `harnesses`, `model_classes`,
`workflow`, `mcp_servers`, or `contexts`.

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

## `workflow:`

```yaml
workflow:
  steps:
    - name: build
      description: "Implements changes"
      role: delegate
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

A registry describes exactly one workflow: a single ordered pipeline of
steps (v1 supports linear ordering only — declaration order is the only
ordering signal; DAG-shaped branching is deferred to a future schema
version). Each entry under `workflow.steps` is an `Agent` — a step's
authoring unit. A step's `name` is its own stable identifier (used
verbatim in rendered output, e.g. the omp agent filename); it carries no
harness-compilation meaning by itself. `role` is the field that selects
the target-specific compilation mechanism — see [Role](#role) below:

| field | yaml tag | type | notes |
|---|---|---|---|
| `Name` | `name` | string | required, must be unique across all steps |
| `Description` | `description` | string | optional |
| `Role` | `role` | string | `primary`, `advisory`, or `delegate`; defaults to `delegate` if omitted (see [Role](#role) below) |
| `Class` | `class` | string | required; must name a key present in `model_classes` |
| `Prompt` | `prompt` | object | exactly one of `file` or `text` (see below) |
| `Targets` | `targets` | `[]string` | which renderer IDs this step applies to; omitted/empty means "every renderer" |
| `Steps` | `steps` | `*int` | optional step budget; only some renderers can express this (see `docs/capabilities.md`) |
| `Permissions` | `permissions` | object | see below |
| `MCP` | `mcp` | `[]AgentMCP` | which MCP servers this step may use |

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
| `Ask` | `ask` | `[]string` — tool-name/glob patterns this step must be asked about before use |

### `role`

A step's `role` is the discipline it needs, not which harness-specific
mechanism expresses it — agentcfg compiles `role` to each target
renderer's native primitive:

- **`primary`** — the workflow's one entry point/orchestrator session. At
  most one step registry-wide may set this. Compiles to opencode's
  `default_agent` and to omp's primary session (`APPEND_SYSTEM.md`).
- **`advisory`** — reads and reasons but must not write or edit; must set
  `permissions.edit: deny` and `permissions.write: deny` (validated —
  `agentcfg validate` rejects an advisory step that doesn't). Requires
  the registry to have a `primary` step. Compiles to a real,
  permission-enforced standalone subagent on opencode. On omp, which has
  no per-subagent enforcement surface it's safe to dispatch a restricted
  role to, it's instead spliced into the primary's own prompt (declares
  the `compose_into_primary` capability — see `docs/capabilities.md`):
  multiple advisory steps are appended after the primary's own prompt,
  each under its own labeled section, in declaration order. A renderer
  that doesn't declare `compose_into_primary` (opencode) renders the
  step exactly as if it were unset — a normal standalone subagent.
- **`delegate`** — independently dispatchable, full permissions as
  declared. A standalone agent file on every harness. Only a `delegate`
  step is ever dispatched by name as a standalone omp agent file, so
  `agentcfg validate` rejects `name: plan` for this role specifically —
  it collides with omp's own reserved plan-mode name and hangs when
  dispatched (a `primary` or `advisory` step named `plan` is never
  dispatched by name on omp, so the collision can't occur for them).

`role: advisory` is meant for steps whose job is discipline the primary
should apply when it works directly or dispatches a `task` (a
build/implementer or plan/architect persona) rather than a specialized,
independently dispatchable tool (research, browser QA, etc.) — the
latter should use `role: delegate`.

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

## `commands:`

A custom agent command (opencode's slash-command feature) is exactly one
of two shapes: flat (a single prompt) or structured (an ordered list of
named steps — a multi-step workflow invoked as one command, e.g. a
plan→build→review pipeline). Each entry is a `Command`:

```yaml
commands:
  - name: review
    description: "Reviews the current diff for correctness and style"
    prompt:
      text: "Review the current diff for correctness, style, and test coverage."
      # — or —
      # file: prompts/review.md

  - name: ship
    description: "Plans, implements, and reviews a change end to end"
    steps:
      - name: plan
        prompt:
          text: "Research the codebase and design an approach before writing code."
      - name: build
        prompt:
          text: "Implement the planned change with tests."
      - name: review
        prompt:
          text: "Review the diff for correctness and run the test suite."
```

| field | yaml tag | type | notes |
|---|---|---|---|
| `Name` | `name` | string | required, unique across all commands; must satisfy the Agent Skills spec's naming rule — lowercase letters, digits, and hyphens only, no leading or trailing hyphen, at most 64 characters |
| `Description` | `description` | string | required |
| `Prompt` | `prompt` | object | flat shape: exactly one of `file` or `text` — same shape and validation as an agent's `prompt`. Mutually exclusive with `Steps` — a command sets exactly one of the two |
| `Steps` | `steps` | `[]CommandStep` | structured shape: an ordered, non-empty list of named phases. Mutually exclusive with `Prompt` |

`CommandStep` (one entry under a structured command's `steps:`):

| field | yaml tag | type | notes |
|---|---|---|---|
| `Name` | `name` | string | required, unique within the command (not globally) |
| `Prompt` | `prompt` | object | exactly one of `file` or `text`, same validation as above |

Unlike `agents:`/`mcp_servers:`, `Command` has **no `targets:` field**.
`Agent.Targets`/`MCPServer.Targets` exist because those entities render to
genuinely different, harness-owned artifacts. A command's rendered
artifact is the identical file for every harness that reads it — there's
no per-harness shape to opt a command out of (see "Rendering" below).

### Rendering: Agent Skills `SKILL.md`

Every command renders to `~/.agents/skills/<name>/SKILL.md`. A flat
command's body is its prompt content unchanged:

```markdown
---
name: review
description: Reviews the current diff for correctness and style
---
Review the current diff for correctness, style, and test coverage.
```

A structured command's body flattens its steps into numbered sections,
prefixed with a directive that triggers omp's native `workflowz` magic
keyword (a deterministic multi-subagent pipeline contract — see
`docs/decisions/` and athal7/agentcfg#3's investigation) on a harness
that recognizes it, and reads as inert extra prose on one that doesn't:

```markdown
---
name: ship
description: Plans, implements, and reviews a change end to end
---
Use `workflowz` to run the following phases as a deterministic pipeline via the persistent eval kernel's `agent()`/`parallel()`/`pipeline()` helpers, each phase's output feeding the next.

## 1. plan

Research the codebase and design an approach before writing code.

## 2. build

Implement the planned change with tests.

## 3. review

Review the diff for correctness and run the test suite.
```

This targets the open [Agent Skills](https://agentskills.io) standard
rather than opencode's original, now-legacy `.opencode/command/*.md`
format. Both harnesses agentcfg renders are confirmed — directly against
each one's own discovery code/docs, not assumed — to read the identical
path:

- **opencode**: its skill loader walks project-local
  `.agents/skills/*/SKILL.md` (up to the git worktree root) and global
  `~/.agents/skills/*/SKILL.md`.
- **omp**: its `agents` skill provider — omp's own docs call
  `.agent[s]/skills` "the canonical OMP-native location" — reads the
  identical path at both project and user scope.

Because the discovery path is identical on both harnesses, rendering a
command needs **no per-harness translation**: `internal/render/opencode`
and `internal/render/omp` both declare the `custom_commands` capability
and both call the same shared `render.RenderCommands` helper, which
produces byte-identical output for either — see
`internal/render/commands.go`. This is load-bearing for structured
commands too: the workflowz directive is baked into the rendered content
**unconditionally**, never gated on which renderer produced the file —
both harnesses' plans write the exact same shared path, so content can
never depend on apply order. `structured_workflow_command` (declared
only by omp) is purely informational: it reports, via `doctor`/
`docs/capabilities.md`, that a structured command's native pipeline
behavior only activates on a renderer declaring it — the rendered
content itself never varies. Removing a command from the registry prunes
its previously-rendered `SKILL.md` file and directory on the next
`agentcfg apply`.

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
- a step with `role` other than `primary`/`advisory`/`delegate`; more than
  one `primary` step registry-wide
- a `role: advisory` step that doesn't set both `permissions.edit: deny`
  and `permissions.write: deny`
- a `role: advisory` step when the registry has no `role: primary` step
  (nothing to compose into)
- a `role: delegate` step named `plan` (collides with omp's reserved
  plan-mode name — see [Role](#role))
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
- a command with no `name`, a duplicate `name`, or a `name` that violates
  the Agent Skills naming rule (uppercase, underscore, leading/trailing
  hyphen, or over 64 characters)
- a command with no `description`
- a command that sets neither or both of `prompt`/`steps`
- a command's `prompt`, or a step's `prompt`, that sets neither or both
  of `file`/`text`, or whose `prompt.file` doesn't exist on disk or
  escapes the registry root
- a structured command with a step that has no `name`, or a duplicate
  step `name` within the same command

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
