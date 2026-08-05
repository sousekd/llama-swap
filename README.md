# llama-swap fork

This repository is a customized fork of [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap). It stays close to upstream while carrying a small set of changes: two tweaks to the legacy `groups:` scheduling path, an `adminPin` UI lock, and a `defaultProfile` startup setting.

For upstream documentation, installation instructions, and the broader feature set, see the original repository: [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap).

## Fork changes at a glance

- Bidirectional group exclusivity (`groups:` only): conflicting groups unload each other consistently. Fork-only; superseded by upstream's `matrix:` solver.
- Pool-scoped group exclusivity (`groups:` only): an optional `pool` field scopes exclusivity to a named resource boundary. Fork-only; superseded by upstream's `matrix:` solver.
- Admin PIN lock: an optional `adminPin` setting gates viewing Activity capture details behind a UI unlock flow.
- Default profile at startup: an optional `defaultProfile` setting activates a profile on startup and configuration reload. Candidate for a future upstream contribution.

Previously included, now removed: the fork's runtime alias profiles feature, dropped in the v244 sync when upstream shipped its own `profiles:`. See [Runtime alias profiles — removed in the v244 sync](#4-runtime-alias-profiles--removed-in-the-v244-sync) for the migration steps.

## Change details

### 1. Bidirectional group exclusivity

Applies to the legacy `groups:` block only; `matrix:` already handles this.

Upstream exclusivity was effectively one-way: an exclusive group evicted others when it loaded, but loading a non-exclusive group did not always evict a conflicting exclusive group. This fork makes it symmetric:

- loading an exclusive group unloads conflicting non-persistent groups
- loading a non-exclusive group unloads conflicting exclusive non-persistent groups
- `persistent: true` still protects a group from eviction

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

If `qwen-72b` is running and `qwen-7b` is requested, the exclusive group is unloaded first.

### 2. Pool-scoped group exclusivity

Applies to the legacy `groups:` block only; `matrix:` expresses resource boundaries through the solver instead.

Adds an optional `pool` field to groups:

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

### 3. Admin PIN lock for Activity captures

An optional `adminPin` top-level setting for deployments where the UI is broadly accessible but captured request and response bodies should stay restricted:

```yaml
adminPin: "1234"
```

- without `adminPin`, the UI behaves like upstream
- with it, viewing Activity capture bodies requires a PIN unlock
- the unlocked state lasts only for the current browser session

Scope: Activity metrics remain visible, and the Performance and Hardware pages plus the Prometheus `/metrics` and `/api/hardware` endpoints are not gated. This is a lightweight privacy barrier for shared internal deployments, not real security — capture data is not protected at the API level.

### 4. Runtime alias profiles — removed in the v244 sync

This fork used to ship its own `profiles:` block. Upstream shipped a native implementation in [PR #935](https://github.com/mostlygeek/llama-swap/pull/935), released in v244, that is a superset of the fork version, so the fork version was dropped during that sync.

**Breaking change:** the fork used `aliases:` inside a profile; upstream uses `pins:`. The loader ignores the unknown `aliases:` key, leaving the profile with no pins and failing validation at startup:

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

Other differences: pins may shadow real model IDs and target selectors, `/upstream/<model>/...` is rewritten for the active profile, and activation is `PUT /api/profiles/active` with a `{"name": "<name>"}` body.

See upstream's [`docs/configuration.md`](./docs/configuration.md) for the current specification.

### 5. Default profile at startup

Upstream's `profiles:` can only be activated at runtime, so a restart or configuration reload always lands on "no profile". This fork adds an optional `defaultProfile` top-level setting:

```yaml
profiles:
  coding:
    description: "Coding-focused model routing"
    pins:
      llm-code: "gpt-oss-120b"

defaultProfile: "coding"
```

- without it, the UI and API behave like upstream and no profile is active at startup
- it must name a key under `profiles:` or the configuration fails to load with `defaultProfile references unknown profile "..."`
- the profile is active from the very first request, and `/v1/models` reflects its pins immediately
- a configuration reload rebuilds the server and returns to `defaultProfile`
- the runtime selection is still not persisted; switching in the UI lasts until the next restart or reload
- selecting "None" in the UI deactivates profiles for the rest of the process lifetime

The change is deliberately small and free of fork-specific assumptions so it can be offered upstream once exercised in production.

## Scope of this fork

The delta is small and intentional. The two `groups:` tweaks are frozen fork-only features, retained only until `matrix:` has proven itself in day-to-day use. The admin PIN is an optional UI gate. The default profile is a small additive config option held here as a candidate for a future upstream contribution.
