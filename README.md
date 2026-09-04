# llama-swap fork

This repository tracks
[mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap) with a
small, explicitly classified set of changes. See the upstream repository for
installation instructions and general documentation.

## Upstream candidates

These changes are reviewed, independently testable candidates for upstream.
They do not imply that an upstream issue or pull request exists. Each candidate
lives on a branch based directly on upstream `main`; `release/staging` merges
those branches for combined testing.

### Model action errors

Branch: `candidate/model-action-errors`

Failed model load and unload actions now display a dismissible error dialog in
the Models UI. When the server returns an OpenAI-compatible error envelope, the
dialog includes its message; malformed or empty responses fall back to the HTTP
status. Pending load state is still cleared before the failure reaches the UI.

### Swap freeze

Branch: `candidate/freeze-swaps`

The snowflake button in the sidebar freezes request-driven model swaps. While
it is active, a request that would unload a running model is rejected with HTTP
409. Requests for already running models and loads that do not require an
eviction continue normally.

The freeze is runtime-only and resets to off after restart or configuration
reload. It does not cancel a swap already in progress, prevent TTL expiry,
block manual unloads, or affect shutdown. The control is independent of the
admin PIN.

The same state is available through `GET /api/freeze` and can be changed with
`PUT /api/freeze` using `{"frozen": true}` or `{"frozen": false}`.

## Fork-only changes

### Admin PIN lock

An optional `adminPin` setting adds a UI lock for Activity capture details:

```yaml
adminPin: "1234"
```

- Without `adminPin`, the UI behaves like upstream.
- With it, viewing captured request and response bodies requires a PIN unlock.
- The unlocked state lasts for the current browser session.

This is a lightweight privacy barrier, not authentication. Activity metrics
and other pages remain visible, and capture data is not independently
protected at the API level. See
[discussion #640](https://github.com/mostlygeek/llama-swap/discussions/640).

`adminPin` protects only the Activity capture View control. It does not restrict
`/metrics`, `/api/mcp`, Tailcat admin access, or direct requests to
`/api/captures/{id}`.

## Previously included

Fork features removed after upstream gained equivalent behavior:

- **Startup profile hook:** contributed upstream and available natively since the merge of [PR #1053](https://github.com/mostlygeek/llama-swap/pull/1053). Takes `hooks.on_startup.profile` with any configured profile name; see [issue #992](https://github.com/mostlygeek/llama-swap/issues/992) for the design discussion, and note the earlier top-level proposal in [PR #993](https://github.com/mostlygeek/llama-swap/pull/993) was closed in favor of this shape. No fork migration is needed: configurations using `hooks.on_startup.profile` keep working identically on upstream code.
- **Runtime alias profiles:** removed in the v244 sync after upstream shipped native profiles in [PR #935](https://github.com/mostlygeek/llama-swap/pull/935). Old fork configurations used `aliases:` inside profiles; current upstream configurations use `pins:`.
- **Bidirectional group exclusivity:** removed after the upstream `matrix:` solver proved suitable for the same scheduling use cases. See [issue #215](https://github.com/mostlygeek/llama-swap/issues/215) and [PR #631](https://github.com/mostlygeek/llama-swap/pull/631).
- **Pool-scoped group exclusivity:** removed for the same reason. See [issue #632](https://github.com/mostlygeek/llama-swap/issues/632).

The fork stays close to upstream and keeps each active feature isolated so it can be removed cleanly if upstream gains equivalent behavior.
