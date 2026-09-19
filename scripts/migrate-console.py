#!/usr/bin/env python3
"""One-time, offline import from the retired Node console into CLIProxyAPI.

The original source stays untouched. Settings and history use SQLite's backup API;
secrets and all migration artifacts are owner-only. No network requests are made.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import sqlite3
import shutil

SETTINGS = {'displayName','clientApiKey','claudeModels','cliEffort','cliProfile','claudeBackgroundModel','claudeSubagentModel','receiptRetentionDays','claudeConfigDir','desktopConfigDir'}

def read(path, default):
    return json.loads(path.read_text()) if path.exists() else default

def private_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary=path.with_name(path.name+'.tmp')
    with open(temporary, 'w', opener=lambda p,f:os.open(p,f,0o600)) as out:
        json.dump(value,out,indent=2)
        out.write('\n')
    temporary.chmod(0o600)
    temporary.replace(path)

def backup_sqlite(source, target):
    with sqlite3.connect(f'file:{source}?mode=ro',uri=True) as src, sqlite3.connect(target) as dst:
        src.backup(dst)
    target.chmod(0o600)

def migrate(source,config,destination,web,port=0,settings_db=None):
    if destination.exists():
        raise ValueError('Destination must be new; refusing to overwrite live proxy data')
    if not config.is_file() or not (web/'index.html').is_file():
        raise ValueError('Proxy config and built frontend index.html are required')
    source_settings=settings_db or source/'settings.sqlite'
    destination.mkdir(parents=True,mode=0o700)
    backups=destination/'migration-backup'
    backups.mkdir(mode=0o700)
    if source_settings.is_file():
        backup_sqlite(source_settings,backups/'settings.sqlite')
    else:
        raise ValueError('Original settings.sqlite is missing')
    for name in ['profiles.json','subscription-usage.json','recent-paths.json','startup.json']:
        if (source/name).exists():
            shutil.copyfile(source/name,backups/name)
            (backups/name).chmod(0o600)
    history=source/'request-history.sqlite'
    if history.is_file():
        backup_sqlite(history,backups/'request-history.sqlite')
    bootstrap=Path(str(config)+'.console.json')
    if bootstrap.exists():
        shutil.copyfile(bootstrap,backups/'previous-console-bootstrap.json')
        (backups/'previous-console-bootstrap.json').chmod(0o600)
    with sqlite3.connect(backups/'settings.sqlite') as old:
        settings={key:json.loads(value) for key,value in old.execute('SELECT key,value FROM settings')}
    profiles=read(source/'profiles.json',{}).get('profiles',[])
    dbfile=destination/'console.sqlite'
    fd=os.open(dbfile,os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o600);os.close(fd)
    with sqlite3.connect(dbfile) as db:
        db.executescript('CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);CREATE TABLE objects(key TEXT PRIMARY KEY,value TEXT NOT NULL);CREATE TABLE receipts(id TEXT PRIMARY KEY,started_at TEXT NOT NULL,profile TEXT NOT NULL,data TEXT NOT NULL);CREATE INDEX receipts_time ON receipts(started_at DESC);')
        db.executemany('INSERT INTO settings VALUES(?,?)',[(k,json.dumps(v)) for k,v in settings.items() if k in SETTINGS])
        db.execute('INSERT INTO objects VALUES(?,?)',('profiles',json.dumps(profiles)))
        db.execute('INSERT INTO objects VALUES(?,?)',('recents',json.dumps(read(source/'recent-paths.json',{'paths':[]}))))
        for key,value in read(source/'subscription-usage.json',{}).items():
            db.execute('INSERT OR REPLACE INTO objects VALUES(?,?)',('usage:'+key.rsplit('|',1)[-1],json.dumps(value)))
        if history.is_file():
            with sqlite3.connect(backups/'request-history.sqlite') as old:
                db.executemany('INSERT OR IGNORE INTO receipts VALUES(?,?,?,?)',old.execute('SELECT id,started_at,profile,data FROM receipts'))
        count=db.execute('SELECT count(*) FROM receipts').fetchone()[0]
    # Existing key is for local operator/service authentication only. The UI never
    # reads this file and application settings do not contain a management secret.
    key=settings.get('managementKey','')
    if key:
        secret=destination/'management.key'
        with open(secret,'w',opener=lambda p,f:os.open(p,f,0o600)) as out:out.write(key)
    service_root=source/'proxy-service'
    service=read(service_root/'service-settings.json',{})
    service['serviceDir']=str(service_root)
    service['config']=str(config)
    old_url=settings.get('proxyUrl','http://127.0.0.1:8317')
    service['consoleUrl']=old_url
    private_json(destination/'service-settings.json',service)
    report={'source':str(source),'destination':str(destination),'profiles':len(profiles),'receipts':count,'migratedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sourcePreserved':True}
    private_json(destination/'migration.json',report)
    private_json(bootstrap,{'dataDir':str(destination),'webDir':str(web),'compatibilityPort':port})
    return report

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source',required=True,type=Path)
    parser.add_argument('--config',required=True,type=Path)
    parser.add_argument('--data-dir',required=True,type=Path)
    parser.add_argument('--web-dir',required=True,type=Path)
    parser.add_argument('--settings-db',type=Path)
    parser.add_argument('--compatibility-port',type=int,default=0)
    args=parser.parse_args()
    if not 0<=args.compatibility_port<=65535:parser.error('Invalid compatibility port')
    # Restrict permissions on SQLite journals and temporary files as well.
    os.umask(0o077)
    print(json.dumps(migrate(args.source.expanduser().resolve(),args.config.expanduser().resolve(),args.data_dir.expanduser().resolve(),args.web_dir.expanduser().resolve(),args.compatibility_port,args.settings_db),indent=2))
