#!/usr/bin/env python3
import hashlib
import json
import os
import sqlite3
import subprocess
import sys
from datetime import datetime

source = "/opt/new-api/data/one-api.db"
root = "/opt/new-api/rollback"
stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
out_dir = os.path.join(root, f"pre-20260830-{stamp}")
os.makedirs(out_dir, mode=0o700, exist_ok=False)
backup = os.path.join(out_dir, "one-api.db")

src = sqlite3.connect(f"file:{source}?mode=ro", uri=True, timeout=60)
dst = sqlite3.connect(backup, timeout=60)
try:
    src.backup(dst, pages=4096, sleep=0.05)
finally:
    dst.close()
    src.close()
os.chmod(backup, 0o600)

check = sqlite3.connect(f"file:{backup}?mode=ro", uri=True)
try:
    integrity = check.execute("PRAGMA integrity_check").fetchone()[0]
    counts = {}
    for table in ("users", "channels", "tokens", "logs", "top_ups", "options"):
        try:
            counts[table] = check.execute(f'SELECT COUNT(*) FROM "{table}"').fetchone()[0]
        except sqlite3.Error as exc:
            counts[table] = f"ERROR:{exc}"
finally:
    check.close()

h = hashlib.sha256()
with open(backup, "rb") as fh:
    for chunk in iter(lambda: fh.read(8 * 1024 * 1024), b""):
        h.update(chunk)
sha = h.hexdigest()
with open(os.path.join(out_dir, "one-api.db.sha256"), "w", encoding="utf-8") as fh:
    fh.write(f"{sha}  one-api.db\n")
os.chmod(os.path.join(out_dir, "one-api.db.sha256"), 0o600)

inspect_raw = subprocess.check_output(["docker", "inspect", "new-api"], text=True)
with open(os.path.join(out_dir, "new-api.inspect.json"), "w", encoding="utf-8") as fh:
    fh.write(inspect_raw)
os.chmod(os.path.join(out_dir, "new-api.inspect.json"), 0o600)

meta = {
    "created_at": datetime.now().isoformat(),
    "source": source,
    "backup": backup,
    "bytes": os.path.getsize(backup),
    "sha256": sha,
    "integrity_check": integrity,
    "counts": counts,
}
with open(os.path.join(out_dir, "metadata.json"), "w", encoding="utf-8") as fh:
    json.dump(meta, fh, ensure_ascii=False, indent=2)
os.chmod(os.path.join(out_dir, "metadata.json"), 0o600)
print(json.dumps(meta, ensure_ascii=False))
