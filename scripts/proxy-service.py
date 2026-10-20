#!/usr/bin/env python3
"""Activate a tested release while preserving the existing config and logins."""
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
import time
import urllib.request

ROOT = Path.home() / '.cliproxy-console/proxy-service'
PLIST = Path.home() / 'Library/LaunchAgents/com.piyush.cliproxyapi.plist'
KEY_FILE = Path.home() / '.config/cliproxy/management-key'
ORIGIN = 'http://127.0.0.1:8317'
REPOSITORY = 'https://github.com/piyush-gambhir/CLIProxyAPI'
DOMAIN = f'gui/{os.getuid()}'


def run(*args, check=True):
    return subprocess.run(args, check=check, capture_output=True, text=True)


def api(route):
    request = urllib.request.Request(
        ORIGIN + '/v8/management/' + route,
        headers={'Authorization': 'Bearer ' + KEY_FILE.read_text().strip()},
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        return json.load(response), response.headers


def snapshot():
    config, headers = api('config')
    credentials, _ = api('credentials')
    plugins, _ = api('plugins')
    accounts = sorted(
        [(row['name'], row.get('type'), bool(row.get('disabled')),
          row.get('prefix', ''), row.get('websockets'))
         for row in credentials['files']], key=lambda row: row[0],
    )
    plugin_state = sorted((p['id'], p.get('enabled')) for p in plugins['plugins'])
    return {
        'config': config, 'accounts': accounts, 'plugins': plugin_state,
        'version': headers.get('X-CPA-Version'),
        'commit': headers.get('X-CPA-Commit'),
    }


def wait_healthy(manifest, before, config_file, config_hash):
    for _ in range(40):
        try:
            after = snapshot()
            if (after['version'], after['commit']) == (manifest['version'], manifest['commit']):
                break
        except (OSError, ValueError):
            pass
        time.sleep(0.5)
    else:
        raise RuntimeError('The new executable did not become healthy within 20 seconds')
    if after['accounts'] != before['accounts']:
        raise RuntimeError('Account names, providers, disabled states, prefixes, or transport settings changed')
    if after['config'] != before['config'] or hashlib.sha256(config_file.read_bytes()).hexdigest() != config_hash:
        raise RuntimeError('The existing configuration changed during activation')
    if after['plugins'] != before['plugins']:
        raise RuntimeError('The enabled plugin list changed during activation')
    quota, _ = api('quota-scheduler/status')
    if quota['strategy'] != before['config']['routing']['strategy']:
        raise RuntimeError('Quota scheduler strategy does not match the existing config')
    with urllib.request.urlopen(ORIGIN + '/management.html', timeout=5) as response:
        if response.status != 200:
            raise RuntimeError('The stock dashboard is unavailable')
    return after


def point_to(target):
    temporary = ROOT / f'current.next.{os.getpid()}'
    try:
        temporary.symlink_to(target, target_is_directory=True)
        temporary.replace(ROOT / 'current')
    finally:
        temporary.unlink(missing_ok=True)


def restart(target, label):
    stopped = run('launchctl', 'bootout', DOMAIN + '/' + label, check=False)
    if stopped.returncode and run('launchctl', 'print', DOMAIN + '/' + label, check=False).returncode == 0:
        raise RuntimeError('Could not unload the existing LaunchAgent: ' + stopped.stderr.strip())
    try:
        point_to(target)
    finally:
        # bootout can return before launchd finishes unloading the service.
        for _ in range(20):
            started = run('launchctl', 'bootstrap', DOMAIN, str(PLIST), check=False)
            if started.returncode == 0:
                break
            time.sleep(0.5)
        else:
            raise RuntimeError('Could not load the LaunchAgent: ' + started.stderr.strip())


def manifest_at(directory):
    return json.loads((directory / 'manifest.json').read_text())


def activate(target, label, before, config_file, config_hash):
    previous = (ROOT / 'current').resolve(strict=True)
    try:
        restart(target, label)
        return wait_healthy(manifest_at(target), before, config_file, config_hash), previous
    except Exception:
        restart(previous, label)
        wait_healthy(manifest_at(previous), before, config_file, config_hash)
        print('Activation failed; restored the previous executable. Existing credentials remain in place.')
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['status', 'install', 'rollback'])
    parser.add_argument('--release-dir', type=Path)
    args = parser.parse_args()
    if args.action == 'status':
        state = snapshot()
        print(json.dumps({'release': str((ROOT / 'current').resolve()),
                          'version': state['version'], 'commit': state['commit'],
                          'accounts': len(state['accounts']),
                          'strategy': state['config']['routing']['strategy'],
                          'sessionAffinity': state['config']['routing']['session-affinity']}, indent=2))
        return

    os.umask(0o077)
    with (ROOT / '.deploy.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        before = snapshot()
        agent = plistlib.loads(PLIST.read_bytes())
        arguments = agent['ProgramArguments']
        config_file = Path(arguments[arguments.index('-config') + 1])
        config_hash = hashlib.sha256(config_file.read_bytes()).hexdigest()
        if args.action == 'install':
            if args.release_dir is None:
                parser.error('install requires --release-dir')
            source = args.release_dir.resolve(strict=True)
            manifest = manifest_at(source)
            if manifest.get('repository') != REPOSITORY or not re.fullmatch(r'[a-f0-9]{40}', manifest['commit']):
                raise ValueError('Release must identify a committed build of the personal fork')
            if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,119}', manifest['version']):
                raise ValueError('Invalid release name')
            identity = run(str(source / 'cliproxyapi'), '--help', check=False)
            expected = f"Version: {manifest['version']}, Commit: {manifest['commit']},"
            if expected not in identity.stdout + identity.stderr:
                raise ValueError('Executable identity does not match its manifest')
            target = ROOT / 'releases' / manifest['version']
            if target.exists():
                raise ValueError('Release already exists; use a new immutable release name')
            shutil.copytree(source, target)
        else:
            deployment = json.loads((ROOT / 'deployment.json').read_text())
            target = Path(deployment['previous']).resolve(strict=True)
            if target.parent != (ROOT / 'releases').resolve():
                raise ValueError('Rollback target must be a managed release')

        backup = ROOT / 'backups' / time.strftime('pre-activate-%Y%m%dT%H%M%SZ', time.gmtime())
        backup.mkdir(mode=0o700, parents=True)
        shutil.copy2(config_file, backup / 'config.yaml')
        shutil.copy2(PLIST, backup / 'launchagent.plist')
        auth_dir = Path(before['config']['oauth']['auth-dir']).expanduser()
        if not auth_dir.is_absolute():
            raise ValueError('Resolve the relative OAuth directory before deployment')
        shutil.copytree(auth_dir, backup / 'auth')
        state, previous = activate(target, agent['Label'], before, config_file, config_hash)
        (ROOT / 'deployment.json').write_text(json.dumps({
            'current': str(target), 'previous': str(previous), 'backup': str(backup),
            'version': state['version'], 'commit': state['commit'],
        }, indent=2) + '\n')
        print(f"Activated {state['version']} ({state['commit']}); preserved {len(state['accounts'])} account entries.")
        print(f'Backup: {backup}')


if __name__ == '__main__':
    main()
