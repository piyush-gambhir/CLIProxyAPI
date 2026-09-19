# Piyush's CLIProxyAPI fork

Upstream: https://github.com/router-for-me/CLIProxyAPI
Fork: https://github.com/piyush-gambhir/CLIProxyAPI

`main` mirrors upstream without custom commits. `piyush` holds our changes and is
the deployment branch. Keep proxy changes small; the separate public
`piyush-gambhir/cliproxy-console` repository owns the UI and account-selection
experience. Preserve the upstream license and attribution.

## Bring in upstream changes

Start with a clean checkout. Review the upstream diff before merging.

```sh
git fetch upstream main --tags
git switch main
git merge --ff-only upstream/main
git push origin main
git switch piyush
git merge main
# Resolve any conflicts and review the result before testing.
go test ./... -timeout 120s
./scripts/build-piyush.sh piyush-YYYY.MM.DD.N
git push origin piyush
```

Never deploy an uncommitted build. Release names are immutable. Tag the tested
commit, then publish the branch and tag. Use a new release name for each build. Inherited release, Docker publishing, and
main-to-dev retarget jobs are restricted to the upstream repository on this branch;
our tags identify local builds and do not publish upstream-style packages.

## Build and deploy locally

The build includes the fork commit, release name, and build time. Configuration
and credentials live outside this repository. The example below assumes the
console and fork checkouts are siblings.

```sh
./scripts/build-piyush.sh piyush-YYYY.MM.DD.N
CLIPROXY_BIN="$PWD/bin/piyush-YYYY.MM.DD.N/cliproxyapi" \
  node scripts/verify-routing.mjs
python3 scripts/proxy-service.py install \
  --release-dir "$PWD/bin/piyush-YYYY.MM.DD.N"
```

The installer keeps release directories, a current symlink, a previous release,
and a dedicated `com.piyush.cliproxyapi` LaunchAgent. It uses the existing
Homebrew configuration file explicitly. Homebrew's proxy service is stopped,
but its binary is retained. Homebrew upgrades do not change the managed service.
The installer checks live health and account/routing preservation and restores
the previous service if activation fails.

```sh
python3 scripts/proxy-service.py status
python3 scripts/proxy-service.py rollback
```

Account isolation must continue to pass: selecting one account must never consume
another account on a failure. Review model names, thinking parameters, streaming,
and management API compatibility when changing request handling.

## Native account gateway

`/inference/:profile/v1/messages`, `/v1/messages/count_tokens` and `/v1/models`
under that prefix accept normal proxy client authentication. Canonical Claude model
IDs are mapped to an account's current prefix internally, with `WithPinnedAuthID`
locking the manager to that account even when registrations overlap. Missing,
disabled and unsupported choices fail closed. The routes are configured through
`GET/PUT /v0/management/account-gateway`, behind existing management authentication.
They persist at `<config-file>.gateway/routes.json`, independently of the console.
Set `CLIPROXY_ACCOUNT_GATEWAY_DIR` in the proxy process to relocate the private store.

`GET /v0/management/account-gateway/receipts` returns up to 5,000 recent requests.
The bounded journal retains routing/model/session/agent IDs, usage counts, completion
and error type, never message content or keys. Two journal files rotate at 8 MiB.
Context capability is configured, not an entitlement claim. Above-200K evidence is
set only for completed generation whose reported input including caches exceeds 200K.
The proxy owns SQLite request history and retention controls; see [native console management](docs/native-console.md).

The gateway enables filtered response headers per request, including retry and
provider quota headers on wrapped errors. New `anthropic-*` and `x-claude-code-*`
request capabilities pass through when the upstream credential executor has not
already constructed that header. Existing upstream credential/session handling is
unchanged. Claude handlers preserve request context so account pins and callbacks
survive both streaming and nonstreaming execution.

Regression coverage includes shared model registrations, disabled accounts, quota
failures without account fallback, SSE pings/fragmentation, metadata privacy and
restart persistence. Native account routes do not call the Management API per request.

## Frontend-only console

All administration logic now runs in `internal/console` and `internal/api/server_console.go`.
The React repository contains only the UI. See [native console](docs/native-console.md) for migration and deployment.
