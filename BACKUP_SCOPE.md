# Backup scope

This private repository is the recoverable source baseline for the customized New API production build dated 2026-08-21.

## Included

- Go backend source and tests
- Classic and default frontend source
- Weekly ranking/reward implementation added on 2026-08-16
- Processing-log timeout/no-charge implementation
- Dockerfiles, Compose examples, module lock files, and source licenses

## Not included

- Production databases or customer data
- Environment files or live credentials
- Logs, caches, build outputs, Docker images, or backup archives
- Server-specific Caddy/systemd/runtime configuration containing secrets

## Verification

The Docker build from this source generated a `/new-api` binary byte-identical to production image `new-api-local:production-nocharge-20260821`.

See `M5_SOURCE_PROVENANCE.md` for exact source path, image ID, binary size, and SHA-256.
