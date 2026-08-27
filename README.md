<p align="center">
  <img src="./docs/images/quota-center-logo.svg" alt="Quota Center icon" width="160" height="160">
</p>

# Quota Center

<p align="center">
  <a href="./README.md"><img src="https://img.shields.io/badge/Language-English-0969da?style=for-the-badge" alt="English"></a>
  <a href="./README.zh-CN.md"><img src="https://img.shields.io/badge/语言-简体中文-d0d7de?style=for-the-badge" alt="简体中文"></a>
</p>

[![CI](https://github.com/RayenAlex/quota-center/actions/workflows/release.yml/badge.svg)](https://github.com/RayenAlex/quota-center/actions/workflows/release.yml)
[![Release](https://img.shields.io/github/v/release/RayenAlex/quota-center)](https://github.com/RayenAlex/quota-center/releases)
[![License](https://img.shields.io/github/license/RayenAlex/quota-center)](LICENSE)

A native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that brings Zhipu, MiniMax, Ark, OpenCode Go, Codex, Gemini, and Grok quota status into one management panel.

Quota Center reuses CPA-native authentication where possible, stores manually added provider credentials in its own private account file, and shows remaining windows, reset times, and provider-specific errors without exposing secrets in the panel or public resource route.

> Quota Center reports the data exposed by each upstream provider. An unavailable quota means that the provider does not expose a stable public endpoint; it is not a claim that the account has no remaining capacity.

## Preview

![Quota Center management panel with account identifiers and update times redacted](docs/images/quota-center-overview.png)

## Features

- **Multi-provider dashboard** — view quota windows, remaining percentages, and reset times in one CPA panel.
- **Native CPA authentication** — syncs Codex, Gemini/Antigravity, and Grok accounts already logged in to CPA.
- **Manual provider accounts** — add Zhipu, MiniMax, Ark, or OpenCode Go credentials from the management panel or configuration.
- **Saved-account autoload** — previously saved manual accounts are loaded and refreshed when the plugin is configured.
- **Safe management boundary** — native CPA credentials are read through CPA auth APIs and are never written, replaced, or deleted by this plugin.
- **Release verification** — tagged releases publish platform archives with detached SHA-256 checksums.

## Supported providers

| Provider | Credential source | Notes |
| --- | --- | --- |
| Zhipu | API key from config or the management panel | Uses the official quota limit endpoint. |
| MiniMax Coding Plan | API key from config or the management panel | Choose International (`minimax.io`) or China (`minimaxi.com`). International uses `https://api.minimax.io/v1/api/openplatform/coding_plan/remains`; China uses `https://api.minimaxi.com/v1/token_plan/remains`. |
| Ark | AccessKey ID + Secret AccessKey | Signs Agent / Coding Plan quota requests. |
| OpenCode Go | API key from config or the management panel | Connections can be saved; no stable public quota endpoint is assumed. |
| Codex | CPA native authentication | Synced from CPA auth files; do not add a second manual credential here. |
| Gemini / Antigravity | CPA native authentication | CPA aliases are normalized to the Gemini provider. |
| Grok | CPA native authentication | Uses an experimental billing interface and reports upstream failures explicitly. |

## Installation

### Manual installation

Download the matching archive and `.sha256` file from [GitHub Releases](https://github.com/RayenAlex/quota-center/releases), verify the checksum, and extract the single library into CPA's plugin directory for the same platform.

The tagged workflow currently publishes:

| Platform | Library in the archive |
| --- | --- |
| Linux amd64 | `quota-center.so` |
| macOS arm64 | `quota-center.dylib` |

For another target, build locally with `make package GOOS=<os> GOARCH=<arch>`.

## Configuration

Set credential environment variables before starting CPA. A minimal Zhipu account looks like this:

```yaml
plugins:
  configs:
    quota-center:
      enabled: true
      timeout: 15s
      accounts:
        - id: zhipu-main
          provider: zhipu
          plan: api-key
          label: Zhipu main
          api_key_env: ZHIPU_API_KEY
```

A multi-provider configuration can combine API-key and AK/SK accounts:

```yaml
plugins:
  configs:
    quota-center:
      timeout: 15s
      accounts:
        - id: minimax-international
          provider: minimax
          plan: coding-plan
          label: MiniMax International
          api_key_env: MINIMAX_CODING_KEY
          endpoint: https://api.minimax.io/v1/api/openplatform/coding_plan/remains

        - id: minimax-china
          provider: minimax
          plan: coding-plan
          label: MiniMax China
          api_key_env: MINIMAX_CHINA_CODING_KEY
          endpoint: https://api.minimaxi.com/v1/token_plan/remains

        - id: ark-agent
          provider: ark
          plan: agent-plan
          label: Ark Agent
          access_key_env: VOLC_ACCESS_KEY
          secret_key_env: VOLC_SECRET_KEY
```

`accounts` is the preferred format. The legacy `plans` array is still accepted and is interpreted as Zhipu accounts for compatibility. For MiniMax, omitting `endpoint` preserves the legacy China (`minimaxi.com`) route; set it explicitly to select International or China. Endpoint overrides must use HTTPS and an allow-listed official host; unsupported custom hosts are rejected during configuration.

## Using the panel

Open **Plugin Management → 额度中心 → status** in the CPA management center.

- When the page is opened from an authenticated CPA management session, the resource shell automatically loads the protected dashboard; an unauthenticated visit keeps the empty shell.
- Use **Add connection** for Zhipu, MiniMax, Ark, and OpenCode Go.
- Use **Sync CPA login accounts** for Codex, Gemini/Antigravity, and Grok.
- Click **Refresh** on an account card to fetch the latest quota windows.
- Switch to the account view to inspect masked credentials and remove manually managed accounts.

The panel never renders a secret value. When a provider has no stable quota endpoint, the card keeps the connection visible and explains why the quota cannot be read.

## Security model

| Boundary | Behavior |
| --- | --- |
| Manual credentials | Stored in `.quota-center-accounts` with private-directory permissions, `0600` file mode, and atomic replacement. |
| CPA-native credentials | Read through `host.auth.list/get`; Codex, Gemini, and Grok entries are never written or deleted by this plugin. |
| Panel and errors | Credentials are masked and known API keys, tokens, and AK/SK values are redacted from errors. |
| Public resource | The initial `/v0/resource/plugins/quota-center/status` response is cache-free and contains no account data; inside an authenticated CPA management session, the browser reuses the existing management authorization to fetch the protected panel. |
| Network requests | Redirects are rejected, provider endpoints are validated, and host callbacks are bounded by response-size and concurrency limits. |

## Management API

All management routes are protected by the CPA Management Key:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/v0/management/plugins/quota-center/status` | Render the authenticated multi-provider dashboard. |
| `POST` | `/v0/management/plugins/quota-center/accounts` | Validate and save a manual provider account. |
| `DELETE` | `/v0/management/plugins/quota-center/accounts?id=<id>` | Delete a manual provider account. |
| `GET` | `/v0/resource/plugins/quota-center/status` | Public resource shell with no credential or cached-account data. |

## Build, test, and release

Run the local checks:

```bash
go test -race ./...
go vet ./...
python3 -m unittest discover -s scripts -p 'test_*.py' -v
```

Build and package the current platform:

```bash
make package
```

Build a specific target and generate its detached checksum:

```bash
make checksums GOOS=linux GOARCH=amd64
```

Release tags must match the value in `VERSION` (`v<version>`). GitHub Actions builds the supported matrix, verifies the archive and checksum, and refuses to replace an existing release tag.

## Project layout

| Path | Purpose |
| --- | --- |
| `*.go` | Plugin ABI, configuration, account storage, provider clients, management API, and tests. |
| `Makefile` | Build, package, and checksum commands. |
| `scripts/` | Archive creation and release-asset verification helpers. |
| `docs/images/` | README logo and sanitized management-panel preview. |
| `.github/workflows/release.yml` | Immutable tagged-release workflow. |

## License

[MIT](LICENSE)
