# llama-swap fork

This repository is a customized fork of [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap).

The fork is kept close to upstream, but carries a small set of changes aimed at the legacy `groups:` configuration path, a practical UI/privacy control, and a runtime-switchable alias overlay.

For the upstream project, general installation instructions, and the broader feature set, see the original repository:

- [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap)

## Relationship to the upstream `matrix:` feature

Upstream introduced a solver-based [`matrix:` configuration](https://github.com/mostlygeek/llama-swap/pull/646) that handles concurrent model execution and eviction natively. A configuration must use either `matrix:` or the legacy `groups:`, not both. If you adopt `matrix:`, the bidirectional and pool-scoped behaviors below are already covered by the solver and you do not need this fork.

The two scheduling tweaks in this fork modify only the legacy `groups:` codepath, which remains useful when a `groups:`-based configuration is preferred for its simplicity. The admin PIN lock is independent of the chosen scheduling path.

## Fork changes at a glance

- Bidirectional group exclusivity (`groups:` path only): fixes the one-way exclusivity behavior so conflicting groups unload each other consistently. Related upstream discussion: [issue #215](https://github.com/mostlygeek/llama-swap/issues/215), [PR #631](https://github.com/mostlygeek/llama-swap/pull/631)
- Pool-scoped group exclusivity (`groups:` path only): adds an optional `pool` field so exclusivity applies within a named resource boundary instead of always globally. Related upstream discussion: [issue #632](https://github.com/mostlygeek/llama-swap/issues/632)
- Admin PIN lock for Activity captures: adds an optional `adminPin` setting and a UI unlock flow so sensitive capture data in the Activity panel is not immediately visible to all UI users. Related upstream discussion: [discussion #640](https://github.com/mostlygeek/llama-swap/discussions/640)
- Runtime alias profiles: adds an optional `profiles:` block and a UI dropdown to remap aliases to different models at runtime without a restart or client change. Upstream PR: [#774](https://github.com/mostlygeek/llama-swap/pull/774)

## Change details

### 1. Bidirectional group exclusivity

Applies only to configurations that use the legacy `groups:` block. Configurations that use `matrix:` already get equivalent behavior from the upstream solver.

Upstream group exclusivity was effectively one-way in some cases.
An exclusive group could evict other groups when it loaded, but loading a non-exclusive group did not always evict a conflicting exclusive group that was already running.

This fork makes the behavior symmetric:

- loading an exclusive group unloads conflicting non-persistent groups
- loading a non-exclusive group unloads conflicting exclusive non-persistent groups
- `persistent: true` still protects a group from being evicted

This matters when you use groups to model mutually exclusive workloads such as a large model and a smaller fallback model that share the same underlying resource.

Example:

```yaml
groups:
  big-model:
    exclusive: true
    members: [qwen-72b]

  small-model:
    exclusive: false
    members: [qwen-7b]
```

In this fork, if `qwen-72b` is running and `qwen-7b` is requested, the exclusive group is unloaded first instead of being left behind due to one-way handling.

### 2. Pool-scoped group exclusivity

Applies only to configurations that use the legacy `groups:` block. Configurations that use `matrix:` express resource boundaries through the solver's sets and `evict_costs` instead.

This fork adds an optional `pool` field to group configuration:

```yaml
groups:
  gpu0-large:
    exclusive: true
    pool: GPU-0
    members: [model-a]

  gpu0-small:
    exclusive: false
    pool: GPU-0
    members: [model-b]

  gpu1-large:
    exclusive: true
    pool: GPU-1
    members: [model-c]
```

Behavior:

- groups in the same named pool interact normally for exclusivity and eviction
- groups in different named pools do not affect each other
- a group with no `pool` is global and interacts with all pools
- `persistent: true` still prevents eviction inside its pool

This is useful when you want exclusivity to reflect a real hardware or scheduling boundary.

In the example above:

- `gpu0-large` and `gpu0-small` can evict each other according to exclusivity rules because both are in `GPU-0`
- `gpu1-large` is isolated from the `GPU-0` groups because it uses a different pool
- a group with no `pool` would still be treated as global and could interact with both `GPU-0` and `GPU-1`

### 3. Admin PIN lock for Activity captures

This fork adds an optional `adminPin` top-level configuration setting for deployments where the UI is broadly accessible, but request and response capture data should remain restricted.

Example:

```yaml
adminPin: "1234"
```

Behavior:

- if `adminPin` is not configured, the UI behaves like upstream
- if `adminPin` is configured, the UI exposes an unlock action
- viewing captured Activity request/response bodies requires a successful PIN verification
- the unlocked state is stored only in the current browser session and resets when the tab/session ends

This is meant as a lightweight privacy control for shared internal deployments where users may need access to the general UI, model list, or status views, but should not automatically see other users' captured prompts and responses.

Current scope:

- Activity metrics remain visible
- the protection applies to viewing capture contents from the Activity panel
- the upstream Performance page and Prometheus `/metrics` endpoint are not gated by the PIN
- this is not a full multi-user authentication or role-based access system

Warning:

- this is not true security, only a UI-level barrier
- the current implementation mainly disables the normal UI path for viewing capture details
- the capture data is not independently protected by the PIN at the API level, so users with browser developer tools or direct API access can still retrieve it with minimal effort

### 4. Runtime alias profiles

This fork adds an optional `profiles:` block that remaps aliases to different models at runtime. Switching profiles is done from the UI header dropdown or via `POST /api/profiles/activate/<name>`, and takes effect immediately without restarting llama-swap or reconfiguring clients.

Useful when clients (agents, harnesses, IDE plugins) address models through semantic aliases such as `llm-plan`, `llm-code`, or `image-gen`, and you want to rewire those aliases for a focused workflow without touching client configuration.

Example:

```yaml
models:
  qwen3.6-27b: { cmd: ..., aliases: [llm-plan, llm-code] }
  glm-5.1:     { cmd: ... }
  flux:        { cmd: ..., aliases: [image-gen] }

profiles:
  plan-smarter:
    description: "Use the smarter model for planning; disable image gen"
    aliases:
      llm-plan: "glm-5.1"
      image-gen: ~
```

With `plan-smarter` active, `llm-plan` resolves to `glm-5.1` instead of the default `qwen3.6-27b`, and `image-gen` is disabled so stray requests do not trigger an unrelated model load.

Behavior:

- additive: if `profiles:` is absent the UI dropdown does not appear and behavior is unchanged
- profile targets must resolve in a single step to a model ID, a static alias, or a `setParamsByID` variant key; profile-to-profile chains and shadowing of model IDs are rejected at load time
- `~` (or an empty string) disables an alias while that profile is active
- the active profile is runtime state and does not persist across restarts
- `/v1/models` listings reflect the active profile's alias reassignments

See [`docs/configuration.md`](./docs/configuration.md) for the full specification and [upstream PR #774](https://github.com/mostlygeek/llama-swap/pull/774) for the proposal discussion.

## Scope of this fork

This fork intentionally keeps its public delta small. The two scheduling changes are confined to the legacy `groups:` codepath and do not touch upstream's `matrix:` solver. The admin PIN is a small, optional UI gate. Runtime alias profiles are additive and live behind an opt-in `profiles:` block. The goal is to stay close to upstream while carrying a few targeted changes that are useful in deployments still based on `groups:` or that need runtime alias rerouting.
