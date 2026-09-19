# Native console management

CLIProxyAPI owns the console backend. The separate `cliproxy-console` repository
contains only React/shadcn frontend assets. There is no Node backend or configuration
synchronization process in the runtime architecture.

## Ownership

- Proxy config YAML: provider credentials, routing and core proxy options.
- `<proxy-config>.console.json`: bootstrap data directory, frontend directory, and
  optional localhost compatibility listener. `CLIPROXY_CONSOLE_DATA_DIR` and
  `CLIPROXY_CONSOLE_WEB_DIR` override the respective fields.
- `<dataDir>/console.sqlite`: settings, subscription profiles, usage snapshots,
  recent client folders, and metadata-only request history. Mode 0600.
- `<dataDir>/service-settings.json`: optional macOS service helper configuration.
- Existing provider auth files and Claude conversation directories remain in place.

Settings use nonempty environment override → SQLite → defaults. Supported overrides:
`CLIPROXY_DISPLAY_NAME`, `CLIPROXY_API_KEY`, `CLIPROXY_CLAUDE_MODELS`,
`CLIPROXY_CLI_EFFORT`, `CLIPROXY_CLI_PROFILE`, `CLIPROXY_CLAUDE_BACKGROUND_MODEL`,
`CLIPROXY_CLAUDE_SUBAGENT_MODEL`, `CLAUDE_CONFIG_DIR`, `CLIPROXY_DESKTOP_CONFIG_DIR`.
Environment overrides cannot be overwritten from the UI and are not copied to SQLite.

Account routes use the current native profile/model state directly. Once profiles
are owned by the native console, the old account-gateway route mirror cannot be
edited or reactivated by deleting the last profile. Provider lookup, model support,
credential pinning and exhaustion behavior stay in the existing inference pipeline.
Requests never contact the frontend or perform management HTTP requests.

The proxy saves receipts directly into SQLite on completion, even while no browser
is open. Retention is configurable from 1 to 365 days and is pruned by the proxy.
The history endpoint returns the latest 200 rows, optionally filtered by subscription.
Legacy journal entries are imported idempotently at startup. No frontend import job
or 5,000-request offline limit remains for native history.

## HTTP API

`/v0/management/console/*` uses existing Management API authentication and additionally
requires a loopback network peer and localhost Host/Origin for local administration.
Client-file paths are constrained to the user's home, resolve symlinks, and exclude
proxy storage/config/auth paths. All settings writes validate before committing.
The static frontend is public at `/console/`; it cannot read management data without
an authenticated request. Its browser tab retains the supplied management key until
locked or the tab session ends.

| Route | Methods | Purpose |
| --- | --- | --- |
| `settings` | GET, PUT | Public settings / validated updates; secrets are write-only |
| `profiles`, `profiles/:id` | GET, POST / GET, PUT, DELETE | Named subscriptions |
| `profiles/:id/prefix` | PUT | Set a unique proxy account prefix |
| `profiles/:id/usage` | GET, POST | Saved snapshot / explicit read-only provider refresh |
| `routing` | GET, PUT | Core routing controls through in-process management handlers |
| `desktop` | GET, PUT | Existing Claude Desktop configuration, with backups |
| `cli-command` | POST | Validate model/effort and generate a secret-free CLI command |
| `wire/preview`, `wire` | POST | Preview or apply local client settings |
| `wire/snippet` | GET | Generate a shell function |
| `targets`, `targets/check` | GET | Existing local configuration folders |
| `requests`, `gateway` | GET | Durable request receipts / gateway state |
| `deployment` | GET, PUT | Stage proxy-owned storage/frontend/listener changes |
| `service-settings` | GET, PUT | Optional service-helper parameters |

Account/OAuth/key operations use the existing `/v0/management/*` endpoints directly.
Only fixed provider usage/profile URLs are requested by the usage feature. It does
not generate completions, reset quota, enable extra usage, or purchase credits.

## Migrate the retired Node console

Stop configuration edits during migration. Build the frontend first. The script
requires a new destination, backs up original SQLite and JSON data, preserves profile
IDs and history, and leaves all source data intact:

```sh
python3 scripts/migrate-console.py \
  --source "$HOME/.cliproxy-console" \
  --config /absolute/path/to/proxy.yaml \
  --data-dir "$HOME/.local/share/cliproxyapi/console" \
  --web-dir /absolute/path/to/cliproxy-console/web/dist \
  --compatibility-port 8320
```

Use `--settings-db` if the old console used a custom database path. Bootstrap JSON
may be edited before restarting. The source backup is under the new directory's
`migration-backup/`. The management secret, when imported, is separated into an
owner-only `management.key` used by the operator's optional service helper; it is not
an application setting or exposed by the console API. Service checks can instead use
`CLIPROXY_MGMT_KEY`. Do not commit any of these files.

Deploy the tested proxy release with `scripts/proxy-service.py` on macOS. For the
one-time transition, `CLIPROXY_LEGACY_CONSOLE_URL` permits a pre-upgrade snapshot of
the retiring local console and `CLIPROXY_LEGACY_CONSOLE_LABEL` stops its LaunchAgent
inside the rollback-protected activation. A successful upgrade moves its plist into
the deployment backup so it cannot restart at login. Omit both variables afterward.
Core routing, saved subscription IDs and Desktop selection are checked before/after.

The compatibility port is served by the **same Go proxy process**, binds only to
127.0.0.1, and forwards its root page to `/console/`. Existing inference paths remain
available there; it is not a second backend. Prefer the main proxy port for new clients.

## Claude launcher

The proxy binary owns argument parsing, subscription/model selection and environment
construction. It reads its native database without needing an open console:

```sh
cliproxyapi claude --proxy-config /absolute/path/to/proxy.yaml --list-subscriptions
cliproxyapi claude --proxy-config /absolute/path/to/proxy.yaml --subscription 'Account name'
```

For plain `claude`, install a shell function calling the proxy binary's `claude`
subcommand. The standard Claude binary must remain on PATH. Cloud/Remote Control
and automatic fallback flags are rejected by this local launcher. History remains in
the configured shared Claude directory. Existing client sessions retain their own
configuration; open a new terminal after changing a shell function.

## Validation and rollback

Run `go test ./...`, `python3 -B -m unittest discover -s scripts/tests`, and the two
fake-provider integration scripts with `CLIPROXY_BIN` pointing to the release.
Tests cover authentication, symlink/path restrictions, canonical 1M models, effort
caps, shell execution, storage restart/retention, profile deletion, direct account
pinning, migration preservation and service rollback. The migration never deletes
source settings or client sessions. To restore the old console deployment, restore
the backed-up LaunchAgent and old frontend revision together with the old proxy.
