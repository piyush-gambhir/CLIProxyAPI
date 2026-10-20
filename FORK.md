# Piyush's CLIProxyAPI fork

Fork: https://github.com/piyush-gambhir/CLIProxyAPI
Upstream: https://github.com/router-for-me/CLIProxyAPI

`main` is our development and deployment branch. It contains current upstream
code and the five Theo-inspired commits for quota polling, reset-soonest routing,
quota status, and Codex WebSocket defaults. The stock management dashboard is used.
`piyush` preserves the retired console/gateway and its unfinished work;
`theo-v8` preserves the original October 3 implementation. Neither is a deployment
branch. The Kiro plugin is maintained in its separate repository.

## Update from upstream

Check weekly for new stable upstream releases. Update sooner for relevant fixes.
Start with a clean checkout and use a temporary branch for the integration:

```sh
git fetch upstream main --tags
git switch main
git pull --ff-only origin main
git switch -c update/upstream-YYYYMMDD
git merge upstream/main
```

Review conflicts, especially config layout, credential persistence, session
affinity, quota handling, and management routes. Preserve explicit account pins
and disabled accounts. Run affected tests first, followed by the full suite and
build required before deployment:

```sh
go test ./sdk/cliproxy/quota ./internal/auth/codex ./sdk/cliproxy/auth ./sdk/cliproxy ./internal/api/handlers/management ./internal/api -timeout 180s
go test ./... -timeout 180s
go build -o bin/verify-build ./cmd/server
git switch main
git merge --ff-only update/upstream-YYYYMMDD
gh auth switch --user piyush-gambhir
git push origin main
```

New features are evaluated individually. Upstream's `dev` branch and unmerged
feature PRs are useful to follow; deployments use reviewed code on our `main`.
Inherited publishing and PR policy workflows run only in the upstream repo.
Our own CI runs tests and a build for `main` and pull requests targeting it.

## Build and activate

Pushing or pulling code does not update the running service. Build from a clean,
committed `main`, use a unique release name, and publish the matching tag:

```sh
./scripts/build-piyush.sh piyush-YYYY.MM.DD.N
git tag piyush-YYYY.MM.DD.N
git push origin piyush-YYYY.MM.DD.N
python3 scripts/proxy-service.py install --release-dir bin/piyush-YYYY.MM.DD.N
python3 scripts/proxy-service.py status
```

The installer uses the existing `com.piyush.cliproxyapi` LaunchAgent, config, and
OAuth directory. It backs up config, credentials, and the LaunchAgent, switches
the release symlink, and checks the reported commit, account settings, routing,
plugin loading, and dashboard. It restores the previous executable if checks
fail. Credentials remain in place; installation does not log in, log out,
replace tokens, or restore old tokens over freshly refreshed credentials.

The process restarts briefly during activation. A failed release is kept for
inspection, and backups are private under the managed service directory.

```sh
python3 scripts/proxy-service.py rollback
```

The runtime uses immutable releases under
`~/.cliproxy-console/proxy-service/releases/`. Its `current` symlink selects the
installed release. Automatic code checks should report useful updates; activating
a new executable is an explicit maintenance step.
