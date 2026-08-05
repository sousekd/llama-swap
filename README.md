# llama-swap fork

This repository is a customized fork of [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap).

The fork is kept close to upstream, but carries a small set of changes aimed at the legacy `groups:` configuration path, a practical UI/privacy control, and a startup setting for upstream's profiles feature.

For the upstream project, general installation instructions, and the broader feature set, see the original repository:

- [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap)

## Relationship to the upstream `matrix:` feature

Upstream introduced a solver-based [`matrix:` configuration](https://github.com/mostlygeek/llama-swap/pull/646) that handles concurrent model execution and eviction natively. A configuration must use either `matrix:` or the legacy `groups:`, not both. If you adopt `matrix:`, the bidirectional and pool-scoped behaviors below are already covered by the solver and you do not need this fork.

The two scheduling tweaks in this fork modify only the legacy `groups:` codepath, which remains useful when a `groups:`-based configuration is preferred for its simplicity. The admin PIN lock and the default profile setting are independent of the chosen scheduling path.

Both scheduling tweaks were proposed upstream and declined in favour of `matrix:` ([PR #631](https://github.com/mostlygeek/llama-swap/pull/631) was closed unmerged, [issue #632](https://github.com/mostlygeek/llama-swap/issues/632) was resolved by the solver). They will not be proposed again. They are retained here only until `matrix:` has proven itself in day-to-day use, after which they are expected to be dropped from the fork.

## Fork changes at a glance

- Bidirectional group exclusivity (`groups:` path only): fixes the one-way exclusivity behavior so conflicting groups unload each other consistently. Fork-only, superseded upstream by `matrix:`; not proposed again. Related upstream discussion: [issue #215](https://github.com/mostlygeek/llama-swap/issues/215), [PR #631](https://github.com/mostlygeek/llama-swap/pull/631)
- Pool-scoped group exclusivity (`groups:` path only): adds an optional `pool` field so exclusivity applies within a named resource boundary instead of always globally. Fork-only, superseded upstream by `matrix:`; not proposed again. Related upstream discussion: [issue #632](https://github.com/mostlygeek/llama-swap/issues/632)
- Admin PIN lock for Activity captures: adds an optional `adminPin` setting and a UI unlock flow so sensitive capture data in the Activity panel is not immediately visible to all UI users. Related upstream discussion: [discussion #640](https://github.com/mostlygeek/llama-swap/discussions/640)
- Default profile at startup: adds an optional `defaultProfile` setting so a configured profile is active after a restart or a configuration reload, instead of always starting with none. Candidate for a future upstream contribution; no issue or pull request has been filed yet.

Previously included, now removed: the fork's runtime alias profiles feature. Upstream shipped its own `profiles:` implementation in v244, so the fork version was dropped during that sync. See [Runtime alias profiles — removed in the v244 sync](#4-runtime-alias-profiles--removed-in-the-v244-sync) for the migration steps.

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
- the upstream Hardware page and `/api/hardware` endpoint (added upstream in v247) are not gated by the PIN
- this is not a full multi-user authentication or role-based access system

Warning:

- this is not true security, only a UI-level barrier
- the current implementation mainly disables the normal UI path for viewing capture details
- the capture data is not independently protected by the PIN at the API level, so users with browser developer tools or direct API access can still retrieve it with minimal effort

### 4. Runtime alias profiles — removed in the v244 sync

This fork used to carry its own `profiles:` block for remapping aliases at runtime. Upstream shipped a native profiles feature in [PR #935](https://github.com/mostlygeek/llama-swap/pull/935), released in v244, and the fork version was removed during that sync. The upstream implementation is a superset of what the fork offered, so there is no reason to keep a competing one.

**This is a breaking configuration change.** The fork used `aliases:` inside a profile; upstream uses `pins:`. Upstream's loader ignores the unknown `aliases:` key, which leaves the profile with no pins and fails validation at startup:

```
profiles.<name>.pins must contain at least one entry
```

Rename the key to migrate. Before (fork):

```yaml
profiles:
  plan-smarter:
    description: "Use the smarter model for planning; disable image gen"
    aliases:
      llm-plan: "glm-5.1"
      image-gen: ~
```

After (upstream v244+):

```yaml
profiles:
  plan-smarter:
    description: "Use the smarter model for planning; disable image gen"
    pins:
      llm-plan: "glm-5.1"
      image-gen: ~
```

Behavioral differences to be aware of:

- upstream allows a pin to shadow a real model ID; the fork rejected this at load time
- pin targets may also be selectors, not just model IDs and static aliases
- upstream adds `/upstream/<model>/...` path rewriting for the active profile
- the activation API moved from `POST /api/profiles/activate/<name>` to `PUT /api/profiles/active` with a `{"name": "<name>"}` body
- the profile picker moved from the fork's sidebar dropdown to upstream's header

See upstream's [`docs/configuration.md`](./docs/configuration.md) for the current specification.

### 5. Default profile at startup

Upstream's `profiles:` can only be activated at runtime, through the header dropdown or the API. A restart or a configuration reload always lands on "no profile", which is awkward for unattended deployments that want a specific profile to be in effect as soon as the process is up.

This fork adds an optional `defaultProfile` top-level setting:

```yaml
profiles:
  coding:
    description: "Coding-focused model routing"
    pins:
      llm-code: "gpt-oss-120b"

defaultProfile: "coding"
```

Behavior:

- if `defaultProfile` is not configured, the UI and API behave like upstream and no profile is active at startup
- if configured, it must name a key under `profiles:` or the configuration fails to load with `defaultProfile references unknown profile "..."`
- the profile is active for the very first request, and `/v1/models` reflects its pins immediately
- a configuration reload rebuilds the server, so it also returns to `defaultProfile`
- the runtime selection is still not persisted; switching profiles in the UI lasts only until the next restart or reload
- selecting "None" in the UI still deactivates profiles for the rest of the process lifetime

This change is deliberately kept small and free of fork-specific assumptions so it can be offered upstream once it has been exercised in production.

## Scope of this fork

This fork intentionally keeps its public delta small. The two scheduling changes are confined to the legacy `groups:` codepath and do not touch upstream's `matrix:` solver; they are frozen, will not be re-proposed upstream, and are expected to be removed once `matrix:` has proven itself. The admin PIN is a small, optional UI gate; upstream v244 added no new endpoint that exposes captured request or response bodies, so the gate still covers the only surface that does. The default profile setting is a small additive config option held here as a candidate for a future upstream contribution. The goal is to stay close to upstream while carrying a few targeted changes that are useful in this deployment.
