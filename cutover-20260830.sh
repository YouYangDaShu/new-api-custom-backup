#!/usr/bin/env bash
set -euo pipefail

host='root@154.44.10.112'
old='new-api'
rollback='new-api-rollback-20260830'
new='new-api'
new_image='new-api-local:production-20260830'
base='/opt/new-api'
stamp=$(date +%Y%m%d-%H%M%S)
backup_dir="$base/rollback/cutover-20260830-$stamp"

ssh "$host" "OLD='$old' ROLLBACK='$rollback' NEW='$new' IMAGE='$new_image' BASE='$base' BACKUP_DIR='$backup_dir' bash -s" <<'REMOTE'
set -euo pipefail

if docker inspect "$ROLLBACK" >/dev/null 2>&1; then
  echo 'ERROR rollback container name already exists'
  exit 20
fi
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo 'ERROR target image missing'
  exit 21
fi
if [ "$(docker inspect -f '{{.State.Running}}' "$OLD")" != true ]; then
  echo 'ERROR current production container is not running'
  exit 22
fi

mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
cp -p "$(docker inspect -f '{{.LogPath}}' "$OLD")" "$BACKUP_DIR/container-json.log" 2>/dev/null || true
chmod 600 "$BACKUP_DIR/container-json.log" 2>/dev/null || true
docker inspect "$OLD" > "$BACKUP_DIR/new-api.inspect.json"
chmod 600 "$BACKUP_DIR/new-api.inspect.json"

python3 - "$BACKUP_DIR/one-api.db" <<'PY'
import hashlib, json, os, sqlite3, sys
src='/opt/new-api/data/one-api.db'
out=sys.argv[1]
os.makedirs(os.path.dirname(out), exist_ok=True)
a=sqlite3.connect(f'file:{src}?mode=ro', uri=True, timeout=60)
b=sqlite3.connect(out, timeout=60)
try:
    a.backup(b, pages=4096, sleep=0.05)
finally:
    b.close(); a.close()
os.chmod(out, 0o600)
c=sqlite3.connect(f'file:{out}?mode=ro', uri=True)
try:
    integrity=c.execute('PRAGMA integrity_check').fetchone()[0]
    counts={}
    for t in ('users','channels','tokens','logs','top_ups','options'):
        counts[t]=c.execute(f'SELECT COUNT(*) FROM "{t}"').fetchone()[0]
finally:
    c.close()
h=hashlib.sha256()
with open(out,'rb') as f:
    for chunk in iter(lambda:f.read(8*1024*1024),b''): h.update(chunk)
meta={'bytes':os.path.getsize(out),'sha256':h.hexdigest(),'integrity':integrity,'counts':counts}
with open(out+'.metadata.json','w') as f: json.dump(meta,f)
os.chmod(out+'.metadata.json',0o600)
print('BACKUP_OK bytes=%s sha256=%s integrity=%s counts=%s' % (meta['bytes'], meta['sha256'], meta['integrity'], meta['counts']))
PY

# Keep the old 21-version container intact under a dated rollback name.
docker stop -t 30 "$OLD" >/dev/null
docker rename "$OLD" "$ROLLBACK"

# Recreate from the saved inspect configuration, changing only name and image.
python3 - "$ROLLBACK" "$new" "$IMAGE" <<'PY'
import json, subprocess, sys
old_name,new_name,image=sys.argv[1:]
i=json.load(open(subprocess.check_output(['docker','inspect',old_name],text=True)))[0]
c=i['Config']; h=i['HostConfig']
args=['docker','create','--name',new_name,'--restart',h.get('RestartPolicy',{}).get('Name') or 'no']
if h.get('NetworkMode'):
    args += ['--network', h['NetworkMode']]
if c.get('WorkingDir'):
    args += ['--workdir', c['WorkingDir']]
if c.get('User'):
    args += ['--user', c['User']]
for e in c.get('Env') or []:
    args += ['--env', e]
for b in h.get('Binds') or []:
    args += ['--volume', b]
for p in h.get('PortBindings') or {}:
    for item in h['PortBindings'][p] or []:
        hostip=item.get('HostIp','')
        hostport=item.get('HostPort','')
        pub=(hostip+':' if hostip else '')+hostport
        args += ['--publish', pub+':'+p.split('/')[0]+'/'+p.split('/')[1]]
if h.get('Privileged'): args += ['--privileged']
for cap in h.get('CapAdd') or []: args += ['--cap-add',cap]
for cap in h.get('CapDrop') or []: args += ['--cap-drop',cap]
for d in h.get('Devices') or []:
    args += ['--device', d['PathOnHost']+':'+d['PathInContainer']+(':'+d.get('CgroupPermissions','rwm'))]
args += [image]
if c.get('Entrypoint'):
    ep=c['Entrypoint']; args += ep if isinstance(ep,list) else [ep]
if c.get('Cmd'):
    cmd=c['Cmd']; args += cmd if isinstance(cmd,list) else [cmd]
subprocess.check_call(args, stdout=subprocess.DEVNULL)
PY

docker start "$new" >/dev/null

ok=0
for n in $(seq 1 30); do
  running=$(docker inspect -f '{{.State.Running}}' "$new" 2>/dev/null || true)
  if [ "$running" = true ]; then
    code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:3000/api/status || true)
    if [ "$code" = 200 ]; then ok=1; break; fi
  fi
  sleep 2
done

if [ "$ok" != 1 ]; then
  echo 'CUTOVER_FAILED local health check'
  docker logs "$new" --tail 80 2>&1 | tail -80 || true
  docker rm -f "$new" >/dev/null 2>&1 || true
  docker rename "$rollback" "$old"
  docker start "$old" >/dev/null
  sleep 5
  echo "ROLLBACK_STATUS running=$(docker inspect -f '{{.State.Running}}' "$old") image=$(docker inspect -f '{{.Config.Image}}' "$old")"
  exit 30
fi

public_code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 https://new.youyangai.top/api/status || true)
if [ "$public_code" != 200 ]; then
  echo "CUTOVER_FAILED public_health=$public_code"
  docker rm -f "$new" >/dev/null 2>&1 || true
  docker rename "$rollback" "$old"
  docker start "$old" >/dev/null
  sleep 5
  echo "ROLLBACK_STATUS running=$(docker inspect -f '{{.State.Running}}' "$old") image=$(docker inspect -f '{{.Config.Image}}' "$old")"
  exit 31
fi

printf 'CUTOVER_OK local=200 public=%s new_image=%s old_container=%s\n' "$public_code" "$IMAGE" "$ROLLBACK"
docker inspect "$new" --format 'new_running={{.State.Running}} new_image={{.Config.Image}} new_restart={{.RestartCount}} new_oom={{.State.OOMKilled}}'
docker inspect "$rollback" --format 'rollback_running={{.State.Running}} rollback_image={{.Config.Image}}'
REMOTE
