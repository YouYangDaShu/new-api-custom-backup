#!/usr/bin/env python3
"""Pull all daily archives, verify them, then permit guarded 15-day cleanup."""
import datetime as dt
import fcntl
import gzip
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys

HOST = 'root@39.109.63.146'
SSH_ARGS = ['-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','ConnectTimeout=40',
            '-o','ServerAliveInterval=8','-o','ServerAliveCountMax=3']
REMOTE_WORKER = '/opt/new-api-backups/logs-backup-worker.py'
LOCAL = Path.home() / '桌面/Hermes备份工具/备份文件/new-api-daily-logs'
LOG = Path.home() / '桌面/Hermes备份工具/new-api-logs-pull.log'


def log(text):
    line=dt.datetime.now().isoformat(timespec='seconds')+' '+text
    print(line,flush=True)
    with LOG.open('a') as f:
        f.write(line+'\n')


def remote(action, payload=None):
    cmd=['ssh']+SSH_ARGS+[HOST,'python3',REMOTE_WORKER,action]
    for attempt in range(3):
        result=subprocess.run(cmd,input=json.dumps(payload) if payload is not None else None,
                              text=True,capture_output=True,timeout=2400)
        if result.returncode==0:
            if result.stderr.strip(): log(result.stderr.strip())
            return json.loads(result.stdout)
        log(f'Remote {action} attempt {attempt+1} failed: '+result.stderr[-700:])
        if action != 'prepare':
            break
    raise RuntimeError('Remote '+action+' failed; do not clean database')


def verify(files):
    verified={}
    for item in files:
        name=item['name']
        if Path(name).name!=name or not name.endswith('.sql.gz'):
            raise RuntimeError('Unsafe archive filename')
        path=LOCAL/name
        if path.stat().st_size != item['bytes']:
            raise RuntimeError('Local size mismatch: '+name)
        h=hashlib.sha256()
        with path.open('rb') as f:
            for block in iter(lambda:f.read(1024*1024),b''): h.update(block)
        if h.hexdigest()!=item['sha256']:
            raise RuntimeError('Local SHA256 mismatch: '+name)
        with gzip.open(path,'rb') as f:
            for block in iter(lambda:f.read(1024*1024),b''): pass
        verified[name]=h.hexdigest()
    return verified


def main():
    os.umask(0o077)
    LOCAL.mkdir(parents=True,exist_ok=True)
    with (LOCAL/'.pull.lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        log('Start daily logs pull; archives never expire')
        prepared=remote('prepare')
        # Never use --delete: both local and server backups are permanent.
        cmd=['rsync','-a','--partial','--append-verify','--timeout=120',
             '-e',shlex.join(['ssh']+SSH_ARGS),HOST+':/opt/new-api-backups/logs/',str(LOCAL)+'/']
        result=subprocess.run(cmd,text=True,capture_output=True,timeout=2400)
        if result.returncode:
            raise RuntimeError('Transfer failed; database cleanup skipped: '+result.stderr[-700:])
        verified=verify(prepared['files'])
        payload=dict(yesterday=prepared['yesterday'],verified=verified,local_directory=str(LOCAL))
        (LOCAL/'latest-verified.json').write_text(json.dumps(dict(payload,verified_at=dt.datetime.now().isoformat()),ensure_ascii=False,indent=2)+'\n')
        result=remote('cleanup',payload)
        summary=dict(verified_files=len(verified),verified_bytes=sum(x['bytes'] for x in prepared['files']),
                     local_directory=str(LOCAL),**result)
        (LOCAL/'latest-result.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
        log('SUCCESS '+json.dumps(summary,ensure_ascii=False))

if __name__=='__main__':
    try:
        main()
    except Exception as exc:
        log('FAILED '+str(exc))
        sys.exit(1)
