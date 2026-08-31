# FIX3 source overlay provenance

- Snapshot source: `/home/youyang/source-worktrees/newapi-dev`
- Snapshot date: 2026-08-31
- Exact remote production base commit: `016a1a20f993130fea0fba3b7a89b57c0dacea9b` (tag `production-20260830-fix2`)
- Candidate image: `new-api-local:production-20260830-fix3`
- Published as an append-only source overlay; this does not deploy or switch production.
- Included: reviewed FIX3 Go/React source and tests, including the lottery MaxQuota fix.
- Excluded: `.hermes/` task artifacts, `home/` temporary workspaces, image archives, databases, logs, runtime state, credentials, and generated build output.
- Production remains on FIX2 until separately authorized.
