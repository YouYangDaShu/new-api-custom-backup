#!/usr/bin/env bash
# 每日归档，不自行清库；清理只能由 M5 拉取校验后调用。
set -Eeuo pipefail
umask 077
exec /usr/bin/python3 /opt/new-api-backups/logs-backup-worker.py prepare
