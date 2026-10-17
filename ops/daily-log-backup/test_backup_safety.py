import datetime as dt
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).parent

def load(name, filename):
    spec=importlib.util.spec_from_file_location(name,HERE/filename)
    mod=importlib.util.module_from_spec(spec); spec.loader.exec_module(mod); return mod

w=load('backup_worker','logs-backup-worker.py')
l=load('backup_local','new-api-logs-pull.py')

class BackupSafetyTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name); self.now=dt.datetime(2026,10,17,23,0,tzinfo=w.TZ)
        self.archives=self.root/'logs'; self.archives.mkdir(); self.executed=[]; self.data={}
        self.addCleanup(patch.stopall)
        patch.object(w,'ARCHIVES',self.archives).start()
        patch.object(w,'RECEIPTS',self.root/'receipts').start()
        patch.object(w,'closed_days',lambda now:['2026-10-01','2026-10-02','2026-10-16']).start()
        patch.object(w,'stats',lambda day:dict(self.data[day])).start()
        patch.object(w,'query',self.fake_query).start()
        patch.object(w.time,'sleep',lambda _:None).start()
        for day in ['2026-10-01','2026-10-02','2026-10-16']: self.archive(day)
        self.payload=dict(yesterday='2026-10-16',verified={m['name']:m['sha256'] for m in self.metadata},local_directory='/verified/local')
    @property
    def metadata(self):
        return [json.loads(p.read_text()) for p in self.archives.glob('*.meta.json')]
    def archive(self,day):
        p=self.archives/('logs-'+day.replace('-','')+'.sql.gz')
        with gzip.open(p,'wb') as f:f.write(b'-- test archive only\n')
        s=dict(day=day,rows=3,max_id=42,max_finished_at=123)
        self.data[day]=s
        m=dict(s,name=p.name,bytes=p.stat().st_size,sha256=w.sha256(p),created_at=self.now.isoformat())
        (self.archives/(p.name+'.meta.json')).write_text(json.dumps(m))
    def fake_query(self,sql):
        self.executed.append(sql)
        self.assertIn('DELETE FROM one_api.logs WHERE created_at >=',sql)
        self.assertIn('ORDER BY created_at,id LIMIT 1000',sql)
        return '3'
    def test_no_ack_cannot_delete(self):
        self.payload['verified']={}
        with self.assertRaises(RuntimeError):w.cleanup(self.now,self.payload)
        self.assertEqual(self.executed,[])
    def test_stale_ack_cannot_delete(self):
        self.payload['yesterday']='2026-10-15'
        with self.assertRaises(RuntimeError):w.cleanup(self.now,self.payload)
        self.assertEqual(self.executed,[])
    def test_changed_archive_cannot_delete(self):
        p=self.archives/'logs-20261001.sql.gz'; p.write_bytes(b'corrupt')
        with self.assertRaises(RuntimeError):w.cleanup(self.now,self.payload)
        self.assertEqual(self.executed,[])
    def test_new_rows_after_backup_cannot_delete(self):
        self.data['2026-10-01']['rows']+=1
        with self.assertRaises(RuntimeError):w.cleanup(self.now,self.payload)
        self.assertEqual(self.executed,[])
    def test_missing_old_day_receipt_cannot_delete(self):
        del self.payload['verified']['logs-20261001.sql.gz']
        with self.assertRaises(RuntimeError):w.cleanup(self.now,self.payload)
        self.assertEqual(self.executed,[])
    def test_dry_run_does_not_delete(self):
        r=w.cleanup(self.now,self.payload,dry_run=True)
        self.assertEqual(r['eligible_days'],['2026-10-01'])
        self.assertEqual(r['cutoff_day'],'2026-10-02')
        self.assertEqual(self.executed,[])
    def test_valid_ack_only_deletes_older_than_15_days(self):
        r=w.cleanup(self.now,self.payload)
        self.assertEqual(r['deleted_rows'],3)
        self.assertEqual(r['eligible_days'],['2026-10-01'])
        self.assertEqual(len(self.executed),1)
        self.assertEqual(len(list(self.archives.glob('*.sql.gz'))),3)
        self.assertEqual(len(list((self.root/'receipts').glob('*.json'))),1)
    def test_local_hash_checks_and_corruption_stop(self):
        patch.object(l,'LOCAL',self.archives).start()
        self.assertEqual(len(l.verify(self.metadata)),3)
        (self.archives/'logs-20261016.sql.gz').write_bytes(b'bad')
        with self.assertRaises(RuntimeError):l.verify(self.metadata)
    def test_filename_validation(self):
        self.payload['verified']['../logs-20261001.sql.gz']='a'*64
        with self.assertRaises(RuntimeError):w.cleanup(self.now,self.payload)
        self.assertEqual(self.executed,[])
    def test_backup_has_no_file_expiry_or_system_restart(self):
        for name in ['logs-backup-worker.py','new-api-logs-pull.py']:
            s=(HERE/name).read_text()
            self.assertNotIn('unlink(',s)
            self.assertNotIn('rm -',s)
            self.assertNotIn('systemctl',s)
        self.assertIn("'--skip-add-drop-table'",(HERE/'logs-backup-worker.py').read_text())

if __name__=='__main__':unittest.main(verbosity=2)
