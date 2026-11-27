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

Failed model loads and unloads display a dismissible dialog wherever
`ModelLoadButton` is used (including the Models page Local and Profiles tabs
and the model detail page). It shows the server's error message when available,
otherwise the HTTP status. Pending load state is cleared before the failure
reaches the UI.

### Swap freeze

Branch: `candidate/freeze-swaps`

The sidebar's snowflake button freezes request-driven swaps: requests requiring
an eviction return HTTP 409; already running models and eviction-free loads
continue normally. That covers `/v1/realtime` too: a websocket for a model that
would need an eviction is refused with the same 409 before the upgrade. When a
client omits `?model=` and names the model in its first `session.update` event,
upstream completes the handshake before the model is known, so the refusal
instead arrives as an OpenAI style `error` event and a close frame on the
already open socket; the model name and the reason appear in its message.

Both `router.use: group` and `router.use: capacity` honor the freeze. In
capacity mode a request that must evict to fit its pools receives the same 409.
The guard runs before the scheduler assigns a swap generation, so freezing
never cancels or interrupts a swap that is already in flight, and the UI
control resolves API paths through upstream's base-path helper.

Freeze resets after restart or configuration reload. It does not cancel an
active swap, prevent TTL expiry, block manual unloads, or affect shutdown, and
is independent of the admin PIN.

Read it with `GET /api/freeze`; change it with `PUT /api/freeze` using
`{"frozen": true}` or `{"frozen": false}`. `/api/events` sends the initial
`freezeChanged` state on connect and broadcasts live `freezeChanged` updates on
toggles and config reloads (mirroring upstream's `pauseChanged` SSE flow), so
all open browser tabs stay in sync automatically. Both endpoints sit on
upstream's `apiChain` and inherit its Basic-auth CSRF protection.

Upstream's pause/resume is a different control: pause unloads every local model
and rejects all local inference with HTTP 503, while freeze leaves the resident
models serving and declines only evictions. Pause wins when both are active,
and it survives a config reload; freeze does not.

Upstream's `fit` selector is another partial decline. A fit selector with
`no_evict: true` answers HTTP 429 when every target would evict a running model.
Selection runs before the swap freeze, so fit still starts an eviction-free
target while swaps are frozen and reports the 409 only when nothing fits. Freeze
covers the paths fit does not: manual `/upstream/...` loads, realtime sessions,
queued requests, and every router mode other than `fit`.

### Chat stream speed estimates

Branch: `candidate/chat-stream-speed-estimates`

After upstream's engine-specific usage extraction, Activity fills a missing
Decode speed (`TokensPerSecond`) for streamed Chat Completions from observed
first-to-last output arrival times and `completion_tokens` in the final usage
chunk (`(completion_tokens - 1) / (lastOutput - firstOutput)`). Measuring
first-to-last output excludes cold model loading, router queueing, and prefill
time, matching upstream's own proxy-side decode measurement in `/v1/realtime`.
Reported rates, including zero, always take precedence, and Prefill speed
(`PromptPerSecond`) is never estimated because request-to-first-token time
includes queueing and model loading. The fallback applies to recognized engines
as well as generic servers and peers when their response omits decode rates.

Clients must request `stream_options.include_usage` when their backend does not
send final usage by default. Buffering and chunk batching affect accuracy, and
compressed, incomplete, usage-free, single-token, or single-write streams
cannot supply an estimate. Requests are not rewritten to enable usage
reporting.

The estimate appears in the existing Decode column, exports, and histograms
without separate provenance fields.

### Configured model order

Branch: `candidate/model-config-order`

Model declaration order in YAML controls automatic `${PORT}` allocation
and presentation in `/v1/models`, `/models`, `/running`, and the web UI. Alias
entries stay beside their model, while peers (including dynamically discovered
peer catalogs and their metadata) and other virtual entries remain
deterministic after configured local models.

On the tabbed Models page, the Local tab keeps configured order inside each
capability section and in the unified list shown when Categories is off.
Sections group by class without re-sorting their rows, while Peers, Selectors,
and Profiles tabs keep upstream's layout and peer refresh controls.

The sidebar's Models sub-menu mirrors the Categories toggle and keeps
configured order both inside each collapsible capability section and in the
flat local list when Categories is off, instead of sorting members by ID. The
Playground model picker also keeps the server's order instead of sorting
client-side: selectors, including upstream's `fit` selectors, arrive after the
local models in deterministic ID order.

Upstream drops redundant picker rows while keeping that order: a model row
always wins over an alias with the same name, and a repeated alias keeps its
first occurrence. The picker therefore shows every model exactly once in
declaration order, even when an alias names another model's ID.

Reordering models can change automatically assigned ports. Configurations that
expose fixed ports to other services should use explicit values in both `cmd`
and `proxy`. Under `router.use: capacity` the order still drives `${PORT}`
allocation and presentation, but pool membership and eviction planning are
unaffected.

### Open in Playground

Branch: `candidate/playground-link`

The model detail page opens the Playground with that model preselected in the
Chat tab, starting a new Chat conversation. Upstream restores the last saved
conversation together with its model and parameters, so the link would lose the
model it selected if it let history restore; the chosen model wins instead.
The conversation on screen is saved before the new one starts, so it stays in
the History menu whenever the history limit keeps more than one. Unlisted
models keep no Playground action because the Playground only selects from the
`/v1/models` listing, which omits them.

### Models tabs scrollbar

Branch: `candidate/models-tabs-scrollbar`

Hides the vertical scrollbar rendered beside the Models page tab strip on
Windows (where `overflow-x-auto` promotes `overflow-y` to `auto` and the
active tab underline protrudes by 1px), matching upstream's
`[scrollbar-width:none] [&::-webkit-scrollbar]:hidden` precedent on the
Playground tab strip.

## Fork-only changes

### Admin PIN lock

An optional `adminPin` setting adds a UI lock for Activity capture details:

```yaml
adminPin: "1234"
```

Unlock lasts for the browser session. Without `adminPin`, the UI behaves like
upstream. This is a lightweight privacy barrier, not authentication; metrics
and other pages remain visible. The UI resolves the endpoints through upstream's
base-path helper, so the lock also works when llama-swap is served under a path
prefix. See
[discussion #640](https://github.com/mostlygeek/llama-swap/discussions/640).

The lock follows upstream's string capture IDs and also applies when captures
are persisted with `store.persistCaptures`.

`adminPin` protects only the Activity capture View control. It does NOT restrict
access to any API or protect the SQLite files.

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
- **Decisions category and badge:** native in upstream
	([PR #1318](https://github.com/mostlygeek/llama-swap/pull/1318)).