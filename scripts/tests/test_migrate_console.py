import importlib.util
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('migration',Path(__file__).parents[1]/'migrate-console.py')
migration=importlib.util.module_from_spec(spec);spec.loader.exec_module(migration)

class MigrationTests(unittest.TestCase):
    def test_preserves_ids_history_and_source_and_separates_management_secret(self):
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);source=root/'old';source.mkdir();destination=root/'native';web=root/'web';web.mkdir();(web/'index.html').write_text('frontend');config=root/'proxy.yaml';config.write_text('port: 8317')
            with sqlite3.connect(source/'settings.sqlite') as db:
                db.execute('CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)')
                db.executemany('INSERT INTO settings VALUES(?,?)',[(k,json.dumps(v)) for k,v in {'displayName':'Gateway','managementKey':'private-management','clientApiKey':'private-client','cliProfile':'same-id','proxyUrl':'http://127.0.0.1:8317'}.items()])
            profiles=[{'id':'same-id','name':'Account','authFile':'claude-example.json'}]
            (source/'profiles.json').write_text(json.dumps({'profiles':profiles}))
            (source/'subscription-usage.json').write_text(json.dumps({'http://127.0.0.1:8317|claude-example.json':{'windows':[]}}))
            with sqlite3.connect(source/'request-history.sqlite') as db:
                db.execute('CREATE TABLE receipts(id TEXT PRIMARY KEY,started_at TEXT,profile TEXT,data TEXT)');db.execute('INSERT INTO receipts VALUES(?,?,?,?)',('receipt-id','2026-09-20T00:00:00Z','same-id','{"id":"receipt-id"}'))
            original=(source/'settings.sqlite').read_bytes()
            report=migration.migrate(source,config,destination,web,8320)
            self.assertEqual(report['profiles'],1);self.assertEqual(report['receipts'],1)
            self.assertEqual((source/'settings.sqlite').read_bytes(),original)
            with sqlite3.connect(destination/'console.sqlite') as db:
                settings=dict(db.execute('SELECT key,value FROM settings'))
                self.assertNotIn('managementKey',settings);self.assertNotIn('proxyUrl',settings)
                self.assertEqual(json.loads(settings['cliProfile']),'same-id')
                self.assertEqual(json.loads(db.execute("SELECT value FROM objects WHERE key='profiles'").fetchone()[0]),profiles)
                self.assertEqual(db.execute('SELECT id FROM receipts').fetchone()[0],'receipt-id')
            self.assertEqual((destination/'management.key').read_text(),'private-management')
            self.assertEqual((destination/'console.sqlite').stat().st_mode & 0o777,0o600)
            self.assertTrue((destination/'migration-backup/settings.sqlite').is_file())
            with self.assertRaisesRegex(ValueError,'must be new'):migration.migrate(source,config,destination,web)
