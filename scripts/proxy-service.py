#!/usr/bin/env python3
"""Install immutable proxy releases with live checks and a rollback target."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
import time
import urllib.request
from urllib.parse import urlparse

# Proxy-owned deployment metadata; the frontend has no service configuration.
config_location = Path(os.environ.get('CLIPROXY_CONFIG', '/opt/homebrew/etc/cliproxyapi.conf')).expanduser().resolve()
bootstrap_file = Path(str(config_location) + '.console.json')
bootstrap = json.loads(bootstrap_file.read_text()) if bootstrap_file.is_file() else {}
DATA_DIR = Path(os.environ.get('CLIPROXY_CONSOLE_DATA_DIR') or bootstrap.get('dataDir') or str(config_location)+'.console')
SETTINGS_FILE = DATA_DIR / 'service-settings.json'
OPTIONS = json.loads(SETTINGS_FILE.read_text()) if SETTINGS_FILE.is_file() else {}
ROOT = Path(os.environ.get('CLIPROXY_SERVICE_DIR') or OPTIONS.get('serviceDir') or str(DATA_DIR / 'proxy-service')).expanduser().resolve()
LEGACY_CONSOLE_URL = os.environ.get('CLIPROXY_LEGACY_CONSOLE_URL', '').rstrip('/')
LEGACY_CONSOLE_LABEL = os.environ.get('CLIPROXY_LEGACY_CONSOLE_LABEL', '')
startup_settings = {}


def option(key, environment, default):
    value = os.environ.get(environment) or OPTIONS.get(key, default)
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f'{key} must be a nonempty string')
    return value


LABEL = option('label', 'CLIPROXY_SERVICE_LABEL', 'local.cliproxyapi')
BREW_LABEL = option('brewLabel', 'CLIPROXY_BREW_LABEL', 'homebrew.mxcl.cliproxyapi')
BREW = option('brewBinary', 'CLIPROXY_BREW', shutil.which('brew') or '/opt/homebrew/bin/brew')
BREW_PREFIX = Path(option('brewPrefix', 'CLIPROXY_BREW_PREFIX', str(Path(BREW).parent.parent)))
PLIST = Path.home() / 'Library' / 'LaunchAgents' / (LABEL + '.plist')
CONFIG = Path(option('config', 'CLIPROXY_CONFIG', str(BREW_PREFIX / 'etc/cliproxyapi.conf'))).expanduser().resolve()
REPOSITORY = option('repository', 'CLIPROXY_RELEASE_REPOSITORY', 'https://github.com/router-for-me/CLIProxyAPI')
CONSOLE_URL = option('consoleUrl', 'CLIPROXY_CONSOLE_URL', 'http://127.0.0.1:8317').rstrip('/')
DOMAIN = f'gui/{os.getuid()}'

if not all(re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]+', label) for label in [LABEL, BREW_LABEL]):
    raise ValueError('Service labels must contain only letters, digits, dots, underscores, or hyphens')
if urlparse(CONSOLE_URL).scheme not in ['http', 'https'] or urlparse(CONSOLE_URL).hostname not in ['127.0.0.1', 'localhost', '::1']:
    raise ValueError('The console URL must point to localhost')


def run(*args, check=True):
    return subprocess.run(args, check=check, capture_output=True, text=True)


def api(route):
    key = os.environ.get('CLIPROXY_MGMT_KEY') or (DATA_DIR / 'management.key').read_text().strip()
    destination = '/v0/management/' + (route[5:] if route.startswith('mgmt/') else 'console/' + route)
    request = urllib.request.Request(CONSOLE_URL + destination, headers={'Authorization': 'Bearer ' + key})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        # Only the one-time migration may read the old local console before cutover.
        if error.code == 404 and LEGACY_CONSOLE_URL:
            with urllib.request.urlopen(LEGACY_CONSOLE_URL + '/api/' + route, timeout=5) as response:
                return json.load(response)
        raise


def snapshot():
    return {'profiles': sorted(p['id'] for p in api('profiles')['profiles']),
            'accounts': sorted(f['name'] for f in api('mgmt/auth-files')['files']),
            'routing': api('routing'), 'desktop': api('desktop')['profile'],
            'configHash': hashlib.sha256(CONFIG.read_bytes()).hexdigest()}


def binary_identity(binary):
    result = run(str(binary), '--help', check=False)
    match = re.search(r'CLIProxyAPI Version: ([^,]+), Commit: ([^,]+), BuiltAt:', result.stdout + result.stderr)
    if not match:
        raise ValueError('Binary does not report a CLIProxyAPI build identity')
    return match.group(1), match.group(2)


def validate_release(directory):
    manifest = json.loads((directory / 'manifest.json').read_text())
    version, commit = manifest.get('version', ''), manifest.get('commit', '')
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]{0,119}', version):
        raise ValueError('Invalid release name')
    if not re.fullmatch(r'[a-f0-9]{40}', commit):
        raise ValueError('Release must reference a full Git commit')
    if manifest.get('repository') != REPOSITORY:
        raise ValueError('Release must come from the configured fork')
    if binary_identity(directory / 'cliproxyapi') != (version, commit):
        raise ValueError('Binary identity does not match its release manifest')
    return manifest


def atomic_json(file, value):
    temp = file.with_name(file.name + '.tmp')
    temp.write_text(json.dumps(value, indent=2) + '\n'); temp.chmod(0o600)
    temp.replace(file)


def point_to(directory):
    temp = ROOT / 'current.next'
    if temp.is_symlink():
        temp.unlink()
    temp.symlink_to(directory, target_is_directory=True)
    temp.replace(ROOT / 'current')


def wait_healthy(version):
    for _ in range(40):
        try:
            # Older consoles cache build identity from management response headers.
            api('mgmt/routing/strategy')
            health = api('health')
            if health.get('ok') and health.get('proxyVersion') == version:
                return
        except Exception:
            pass
        time.sleep(0.25)
    raise RuntimeError(f'Proxy did not become healthy as {version}')


def active(label):
    return run('launchctl', 'print', f'{DOMAIN}/{label}', check=False).returncode == 0


def bootstrap_agent():
    # launchd can briefly retain the unloaded job after bootout returns.
    for attempt in range(20):
        try:
            run('launchctl', 'bootstrap', DOMAIN, str(PLIST))
            return
        except subprocess.CalledProcessError as error:
            if error.returncode != 5 or attempt == 19:
                raise
            time.sleep(0.25)


def activate(target, manifest, before):
    legacy_was_active = bool(LEGACY_CONSOLE_LABEL) and active(LEGACY_CONSOLE_LABEL)
    previous = (ROOT / 'current').resolve() if (ROOT / 'current').is_symlink() else None
    previous_version = binary_identity(previous / 'cliproxyapi')[0] if previous else api('health')['proxyVersion']
    was_managed, was_brew = active(LABEL), active(BREW_LABEL)
    if was_managed and was_brew:
        raise RuntimeError('Both proxy services are loaded; resolve the duplicate before deploying')
    if not was_managed and not was_brew:
        raise RuntimeError('No known proxy service is loaded; refusing to guess')
    backup = ROOT / 'backups' / (time.strftime('%Y%m%d-%H%M%S') + '-' + str(time.time_ns()))
    backup.mkdir(parents=True, mode=0o700)
    shutil.copy2(CONFIG, backup / 'cliproxyapi.conf'); (backup / 'cliproxyapi.conf').chmod(0o600)
    if PLIST.exists():
        shutil.copy2(PLIST, backup / PLIST.name)
    brew_plist = Path.home() / 'Library/LaunchAgents' / (BREW_LABEL + '.plist')
    if brew_plist.exists():
        shutil.copy2(brew_plist, backup / brew_plist.name)
    atomic_json(backup / 'before.json', before)
    config = {'Label':LABEL, 'ProgramArguments':[str(ROOT / 'current/cliproxyapi'), '-config',str(CONFIG)],
              'WorkingDirectory':str(ROOT), 'RunAtLoad':True, 'KeepAlive':True,
              'StandardOutPath':str(ROOT / 'service.log'), 'StandardErrorPath':str(ROOT / 'service.log')}
    (ROOT / 'service.log').touch(mode=0o600, exist_ok=True)
    state = {'current':str(target), 'version':manifest['version'], 'commit':manifest['commit'],
             'previous':str(previous) if previous else None, 'backup':str(backup),
             'checkedAt':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())}
    try:
        if legacy_was_active:
            run('launchctl','bootout',f'{DOMAIN}/{LEGACY_CONSOLE_LABEL}')
        if was_brew:
            run(BREW, 'services', 'stop', 'cliproxyapi')
        else:
            run('launchctl', 'bootout', f'{DOMAIN}/{LABEL}')
        point_to(target)
        PLIST.write_bytes(plistlib.dumps(config)); PLIST.chmod(0o600)
        bootstrap_agent()
        wait_healthy(manifest['version'])
        if snapshot() != before:
            raise RuntimeError('Account, routing, or Desktop configuration changed during activation')
        atomic_json(ROOT / 'state.json', state)
    except Exception:
        run('launchctl','bootout',f'{DOMAIN}/{LABEL}',check=False)
        if previous:
            point_to(previous)
        if was_brew:
            if not active(BREW_LABEL):
                run(BREW,'services','start','cliproxyapi')
        else:
            shutil.copy2(backup / PLIST.name, PLIST)
            bootstrap_agent()
        if legacy_was_active:
            legacy_plist = Path.home() / 'Library/LaunchAgents' / (LEGACY_CONSOLE_LABEL + '.plist')
            run('launchctl','bootstrap',DOMAIN,str(legacy_plist),check=False)
        wait_healthy(previous_version)
        raise
    if legacy_was_active:
        legacy_plist = Path.home() / 'Library/LaunchAgents' / (LEGACY_CONSOLE_LABEL + '.plist')
        if legacy_plist.exists():
            legacy_plist.replace(backup / legacy_plist.name)
    print(json.dumps(state, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['install','rollback','status'])
    parser.add_argument('--release-dir',type=Path)
    args = parser.parse_args()
    if sys.platform != 'darwin':
        raise RuntimeError('This optional service installer requires macOS and launchd')
    if args.action == 'status':
        state = json.loads((ROOT / 'state.json').read_text()) if (ROOT / 'state.json').exists() else {'installed':False}
        state.update({'managedService':active(LABEL),'homebrewService':active(BREW_LABEL),'health':api('health')})
        print(json.dumps(state,indent=2)); return
    ROOT.mkdir(parents=True,mode=0o700,exist_ok=True); ROOT.chmod(0o700)
    with (ROOT / 'deploy.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        deploy(args)


def deploy(args):
    (ROOT / 'releases').mkdir(mode=0o700,exist_ok=True)
    before = snapshot()
    if args.action == 'rollback':
        state = json.loads((ROOT / 'state.json').read_text())
        if not state.get('previous'):
            raise ValueError('No previous release is available')
        target = Path(state['previous'])
        if target.parent != ROOT / 'releases':
            raise ValueError('Rollback target is outside the managed releases directory')
        manifest = json.loads((target / 'manifest.json').read_text())
        if binary_identity(target / 'cliproxyapi') != (manifest['version'],manifest['commit']):
            raise ValueError('Rollback binary identity has changed')
    else:
        if args.release_dir is None:
            raise ValueError('--release-dir is required for install')
        source = args.release_dir.resolve(); manifest = validate_release(source)
        target = ROOT / 'releases' / manifest['version']
        if target.exists():
            # A failed activation may have staged this exact immutable release.
            same_manifest = json.loads((target / 'manifest.json').read_text()) == manifest
            same_binary = hashlib.sha256((source / 'cliproxyapi').read_bytes()).digest() == hashlib.sha256((target / 'cliproxyapi').read_bytes()).digest()
            if not same_manifest or not same_binary:
                raise ValueError('Release already exists with different contents; use a new immutable release name')
        else:
            target.mkdir(mode=0o700)
            shutil.copy2(source / 'cliproxyapi',target / 'cliproxyapi'); (target / 'cliproxyapi').chmod(0o755)
            shutil.copy2(source / 'manifest.json',target / 'manifest.json')
        # Seed the first rollback release from the exact currently installed binary.
        if not (ROOT / 'current').exists():
            original = (BREW_PREFIX / 'opt/cliproxyapi/bin/cliproxyapi').resolve()
            version,commit = binary_identity(original)
            fallback = ROOT / 'releases' / ('upstream-' + re.sub(r'[^a-zA-Z0-9._-]','_',version))
            fallback.mkdir(mode=0o700,exist_ok=True)
            shutil.copy2(original,fallback / 'cliproxyapi')
            atomic_json(fallback / 'manifest.json',{'version':version,'commit':commit,'repository':'https://github.com/router-for-me/CLIProxyAPI'})
            point_to(fallback)
    activate(target,manifest,before)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Proxy service: {error}',file=sys.stderr)
        sys.exit(1)
