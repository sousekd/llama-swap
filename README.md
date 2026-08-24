# llama-swap fork

This repository tracks [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap) with an admin PIN feature. See the upstream repository for installation instructions and general documentation.

## Fork changes

### Admin PIN lock

An optional `adminPin` setting adds a UI lock for Activity capture details:

```yaml
adminPin: "1234"
```

- without `adminPin`, the UI behaves like upstream
- with it, viewing captured request and response bodies requires a PIN unlock
- the unlocked state lasts for the current browser session

This is a lightweight privacy barrier, not authentication. Activity metrics and other pages remain visible, and capture data is not independently protected at the API level. See [discussion #640](https://github.com/mostlygeek/llama-swap/discussions/640).

New upstream surfaces are deliberately not gated: `adminPin` protects only Activity capture details, leaving upstream's `/metrics`, MCP endpoints, `/api/tools`, and reference-docs API exposed.

## Previously included

Fork features removed after upstream gained equivalent behavior:

- **Startup profile hook:** contributed upstream and available natively since the merge of [PR #1053](https://github.com/mostlygeek/llama-swap/pull/1053). Takes `hooks.on_startup.profile` with any configured profile name; see [issue #992](https://github.com/mostlygeek/llama-swap/issues/992) for the design discussion, and note the earlier top-level proposal in [PR #993](https://github.com/mostlygeek/llama-swap/pull/993) was closed in favor of this shape. No fork migration is needed: configurations using `hooks.on_startup.profile` keep working identically on upstream code.
- **Runtime alias profiles:** removed in the v244 sync after upstream shipped native profiles in [PR #935](https://github.com/mostlygeek/llama-swap/pull/935). Old fork configurations used `aliases:` inside profiles; current upstream configurations use `pins:`.
- **Bidirectional group exclusivity:** removed after the upstream `matrix:` solver proved suitable for the same scheduling use cases. See [issue #215](https://github.com/mostlygeek/llama-swap/issues/215) and [PR #631](https://github.com/mostlygeek/llama-swap/pull/631).
- **Pool-scoped group exclusivity:** removed for the same reason. See [issue #632](https://github.com/mostlygeek/llama-swap/issues/632).

The fork stays close to upstream and keeps each active feature isolated so it can be removed cleanly if upstream gains equivalent behavior.
