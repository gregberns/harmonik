---
title: adze
status: explored
type: component
category: internal
source: ~/github/machine-setup
related: [docs/components/external/ntm.md, docs/problems/system-coherence.md]
created: 2026-04-13
updated: 2026-04-13
---

# adze

## Summary
Adze is a declarative machine configuration tool. Single Go binary. It manages the gap between "what a machine should look like" and "what it actually looks like" -- in both directions. Unlike most configuration management tools that only push config to machines, adze can also pull the current machine state back into config, making it uniquely suited for bootstrapping new configurations from existing setups.

## Key Capabilities

### Bidirectional Sync
Config-to-machine (apply) AND machine-to-config (capture). This is the distinguishing feature. An agent can inspect a working machine and generate a reproducible configuration from it, not just apply a pre-written one.

### Dependency Graph Execution
Steps declare what they provide and what they require. Adze resolves the DAG via Kahn's algorithm and executes in topological order. This means step ordering is derived from dependencies, not hard-coded sequences.

### Stateless Resume
Every step re-checks its conditions on every invocation. Already-satisfied steps skip automatically. There is no state file to corrupt, lose, or get out of sync. This makes adze idempotent by design.

### Built-In Steps
20 built-in step types covering package management (brew, apt, cargo, go), language toolchains, shell configuration, system settings, git, and SSH. These cover the common needs of a development machine without custom scripting.

### Custom Steps in YAML
Extensible with custom steps that declare their own provides/requires for dependency integration. Custom steps participate in the same DAG resolution as built-in steps.

### AI-Native Design
The doctor command dumps full machine context in a format designed for AI review. Agents can generate, refine, and troubleshoot configurations without human interpretation of error output.

### Plan Without Execute
The plan command shows what would change; the validate command checks config syntax and dependency resolution without touching the machine. This supports a review-before-apply workflow.

### Drift Detection
The status command shows divergence between the declared configuration and the actual machine state. This is the monitoring capability -- detecting when a machine has drifted from its intended configuration.

### Composable Configs
Include directives with deep merge semantics. Base configurations can be extended and overridden for specific machine roles without duplication.

## Integration Points for Harmonik

Adze is the **environment and infrastructure layer**. Its role in the system:

- **Worker environment setup**: When NTM spawns agent processes on new machines or in new containers, adze ensures the environment has the required toolchains, packages, and configuration.
- **Reproducibility guarantee**: Every agent worker operates in a known, declared environment. Drift detection catches when environments diverge from spec.
- **Infrastructure-as-code for agents**: Agent execution requirements (specific language versions, tools, shell configuration) are declared in adze configs rather than documented in READMEs.
- **Bootstrapping new environments**: The bidirectional sync means a working development machine can be captured and replicated, reducing the cost of onboarding new machines into the fleet.
- **Addresses P04 at the infrastructure level**: System Coherence at Scale (P04) applies to infrastructure too. Adze prevents configuration entropy across machines.

## Limitations and Gaps

- **macOS/Linux only**: No Windows support. Limits the environments agents can operate in.
- **Machine-scoped**: Operates on individual machines, not clusters or container orchestration. No native support for managing fleets of machines as a unit.
- **No remote execution**: Adze runs locally on the target machine. Applying configuration to a remote machine requires SSH access and running adze there.
- **No rollback**: Changes are applied forward. If a step breaks something, the fix is to update the config and re-apply, not to roll back to a prior state.

## Open Questions

1. Should adze configs be part of the harmonik repository, or should each component repo carry its own adze config for its development environment?
2. How does adze interact with containerized agent environments? Is there value in adze-managed containers vs. Dockerfiles?
3. Can adze's drift detection feed into the monitoring subsystem to trigger alerts when agent environments diverge?
