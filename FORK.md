# Piyush's CLIProxyAPI fork

Upstream: https://github.com/router-for-me/CLIProxyAPI
Fork: https://github.com/piyush-gambhir/CLIProxyAPI

`main` mirrors upstream without custom commits. `piyush` holds our changes and is
the deployment branch. Keep proxy changes small; the separate private
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
commit, then publish the branch and tag. Use a new release name for each build.

## Build and deploy locally

The build includes the fork commit, release name, and build time. Configuration
and credentials live outside this repository. The example below assumes the
console and fork checkouts are siblings.

```sh
./scripts/build-piyush.sh piyush-YYYY.MM.DD.N
CLIPROXY_BIN="$PWD/bin/piyush-YYYY.MM.DD.N/cliproxyapi" \
  node ../cliproxy-console/scripts/verify-routing.mjs
python3 ../cliproxy-console/scripts/proxy-service.py install \
  --release-dir "$PWD/bin/piyush-YYYY.MM.DD.N"
```

The installer keeps release directories, a current symlink, a previous release,
and a dedicated `com.piyush.cliproxyapi` LaunchAgent. It uses the existing
Homebrew configuration file explicitly. Homebrew's proxy service is stopped,
but its binary is retained. Homebrew upgrades do not change the managed service.
The installer checks live health and account/routing preservation and restores
the previous service if activation fails.

```sh
python3 ../cliproxy-console/scripts/proxy-service.py status
python3 ../cliproxy-console/scripts/proxy-service.py rollback
```

Account isolation must continue to pass: selecting one account must never consume
another account on a failure. Review model names, thinking parameters, streaming,
and management API compatibility when changing request handling.
