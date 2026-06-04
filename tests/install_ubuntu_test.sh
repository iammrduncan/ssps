#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

CONFIG_FILE="${TMP_DIR}/ssps-install.conf"
OUTPUT_DIR="${TMP_DIR}/rendered"

cat > "${CONFIG_FILE}" <<'CONFIG'
SSPS_SERVICE_NAME=ssps-test
SSPS_USER=ssps-test
SSPS_GROUP=ssps-test
SSPS_INSTALL_DIR=/opt/ssps-test
SSPS_DATA_DIR=/var/lib/ssps-test
SSPS_BINARY=/opt/ssps-test/ssps
SSPS_ENV_FILE=/etc/default/ssps-test
SSPS_ADDR=127.0.0.1:18080
SSPS_DB_PATH=/var/lib/ssps-test/ssps.db
SSPS_FLUSH_INTERVAL=15m
SSPS_DB_CHECKPOINT_INTERVAL=2m
SSPS_DB_COMPACT_INTERVAL=12h
SSPS_WS_UPDATE_INTERVAL=45s
SSPS_LIMIT_NOFILE=999999
SSPS_TASKS_MAX=infinity
SSPS_ENABLE_SYSCTL=1
SSPS_SYSCTL_FILE=/etc/sysctl.d/98-ssps-test.conf
SSPS_SYSCTL_FILE_MAX=1999999
SSPS_SYSCTL_NR_OPEN=1999999
SSPS_SYSCTL_SOMAXCONN=60000
SSPS_SYSCTL_TCP_MAX_SYN_BACKLOG=60000
CONFIG

bash "${ROOT_DIR}/scripts/install-ubuntu.sh" --config "${CONFIG_FILE}" --dry-run --output-dir "${OUTPUT_DIR}"

ENV_FILE="${OUTPUT_DIR}/etc/default/ssps-test"
SERVICE_FILE="${OUTPUT_DIR}/etc/systemd/system/ssps-test.service"
SYSCTL_FILE="${OUTPUT_DIR}/etc/sysctl.d/98-ssps-test.conf"
PLAN_FILE="${OUTPUT_DIR}/install-plan.txt"

grep -q 'SSPS_ADDR="127.0.0.1:18080"' "${ENV_FILE}"
grep -q 'SSPS_DB_CHECKPOINT_INTERVAL="2m"' "${ENV_FILE}"
grep -q 'SSPS_WS_UPDATE_INTERVAL="45s"' "${ENV_FILE}"
grep -q 'EnvironmentFile=/etc/default/ssps-test' "${SERVICE_FILE}"
grep -q 'ExecStart=/opt/ssps-test/ssps' "${SERVICE_FILE}"
grep -q 'LimitNOFILE=999999' "${SERVICE_FILE}"
grep -q 'TasksMax=infinity' "${SERVICE_FILE}"
grep -q 'fs.file-max = 1999999' "${SYSCTL_FILE}"
grep -q 'systemctl enable --now ssps-test' "${PLAN_FILE}"

sed -i.bak 's/127.0.0.1:18080/127.0.0.1:28080/' "${CONFIG_FILE}"
bash "${ROOT_DIR}/scripts/install-ubuntu.sh" --config "${CONFIG_FILE}" --dry-run --output-dir "${OUTPUT_DIR}"
grep -q 'SSPS_ADDR="127.0.0.1:28080"' "${ENV_FILE}"

test -f "${ROOT_DIR}/deploy/ssps-install.conf"
