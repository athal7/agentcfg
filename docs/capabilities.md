<!-- GENERATED FILE. Do not edit by hand — regenerate with `make docs-capabilities` (see scripts/gen-capabilities.sh). Source: internal/render/renderer.go's Capability constants, internal/renderers.All(), and examples/registry. -->

## Agents

| capability | claude | codex | opencode | omp |
|---|---|---|---|---|
| agent_definitions | ✓ | ✓ | ✓ | ✓ |
| primary_agent | ✓ | ≈ | ✓ | ≈ |
| compose_into_primary | ✗ | ✓ | ✗ | ✓ |
| prompt_append | ≈ | ✓ | ≈ | ✓ |
| prompt_file_reference | ✗ | ✗ | ✓ | ✓ |
| agent_steps | ✓ | ✗ | ✓ | ✗ |

## Permissions

| capability | claude | codex | opencode | omp |
|---|---|---|---|---|
| primary_agent_tool_permission | ✓ | ✓ | ✓ | ✗ |
| agent_task_permission | ✓ | ✗ | ✓ | ✗ |
| external_directory_policy | ✗ | ✗ | ✓ | ✗ |

## Model bindings

| capability | claude | codex | opencode | omp |
|---|---|---|---|---|
| model_literal_binding | ✓ | ✓ | ✓ | ≈ |
| model_class_binding | ≈ | ≈ | ≈ | ✓ |

## Bash policies

| capability | claude | codex | opencode | omp |
|---|---|---|---|---|
| bash_unordered_map | ✗ | ✗ | ✓ | ≈ |
| bash_ordered_list | ✗ | ✗ | ≈ | ✓ |
| bash_interior_glob | ✗ | ✗ | ✓ | ✗ |
| per_agent_bash_policy | ✗ | ✗ | ✓ | ✗ |
| global_bash_policy | ✗ | ✗ | ✓ | ✓ |

## MCP servers

| capability | claude | codex | opencode | omp |
|---|---|---|---|---|
| mcp_local_transport | ✓ | ✓ | ✓ | ✓ |
| mcp_remote_transport | ✓ | ✓ | ✓ | ✓ |
| mcp_tool_globs | ✗ | ✓ | ✓ | ✓ |
| mcp_per_tool_ask | ✓ | ✓ | ✓ | ✗ |

## Project policy

| capability | claude | codex | opencode | omp |
|---|---|---|---|---|
| project_model_policy | ✓ | ✓ | ✓ | ✓ |

## Commands

| capability | claude | codex | opencode | omp |
|---|---|---|---|---|
| custom_commands | ✓ | ✓ | ✓ | ✓ |
| structured_workflow_command | ✗ | ✗ | ✗ | ✓ |

## Equivalent capabilities

An `≈` indicates the same registry feature uses a different native mechanism.

| harness | capability | expressed via |
|---|---|---|
| codex | primary_agent | prompt_append |
| omp | primary_agent | prompt_append |
| claude | prompt_append | primary_agent |
| opencode | prompt_append | primary_agent |
| omp | model_literal_binding | model_class_binding |
| claude | model_class_binding | model_literal_binding |
| codex | model_class_binding | model_literal_binding |
| opencode | model_class_binding | model_literal_binding |
| omp | bash_unordered_map | bash_ordered_list |
| opencode | bash_ordered_list | bash_unordered_map |

## Registry gaps

### claude

- **Skipped `per_agent_bash_policy` for `lead`**. this harness has no per-agent bash scoping; only the global bash profile is applied, harness-wide, so per-agent profile overrides are dropped.
- **Skipped `external_directory_policy` for `agent:lead.permissions.external_directory`**. agent "lead" sets permissions.external_directory; this harness has no external-directory access policy, so it was dropped.
- **Reduced `compose_into_primary` for `agent:plan`**. agent "plan" has role: advisory; this harness has no splicing mechanism, so it is rendered as a normal standalone agent instead.
- **Reduced `structured_workflow_command` for `command:ship`**. command "ship" has 3 steps; this harness has no native structured-workflow mechanism, so it behaves as flattened, numbered prose here instead of a deterministic multi-phase pipeline.

### codex

- **Skipped `agent_steps` for `agent:build.steps`**. agent "build" sets steps: 40; this harness has no step-budget mechanism, so the step limit is dropped.
- **Skipped `per_agent_bash_policy` for `lead`**. this harness has no per-agent bash scoping; only the global bash profile is applied, harness-wide, so per-agent profile overrides are dropped.
- **Skipped `external_directory_policy` for `agent:lead.permissions.external_directory`**. agent "lead" sets permissions.external_directory; this harness has no external-directory access policy, so it was dropped.
- **Skipped `agent_task_permission` for `agent:lead.permissions.task`**. agent "lead" sets permissions.task="allow"; this harness has no task-dispatch permission control, so subagent dispatch is always allowed.
- **Skipped `agent_task_permission` for `agent:build.permissions.task`**. agent "build" sets permissions.task="deny"; this harness has no task-dispatch permission control, so subagent dispatch is always allowed.
- **Reduced `structured_workflow_command` for `command:ship`**. command "ship" has 3 steps; this harness has no native structured-workflow mechanism, so it behaves as flattened, numbered prose here instead of a deterministic multi-phase pipeline.

### opencode

- **Reduced `compose_into_primary` for `agent:plan`**. agent "plan" has role: advisory; this harness has no splicing mechanism, so it is rendered as a normal standalone agent instead.
- **Reduced `structured_workflow_command` for `command:ship`**. command "ship" has 3 steps; this harness has no native structured-workflow mechanism, so it behaves as flattened, numbered prose here instead of a deterministic multi-phase pipeline.

### omp

- **Skipped `agent_steps` for `agent:build.steps`**. agent "build" sets steps: 40; this harness has no step-budget mechanism, so the step limit is dropped.
- **Skipped `per_agent_bash_policy` for `lead`**. this harness has no per-agent bash scoping; only the global bash profile is applied, harness-wide, so per-agent profile overrides are dropped.
- **Skipped `primary_agent_tool_permission` for `agent:lead.permissions`**. agent "lead" is the primary agent and sets permissions.edit="deny"/permissions.write="deny"; this harness has no per-agent tool-permission surface for the primary session (only subagents get one), so the restriction is dropped and the primary session keeps full edit/write access.
- **Skipped `external_directory_policy` for `agent:lead.permissions.external_directory`**. agent "lead" sets permissions.external_directory; this harness has no external-directory access policy, so it was dropped.
- **Skipped `agent_task_permission` for `agent:lead.permissions.task`**. agent "lead" sets permissions.task="allow"; this harness has no task-dispatch permission control, so subagent dispatch is always allowed.
- **Skipped `agent_task_permission` for `agent:build.permissions.task`**. agent "build" sets permissions.task="deny"; this harness has no task-dispatch permission control, so subagent dispatch is always allowed.
- **Skipped `mcp_per_tool_ask` for `agent:build.mcp:context7`**. agent "build"'s mcp server "context7" sets per-tool ask patterns [resolve-library-id]; this harness has no per-tool ask-listing, so tools are either fully allowed or fully blocked at the server level.
