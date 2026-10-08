# llama-swap fork

This fork tracks [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap)
with a small set of isolated changes. See upstream for installation and general
documentation, and the [sync workflow](.github/skills/upstream-sync/SKILL.md) for
fork maintenance.

## Upstream candidates

Each candidate is independently testable on upstream `main`;
`release/staging` merges them for combined testing. A candidate branch does not
imply an upstream issue or pull request exists.

### Model action errors

Branch: `candidate/model-action-errors`

Failed model loads and unloads display a dismissible dialog in the Models UI.
It shows the server's error message when available, otherwise the HTTP status.
Pending load state is cleared before the failure reaches the UI.

### Swap freeze

Branch: `candidate/freeze-swaps`

The sidebar's snowflake button freezes request-driven swaps: requests requiring
an eviction return HTTP 409; already running models and eviction-free loads
continue normally.

Freeze resets after restart or configuration reload. It does not cancel an
active swap, prevent TTL expiry, block manual unloads, or affect shutdown, and
is independent of the admin PIN.

Read it with `GET /api/freeze`; change it with `PUT /api/freeze` using
`{"frozen": true}` or `{"frozen": false}`.

### Chat stream speed estimates

Branch: `candidate/chat-stream-speed-estimates`

Activity estimates prompt and generation speeds for streamed Chat Completions
from observed arrival times and token counts in the final usage chunk. Native
rates take precedence per field.

Clients must request `stream_options.include_usage` when their backend does not
send final usage by default. Buffering and chunk batching affect accuracy, and
compressed, incomplete, or usage-free streams cannot supply estimates.
Requests are not rewritten to enable usage reporting.

### Configured model order

Branch: `candidate/model-config-order`

Model declaration order in YAML controls automatic `${PORT}` allocation
and presentation in `/v1/models`, `/models`, `/running`, and the web UI. Alias
entries stay beside their model, while peers and other virtual entries remain
deterministic after configured local models.

Reordering models can change automatically assigned ports. Configurations that
expose fixed ports to other services should use explicit values in both `cmd`
and `proxy`.

## Fork-only changes

### Admin PIN lock

An optional `adminPin` setting adds a UI lock for Activity capture details:

```yaml
adminPin: "1234"
```

Unlock lasts for the browser session. Without `adminPin`, the UI behaves like
upstream. This is a lightweight privacy barrier, not authentication; metrics
and other pages remain visible. See
[discussion #640](https://github.com/mostlygeek/llama-swap/discussions/640).

`adminPin` protects only the Activity capture View control. It does NOT restrict
access to any API.

## Previously included

Fork features removed after upstream gained equivalent behavior:

- **Startup profile hook:** native `hooks.on_startup.profile`
	([PR #1053](https://github.com/mostlygeek/llama-swap/pull/1053)).
- **Runtime alias profiles:** use upstream profile `pins:` instead of the
	former `aliases:` ([PR #935](https://github.com/mostlygeek/llama-swap/pull/935)).
- **Group exclusivity:** use upstream `matrix:` for
	[bidirectional](https://github.com/mostlygeek/llama-swap/issues/215) and
	[pool-scoped](https://github.com/mostlygeek/llama-swap/issues/632) constraints.
- **Profile target actions:** native in upstream
	([PR #1170](https://github.com/mostlygeek/llama-swap/pull/1170)).