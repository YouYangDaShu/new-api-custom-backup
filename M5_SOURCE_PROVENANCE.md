# M5 source provenance

- Archive: `M5-final-delta-20260824/final-business-delta.tar.gz`
- Recovered path: `/opt/new-api/build-src/new-api`
- Archive source tree size: 2091 files, about 29 MB including Git metadata
- Archived Git HEAD: `5e866446497652bf8581bb7833fae565beb39fd6`
- Archived Git tag: `v1.0.0-rc.14`
- Current production image associated with this recovery: `new-api-local:production-nocharge-20260821`
- The archive also contains the production image exports `new-api-production-nocharge-20260821.tar` and `new-api-production-nocharge-116f1d3.tar.gz`.

The final working source tree used for `main` is the newer local customized tree recovered from the M5 backup context, with the production features retained: rankings, rewards/rebates, smart routing, token groups, and processing-log no-charge handling.

Runtime data, databases, logs, credentials, `.env` files, certificates, dependency trees, and build outputs are excluded.
