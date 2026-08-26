# Production source provenance — 2026-08-21 build

This branch is a disaster-recovery source backup for the customized New API build currently used in production.

## Authoritative source

- Full M5 filesystem archive: `M5-full-system-20260823/root-filesystem.tar.gz`
- Recovered containerd build snapshot: `snapshots/1273/fs/build`
- Snapshot's last source modification: 2026-08-21 19:32:43 +08:00
- Dependency manifests (`go.mod`, `go.sum`) restored from the earlier verified M5 source snapshot because BuildKit's copied build layer does not retain files consumed by the cached dependency layer.

## Production identity

- Production image tag: `new-api-local:production-nocharge-20260821`
- Production image ID: `sha256:0fac7b9603f80069c88cef83ac71be023ab090d3d528b8f22b1b3a5166f5761e`
- Embedded application version: `new-api-local:aff-transfer-confirm-20260714`

## Reproducibility verification

Building this source with the repository Dockerfile produced `/new-api` exactly matching the production image binary:

- File size: `136216738` bytes
- SHA-256: `e094fe50b92b87d0bba7ea1796d9c50453a0524ea6b130e86b9df0b3ded35184`

The match is byte-for-byte, not only version/string based.

Verified feature source includes:

- Weekly user ranking and reward settlement
- Weekly ranking frontend
- Processing-log timeout/no-charge handling
- Tests for weekly ranking and text-quota/billing behavior

## Exclusions

This repository intentionally excludes production runtime state and secrets, including:

- SQLite databases, WAL/SHM files
- Logs and caches
- `.env` runtime files (the documented `.env.example` is retained)
- Tokens, passwords, API keys, private keys, certificates
- Docker image archives and machine backups
- Built distributions and dependency caches

This is the source baseline to use for future feature development. Production deployment still requires isolated build/testing and a verified rollback path.
