# Backup scope

This private repository preserves the customized New API production source as append-only release history.

## Preserved releases

- `production-20260821-byte-identical`: byte-identical source baseline for image `new-api-local:production-nocharge-20260821`.
- `production-20260830`: source release for image `new-api-local:production-20260830`.

The 2026-08-30 release is added after the 2026-08-21 commit. The 2026-08-21 commit and tag must not be rewritten, moved, or deleted.

## Included

- Go backend source and tests
- Classic and default frontend source
- Lottery, token-ranking, and channel-monitor customizations
- Request lifecycle, asynchronous billing, refund, and idempotency fixes
- Authentication, access-token, CORS, and public base-path fixes
- Usage-log public-prefix routing fix
- Dockerfiles, Compose examples, module lock files, and source licenses

## Not included

- Production databases or customer data
- Environment files or live credentials
- Logs, caches, build outputs, Docker images, or backup archives
- Hermes/Codex task files and temporary candidate worktrees
- Server-specific Caddy/systemd/runtime configuration containing secrets

## Verification

The 2026-08-21 source and tag remain available in Git history. The 2026-08-30 source is validated and built separately before its release tag is pushed.

See `M5_SOURCE_PROVENANCE.md` for the preserved 2026-08-21 source and image provenance.
