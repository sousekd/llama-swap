# llama-swap fork

This repository is a customized fork of [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap).

The fork is kept close to upstream, but carries a small set of changes aimed at more complex model-group configurations and a few practical UI/privacy controls.

For the upstream project, general installation instructions, and the broader feature set, see the original repository:

- [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap)

## Fork changes at a glance

- Bidirectional group exclusivity: fixes the one-way exclusivity behavior so conflicting groups unload each other consistently. Related upstream discussions: [issue #215](https://github.com/mostlygeek/llama-swap/issues/215), [PR #631](https://github.com/mostlygeek/llama-swap/pull/631)
- Pool-scoped group exclusivity: adds an optional `pool` field so exclusivity applies within a named resource boundary instead of always globally. Related upstream discussion: [issue #632](https://github.com/mostlygeek/llama-swap/issues/632)
- Admin PIN lock for Activity captures: adds an optional `adminPin` setting and a UI unlock flow so sensitive capture data in the Activity panel is not immediately visible to all UI users. Related upstream discussion: [discussion #640](https://github.com/mostlygeek/llama-swap/discussions/640)

## Change details

### 1. Bidirectional group exclusivity

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
- this is not a full multi-user authentication or role-based access system

## Scope of this fork

This fork intentionally keeps its public delta small.
The goal is to stay close to upstream while carrying a few targeted changes that are useful in more complex local deployments.