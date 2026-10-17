#!/usr/bin/env python3
"""Daily logs archives; deletion requires a freshly verified local copy."""
import contextlib
import datetime as dt
import fcntl
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from zoneinfo import ZoneInfo

ROOT = Path('/opt/new-api-backups')
ARCHIVES = ROOT / 'logs'
RECEIPTS = ROOT / 'local-verified'
TZ = ZoneInfo('Asia/Shanghai')
RETAIN_DAYS = 15
BATCH_SIZE = 1000
SAFE_NAME = re.compile(r'logs-\d{8}(?:-\d{8}T\d{6}(?:-\d+)?)?\.sql\.gz\Z')


def emit(message):
    print(message, file=sys.stderr, flush=True)


def docker_tool(tool, args):
    return ['docker', 'exec', 'newapi-mysql', 'sh', '-c',
            'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec ' + tool + ' "$@"',
            'sh', '-uroot'] + args


def query(sql):
    result = subprocess.run(docker_tool('mysql', ['--default-character-set=utf8mb4', '-N', '-B', '-e',
        "SET time_zone='+08:00'; SET SESSION innodb_lock_wait_timeout=5; " + sql]),
        text=True, capture_output=True, timeout=120)
    if result.returncode:
        raise RuntimeError('MySQL failed: ' + result.stderr[-600:])
    return result.stdout.strip()


