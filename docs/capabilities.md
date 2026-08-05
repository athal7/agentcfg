<!-- GENERATED FILE. Do not edit by hand — regenerate with `make docs-capabilities` (see scripts/gen-capabilities.sh). Source: internal/render/renderer.go's Capability constants, internal/renderers.All(), and examples/registry. -->

| capability | opencode | omp |
|---|---|---|
| agent_definitions | ✓ | ✓ |
| primary_agent | ✓ | ✗ |
| prompt_append | ✗ | ✓ |
| prompt_file_reference | ✓ | ✓ |
| agent_steps | ✓ | ✗ |
| agent_task_permission | ✓ | ✗ |
| model_literal_binding | ✓ | ✗ |
| model_class_binding | ✗ | ✓ |
| model_alias_only | ✗ | ✗ |
| bash_unordered_map | ✓ | ✗ |
| bash_ordered_list | ✗ | ✓ |
| bash_bucketed_lists | ✗ | ✗ |
| bash_coarse_mode | ✗ | ✗ |
| bash_interior_glob | ✓ | ✗ |
| per_agent_bash_policy | ✓ | ✗ |
| global_bash_policy | ✓ | ✓ |
| external_directory_policy | ✓ | ✗ |
| mcp_local_transport | ✓ | ✓ |
| mcp_tool_globs | ✓ | ✗ |
| mcp_per_tool_ask | ✓ | ✗ |
| project_model_policy | ✓ | ✓ |

opencode: no gaps
omp  skip  agent_steps  agent:build.steps
    agent "build" sets steps: 40; this harness has no step-budget mechanism, so the step limit is dropped.
omp  skip  per_agent_bash_policy  lead
    this harness has no per-agent bash scoping; only the global bash profile is applied, harness-wide, so per-agent profile overrides are dropped.
omp  skip  external_directory_policy  agent:lead.permissions.external_directory
    agent "lead" sets permissions.external_directory; this harness has no external-directory access policy, so it was dropped.
