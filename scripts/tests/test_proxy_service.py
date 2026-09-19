"""Exercise deployment failures without touching real services or credentials."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import tempfile
import subprocess
import sys
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('proxy_service', Path(__file__).parents[1] / 'proxy-service.py')
service = importlib.util.module_from_spec(spec)
spec.loader.exec_module(service)


class DeploymentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.config = self.root / 'proxy.conf'
        self.config.write_text('test configuration')
        self.plist = self.root / 'service.plist'
        self.old_plist = plistlib.dumps({'Label': service.LABEL, 'old': True})
        self.plist.write_bytes(self.old_plist)
        self.previous = self.root / 'releases' / 'previous'
        self.target = self.root / 'releases' / 'next'
        self.previous.mkdir(parents=True)
        self.target.mkdir()
        (self.root / 'current').symlink_to(self.previous)
        self.manifest = {'version': 'console-next', 'commit': 'a' * 40,
                         'repository': service.REPOSITORY}
        (self.target / 'manifest.json').write_text(json.dumps(self.manifest))
        self.before = {'profiles': ['one'], 'accounts': ['one.json'], 'routing': {'fallback': False}}
        for name, value in [('ROOT', self.root), ('PLIST', self.plist), ('CONFIG', self.config)]:
            p = patch.object(service, name, value); p.start(); self.addCleanup(p.stop)
        self.run = self.mock('run')
        self.identity = self.mock('binary_identity', return_value=('previous', 'old-commit'))
        self.active = self.mock('active', side_effect=lambda label: label == service.BREW_LABEL)
        self.health = self.mock('wait_healthy')
        self.snapshot = self.mock('snapshot', return_value=self.before)
        self.stdout = contextlib.redirect_stdout(io.StringIO()); self.stdout.__enter__()
        self.addCleanup(self.stdout.__exit__, None, None, None)
        platform = patch.object(service.sys, 'platform', 'darwin')
        platform.start(); self.addCleanup(platform.stop)

    def mock(self, name, **kwargs):
        p = patch.object(service, name, **kwargs)
        self.addCleanup(p.stop)
        return p.start()

    def assert_restored(self):
        self.assertEqual((self.root / 'current').resolve(), self.previous)
        self.assertFalse((self.root / 'state.json').exists())
        self.assertEqual(self.config.read_text(), 'test configuration')
        self.health.assert_called_with('previous')

    def test_promotes_release_and_records_previous_after_checks(self):
        service.activate(self.target, self.manifest, self.before)
        self.assertEqual((self.root / 'current').resolve(), self.target)
        state = json.loads((self.root / 'state.json').read_text())
        self.assertEqual(state['previous'], str(self.previous))
        self.assertEqual(state['commit'], self.manifest['commit'])
        self.assertEqual((Path(state['backup']) / 'cliproxyapi.conf').read_text(), 'test configuration')
        self.run.assert_any_call(service.BREW, 'services', 'stop', 'cliproxyapi')
        self.run.assert_any_call('launchctl', 'bootstrap', service.DOMAIN, str(self.plist))
        self.health.assert_called_once_with('console-next')

    def test_unhealthy_release_restores_homebrew(self):
        self.active.side_effect = [False, True, False]
        self.health.side_effect = [RuntimeError('unhealthy'), None]
        with self.assertRaisesRegex(RuntimeError, 'unhealthy'):
            service.activate(self.target, self.manifest, self.before)
        self.assert_restored()
        self.run.assert_any_call(service.BREW, 'services', 'start', 'cliproxyapi')

    def test_failed_managed_upgrade_restores_previous_agent(self):
        self.active.side_effect = lambda label: label == service.LABEL
        self.health.side_effect = [RuntimeError('unhealthy'), None]
        with self.assertRaisesRegex(RuntimeError, 'unhealthy'):
            service.activate(self.target, self.manifest, self.before)
        self.assert_restored()
        self.assertEqual(self.plist.read_bytes(), self.old_plist)

    def test_changed_account_state_rejects_release(self):
        self.snapshot.return_value = {**self.before, 'accounts': []}
        with self.assertRaisesRegex(RuntimeError, 'configuration changed'):
            service.activate(self.target, self.manifest, self.before)
        self.assert_restored()

    def test_stop_failure_still_recovers_the_previous_service(self):
        self.active.side_effect = [False, True, False]
        self.run.side_effect = [RuntimeError('stop failed'), None, None]
        with self.assertRaisesRegex(RuntimeError, 'stop failed'):
            service.activate(self.target, self.manifest, self.before)
        self.assert_restored()
        self.run.assert_any_call(service.BREW, 'services', 'start', 'cliproxyapi')

    def test_state_write_failure_rolls_back(self):
        original = service.atomic_json
        def fail_state(path, value):
            if path.name == 'state.json':
                raise OSError('disk full')
            original(path, value)
        with patch.object(service, 'atomic_json', side_effect=fail_state):
            with self.assertRaisesRegex(OSError, 'disk full'):
                service.activate(self.target, self.manifest, self.before)
        self.assert_restored()

    def test_duplicate_services_are_rejected_without_stopping_either(self):
        self.active.side_effect = None; self.active.return_value = True
        with self.assertRaisesRegex(RuntimeError, 'Both proxy services'):
            service.activate(self.target, self.manifest, self.before)
        self.run.assert_not_called()

    def test_install_and_rollback_accept_an_existing_service_directory(self):
        for action in ['install', 'rollback']:
            with self.subTest(action=action), patch.object(service, 'deploy') as deploy:
                with patch.object(service.sys, 'argv', ['proxy-service.py', action]):
                    service.main()
                self.assertEqual(deploy.call_args.args[0].action, action)

    def test_bootstrap_retries_launchd_busy_only(self):
        self.run.side_effect = [subprocess.CalledProcessError(5, 'launchctl'), None]
        with patch.object(service.time, 'sleep') as sleep:
            service.bootstrap_agent()
            sleep.assert_called_once_with(0.25)
        self.assertEqual(self.run.call_count, 2)
        self.run.reset_mock()
        self.run.side_effect = subprocess.CalledProcessError(1, 'launchctl')
        with patch.object(service.time, 'sleep') as sleep:
            with self.assertRaises(subprocess.CalledProcessError):
                service.bootstrap_agent()
            sleep.assert_not_called()

    def test_release_manifest_must_match_the_actual_binary(self):
        self.identity.return_value = (self.manifest['version'], self.manifest['commit'])
        self.assertEqual(service.validate_release(self.target), self.manifest)
        self.identity.return_value = ('another-build', self.manifest['commit'])
        with self.assertRaisesRegex(ValueError, 'identity does not match'):
            service.validate_release(self.target)
        for field, value in [('version', '../escape'), ('commit', 'short'), ('repository', 'another-repo')]:
            with self.subTest(field=field):
                (self.target / 'manifest.json').write_text(json.dumps({**self.manifest, field: value}))
                with self.assertRaises(ValueError):
                    service.validate_release(self.target)


class LocalConfigurationTests(unittest.TestCase):
    def test_health_check_refreshes_build_identity(self):
        with patch.object(service, 'api', side_effect=[{}, {'ok': True, 'proxyVersion': 'new'}]) as api:
            service.wait_healthy('new')
            self.assertEqual([call.args[0] for call in api.call_args_list], ['mgmt/routing/strategy', 'health'])

    def test_installer_and_launcher_use_custom_local_options(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            settings = {'label': 'example.proxy', 'brewLabel': 'example.brew.proxy',
                        'brewBinary': '/example/bin/brew', 'config': str(root / 'config with spaces.conf'),
                        'repository': 'https://github.com/example/proxy', 'consoleUrl': 'http://localhost:8320'}
            (root / 'service-settings.json').write_text(json.dumps(settings))
            env = {k: v for k, v in os.environ.items() if not k.startswith('CLIPROXY_')}
            env['CLIPROXY_SERVICE_DIR'] = temp
            env['CLIPROXY_CONSOLE_DATA_DIR'] = temp
            script = str(Path(__file__).parents[1] / 'proxy-service.py')
            code = 'import runpy,json,sys; m=runpy.run_path(sys.argv[1]); print(json.dumps([m["LABEL"],str(m["CONFIG"]),m["REPOSITORY"],m["CONSOLE_URL"]]))'
            values = json.loads(subprocess.check_output([sys.executable, '-B', '-c', code, script], env=env))
            self.assertEqual(values, [settings['label'], settings['config'], settings['repository'], settings['consoleUrl']])
            binary = root / 'current' / 'cliproxyapi'
            binary.parent.mkdir()
            binary.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\n'); binary.chmod(0o700)
            launcher = str(Path(__file__).parents[1] / 'cliproxyapi')
            args = subprocess.check_output([sys.executable, '-B', launcher, '--help'], env=env, text=True).splitlines()
            self.assertEqual(args, ['-config', settings['config'], '--help'])
            env['CLIPROXY_CONFIG'] = str(root / 'override.conf')
            args = subprocess.check_output([sys.executable, '-B', launcher, '--help'], env=env, text=True).splitlines()
            self.assertEqual(args[1], env['CLIPROXY_CONFIG'])


if __name__ == '__main__':
    unittest.main()
