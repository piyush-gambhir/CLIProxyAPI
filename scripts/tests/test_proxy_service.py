import copy
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('proxy_service', Path(__file__).parents[1] / 'proxy-service.py')
service = importlib.util.module_from_spec(spec)
spec.loader.exec_module(service)


class ActivationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.config_file = Path(self.temp.name) / 'config.yaml'
        self.config_file.write_text('existing config')
        self.config_hash = hashlib.sha256(self.config_file.read_bytes()).hexdigest()
        self.before = {
            'version': 'old', 'commit': 'old-commit',
            'accounts': [('claude.json', 'claude', False, '', None),
                         ('disabled.json', 'codex', True, '', True)],
            'plugins': [('kiro', True)],
            'config': {'routing': {'strategy': 'reset-soonest'}},
        }
        self.after = dict(self.before, version='new', commit='new-commit')
        self.manifest = {'version': 'new', 'commit': 'new-commit'}

    def check_health(self, after):
        with patch.object(service, 'snapshot', return_value=after), \
             patch.object(service, 'api', return_value=({'strategy': 'reset-soonest'}, {})), \
             patch.object(service.urllib.request, 'urlopen') as open_url:
            open_url.return_value.__enter__.return_value.status = 200
            return service.wait_healthy(self.manifest, self.before, self.config_file, self.config_hash)

    def test_accepts_same_accounts_including_disabled_account(self):
        self.assertEqual(self.check_health(self.after), self.after)

    def test_rejects_missing_account(self):
        after = dict(self.after, accounts=self.after['accounts'][:1])
        with self.assertRaisesRegex(RuntimeError, 'Account'):
            self.check_health(after)

    def test_rejects_changed_disabled_state(self):
        after = copy.deepcopy(self.after)
        after['accounts'][1] = ('disabled.json', 'codex', False, '', True)
        with self.assertRaisesRegex(RuntimeError, 'Account'):
            self.check_health(after)

    def test_rejects_missing_plugin(self):
        with self.assertRaisesRegex(RuntimeError, 'plugin'):
            self.check_health(dict(self.after, plugins=[]))

    def test_rejects_config_written_during_startup(self):
        self.config_file.write_text('changed config')
        with self.assertRaisesRegex(RuntimeError, 'configuration'):
            self.check_health(self.after)

    def test_failed_health_restores_previous_binary_without_replacing_credentials(self):
        previous = Path(self.temp.name) / 'previous'
        previous.mkdir()
        (Path(self.temp.name) / 'current').symlink_to(previous)
        target = Path(self.temp.name) / 'new'
        with patch.object(service, 'ROOT', Path(self.temp.name)), \
             patch.object(service, 'restart') as restart, \
             patch.object(service, 'manifest_at', return_value=self.manifest), \
             patch.object(service, 'wait_healthy', side_effect=[RuntimeError('missing account'), self.before]), \
             patch.object(service.shutil, 'copy2') as replace_file:
            with self.assertRaisesRegex(RuntimeError, 'missing account'):
                service.activate(target, 'label', self.before, self.config_file, self.config_hash)
            self.assertEqual(restart.call_args_list[0].args, (target, 'label'))
            self.assertEqual(restart.call_args_list[1].args, (previous.resolve(), 'label'))
            replace_file.assert_not_called()


if __name__ == '__main__':
    unittest.main()