def sha256(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def date_bounds(day):
    start = dt.datetime.combine(dt.date.fromisoformat(day), dt.time(), TZ)
    return int(start.timestamp()), int((start + dt.timedelta(days=1)).timestamp())


def stats(day):
    a, b = date_bounds(day)
    raw = query(f'SELECT COUNT(*),COALESCE(MAX(id),0),COALESCE(MAX(finished_at),0) '
                f'FROM one_api.logs WHERE created_at >= {a} AND created_at < {b};')
    count, max_id, max_finished = map(int, raw.split('\t'))
    return dict(day=day, rows=count, max_id=max_id, max_finished_at=max_finished)


def stored_metadata(day):
    valid = []
    for p in ARCHIVES.glob('logs-' + day.replace('-', '') + '*.meta.json'):
        try:
            m = json.loads(p.read_text())
            if m['day'] == day and SAFE_NAME.fullmatch(m['name']):
                valid.append(m)
        except (OSError, ValueError, KeyError):
            continue
    return sorted(valid, key=lambda m: m.get('created_at', ''), reverse=True)


def matches(m, s):
    return all(m.get(k) == s[k] for k in ['day', 'rows', 'max_id', 'max_finished_at'])


def checked_archive(m):
    p = ARCHIVES / m['name']
    return p.is_file() and p.stat().st_size == m['bytes'] and sha256(p) == m['sha256']


def dump_day(day):
    for attempt in range(2):
        before = stats(day)
        for existing in stored_metadata(day):
            if matches(existing, before) and checked_archive(existing):
                return existing
        base = 'logs-' + day.replace('-', '')
        path = ARCHIVES / (base + '.sql.gz')
        if path.exists():
            stamp = dt.datetime.now(TZ).strftime('%Y%m%dT%H%M%S')
            path = ARCHIVES / (base + '-' + stamp + '.sql.gz')
            serial = 1
            while path.exists():
                path = ARCHIVES / (base + '-' + stamp + '-' + str(serial) + '.sql.gz')
                serial += 1
        partial = path.with_name(path.name + '.partial')
        a, b = date_bounds(day)
        args = ['--single-transaction', '--skip-lock-tables', '--quick', '--no-tablespaces',
                '--set-gtid-purged=OFF', '--skip-add-drop-table', '--hex-blob',
                '--default-character-set=utf8mb4',
                f'--where=created_at >= {a} AND created_at < {b}', 'one_api', 'logs']
        emit(f'Archive {day}: {before["rows"]} rows')
        with partial.open('wb') as out, contextlib.ExitStack() as stack:
            errfile = stack.enter_context(__import__('tempfile').TemporaryFile())
            proc = subprocess.Popen(docker_tool('mysqldump', args), stdout=subprocess.PIPE, stderr=errfile)
            try:
                with gzip.GzipFile(filename='', mode='wb', fileobj=out, compresslevel=1, mtime=0) as zipped:
                    for block in iter(lambda: proc.stdout.read(1024 * 1024), b''):
                        zipped.write(block)
                rc = proc.wait(timeout=600)
            except BaseException:
                proc.kill(); proc.wait(); raise
            finally:
                proc.stdout.close()
            if rc:
                errfile.seek(0)
                raise RuntimeError('mysqldump failed: ' + errfile.read().decode(errors='replace')[-600:])
            out.flush(); os.fsync(out.fileno())
        subprocess.run(['gzip', '-t', str(partial)], check=True, timeout=600)
        if stats(day) != before:
            emit(f'Day {day} changed during dump; retry without deleting anything')
            continue
        os.replace(partial, path)
        m = dict(before, name=path.name, bytes=path.stat().st_size, sha256=sha256(path),
                 created_at=dt.datetime.now(TZ).isoformat())
        meta = ARCHIVES / (path.name + '.meta.json')
        meta.write_text(json.dumps(m, ensure_ascii=False, indent=2) + '\n')
        return m
    raise RuntimeError('Logs changed while archiving ' + day)


def closed_days(now):
    today_start = int(dt.datetime.combine(now.date(), dt.time(), TZ).timestamp())
    text = query(f"SELECT DISTINCT DATE(FROM_UNIXTIME(created_at)) FROM one_api.logs "
                 f"WHERE created_at < {today_start} ORDER BY 1;")
    days = set(text.splitlines()) if text else set()
    days.add((now.date() - dt.timedelta(days=1)).isoformat())
    return sorted(days)


def manifest():
    files = []
    for p in sorted(ARCHIVES.glob('*.sql.gz')):
        if not SAFE_NAME.fullmatch(p.name):
            raise RuntimeError('Unexpected archive filename: ' + p.name)
        files.append(dict(name=p.name, bytes=p.stat().st_size, sha256=sha256(p)))
    return files


def prepare(now):
    for day in closed_days(now):
        dump_day(day)
    return dict(prepared_at=now.isoformat(), yesterday=(now.date()-dt.timedelta(days=1)).isoformat(),
                files=manifest(), database_retain_days=RETAIN_DAYS, backup_expiry=False)


def cleanup(now, payload, dry_run=False):
    yesterday = (now.date() - dt.timedelta(days=1)).isoformat()
    if payload.get('yesterday') != yesterday:
        raise RuntimeError('Local verification is not for the current previous day; no deletion')
    ack = payload.get('verified', {})
    for name, digest in ack.items():
        if not SAFE_NAME.fullmatch(name) or not re.fullmatch(r'[0-9a-f]{64}', digest):
            raise RuntimeError('Invalid verification receipt; no deletion')
    def require_verified(day):
        current = stats(day)
        for m in stored_metadata(day):
            if matches(m, current) and ack.get(m['name']) == m['sha256'] and checked_archive(m):
                return m
        raise RuntimeError(f'{day}: no matching local-verified complete archive; no deletion')
    # Always require yesterday, even when there are currently no old rows.
    required = [require_verified(yesterday)]
    cutoff_day = now.date() - dt.timedelta(days=RETAIN_DAYS)
    eligible = [d for d in closed_days(now) if dt.date.fromisoformat(d) < cutoff_day]
    for day in eligible:
        required.append(require_verified(day))
    if dry_run:
        return dict(dry_run=True, eligible_days=eligible, deleted_rows=0,
                    retain_days=RETAIN_DAYS, cutoff_day=cutoff_day.isoformat())
    RECEIPTS.mkdir(parents=True, exist_ok=True)
    receipt = dict(verified_at=dt.datetime.now(TZ).isoformat(), local_directory=payload.get('local_directory'),
                   files={m['name']:m['sha256'] for m in required})
    (RECEIPTS / ('verified-' + now.strftime('%Y%m%dT%H%M%S') + '.json')).write_text(
        json.dumps(receipt, ensure_ascii=False, indent=2) + '\n')
    deleted = 0
    for m in required[1:]:
        a,b = date_bounds(m['day'])
        if not matches(m,stats(m['day'])):
            raise RuntimeError('Archived day changed before cleanup; stop')
        while True:
            n = int(query(f'DELETE FROM one_api.logs WHERE created_at >= {a} AND created_at < {b} '
                          f'ORDER BY created_at,id LIMIT {BATCH_SIZE}; SELECT ROW_COUNT();').splitlines()[-1])
            deleted += n
            emit(f'Cleanup {m["day"]}: deleted {n}; total {deleted}')
            if n < BATCH_SIZE:
                break
            time.sleep(0.3)
    return dict(deleted_rows=deleted, eligible_days=eligible, retain_days=RETAIN_DAYS,
                cutoff_day=cutoff_day.isoformat(), local_verified=True, backup_expiry=False)


def main():
    os.umask(0o077)
    ROOT.mkdir(parents=True,exist_ok=True)
    ARCHIVES.mkdir(parents=True,exist_ok=True)
    with open('/tmp/new-api-logs-backup.lock', 'a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        now = dt.datetime.now(TZ)
        action = sys.argv[1] if len(sys.argv)>1 else 'prepare'
        if action == 'prepare':
            result=prepare(now)
        elif action in ('cleanup','dry-run'):
            result=cleanup(now,json.load(sys.stdin),dry_run=action=='dry-run')
        else:
            raise RuntimeError('Unknown action')
        print(json.dumps(result,ensure_ascii=False),flush=True)

if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        emit('BACKUP FAILED: '+str(exc))
        sys.exit(1)
