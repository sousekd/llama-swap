# llama-swap fork

This repository tracks [mostlygeek/llama-swap](https://github.com/mostlygeek/llama-swap) with an admin PIN feature and an upstream candidate for selecting a profile at startup. See the upstream repository for installation instructions and general documentation.

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

### Startup profile hook

An optional `hooks.on_startup.profile` setting activates a profile on startup and after a configuration reload:

```yaml
profiles:
  coding:
    pins:
      llm-code: "gpt-oss-120b"

hooks:
  on_startup:
    profile: "coding"
```

- omitting it keeps upstream behavior and starts with no active profile
- it must name a configured profile
- runtime profile changes are not persisted across restart or reload
- selecting "None" still disables profiles for the current process

This follows the maintainer-preferred design discussed in [issue #992](https://github.com/mostlygeek/llama-swap/issues/992) after the top-level proposal in [PR #993](https://github.com/mostlygeek/llama-swap/pull/993) was closed. Configurations from earlier fork releases must move `defaultProfile: "coding"` to `hooks.on_startup.profile` as shown above; the top-level key is no longer supported.

## Previously included

- **Runtime alias profiles:** removed in the v244 sync after upstream shipped native profiles in [PR #935](https://github.com/mostlygeek/llama-swap/pull/935). Old fork configurations used `aliases:` inside profiles; current upstream configurations use `pins:`.
- **Bidirectional group exclusivity:** removed after the upstream [`matrix:` solver](https://github.com/mostlygeek/llama-swap/pull/646) proved suitable for the same scheduling use cases. See [issue #215](https://github.com/mostlygeek/llama-swap/issues/215) and [PR #631](https://github.com/mostlygeek/llama-swap/pull/631).
- **Pool-scoped group exclusivity:** removed for the same reason. See [issue #632](https://github.com/mostlygeek/llama-swap/issues/632).

The fork stays close to upstream and keeps each active feature isolated so it can be removed cleanly if upstream gains equivalent behavior.
