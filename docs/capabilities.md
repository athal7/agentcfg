<!-- GENERATED FILE. Do not edit by hand — regenerate with `make docs-capabilities` (see scripts/gen-capabilities.sh). Source: internal/render/renderer.go's Capability constants, internal/renderers.All(), and examples/registry. -->

| capability | opencode | omp |
|---|---|---|
| agent_definitions | ✓ | ✓ |
| primary_agent | ✓ | ≈ |
| primary_agent_tool_permission | ✓ | ✗ |
| compose_into_primary | ✗ | ✓ |
| prompt_append | ≈ | ✓ |
| prompt_file_reference | ✓ | ✓ |
| agent_steps | ✓ | ✗ |
| agent_task_permission | ✓ | ✗ |
| model_literal_binding | ✓ | ≈ |
| model_class_binding | ≈ | ✓ |
| bash_unordered_map | ✓ | ≈ |
| bash_ordered_list | ≈ | ✓ |
| bash_interior_glob | ✓ | ✗ |
| per_agent_bash_policy | ✓ | ✗ |
| global_bash_policy | ✓ | ✓ |
| external_directory_policy | ✓ | ✗ |
| mcp_local_transport | ✓ | ✓ |
| mcp_remote_transport | ✓ | ✓ |
| mcp_tool_globs | ✓ | ✗ |
| mcp_per_tool_ask | ✓ | ✗ |
| project_model_policy | ✓ | ✓ |
| custom_commands | ✓ | ✓ |

≈ = same underlying feature, expressed via a different harness-native mechanism (not a gap):
omp  primary_agent — via prompt_append
opencode  prompt_append — via primary_agent
omp  model_literal_binding — via model_class_binding
opencode  model_class_binding — via model_literal_binding
omp  bash_unordered_map — via bash_ordered_list
opencode  bash_ordered_list — via bash_unordered_map

opencode  reduction  compose_into_primary  agent:plan
    agent "plan" has role: advisory; this harness has no splicing mechanism, so it is rendered as a normal standalone agent instead.
omp  skip  agent_steps  agent:build.steps
    agent "build" sets steps: 40; this harness has no step-budget mechanism, so the step limit is dropped.
omp  skip  per_agent_bash_policy  lead
    this harness has no per-agent bash scoping; only the global bash profile is applied, harness-wide, so per-agent profile overrides are dropped.
omp  skip  primary_agent_tool_permission  agent:lead.permissions
    agent "lead" is the primary agent and sets permissions.edit="deny"/permissions.write="deny"; this harness has no per-agent tool-permission surface for the primary session (only subagents get one), so the restriction is dropped and the primary session keeps full edit/write access.
omp  skip  external_directory_policy  agent:lead.permissions.external_directory
    agent "lead" sets permissions.external_directory; this harness has no external-directory access policy, so it was dropped.
omp  skip  agent_task_permission  agent:lead.permissions.task
    agent "lead" sets permissions.task="allow"; this harness has no task-dispatch permission control, so subagent dispatch is always allowed.
omp  skip  agent_task_permission  agent:build.permissions.task
    agent "build" sets permissions.task="deny"; this harness has no task-dispatch permission control, so subagent dispatch is always allowed.
omp  skip  mcp_per_tool_ask  agent:build.mcp:context7
    agent "build"'s mcp server "context7" sets per-tool ask patterns [resolve-library-id]; this harness has no per-tool ask-listing, so tools are either fully allowed or fully blocked at the server level.
