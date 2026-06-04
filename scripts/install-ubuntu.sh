#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG_FILE="${ROOT_DIR}/deploy/ssps-install.conf"
DRY_RUN=0
OUTPUT_DIR=""

usage() {
  cat <<USAGE
Usage: scripts/install-ubuntu.sh [--config PATH] [--dry-run] [--output-dir PATH]

Installs or updates SSPS on Ubuntu. First run installs dependencies, builds the
binary, writes systemd/sysctl config, enables the service, and starts it.
Second and later runs rebuild the binary, rewrite config files, reload systemd,
and restart the service so config changes take effect.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      CONFIG_FILE="${2:?missing value for --config}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --output-dir)
      OUTPUT_DIR="${2:?missing value for --output-dir}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -f "${CONFIG_FILE}" ]]; then
  echo "config file not found: ${CONFIG_FILE}" >&2
  exit 1
fi

# shellcheck source=/dev/null
source "${CONFIG_FILE}"

SSPS_SERVICE_NAME="${SSPS_SERVICE_NAME:-ssps}"
SSPS_USER="${SSPS_USER:-ssps}"
SSPS_GROUP="${SSPS_GROUP:-${SSPS_USER}}"
SSPS_INSTALL_DIR="${SSPS_INSTALL_DIR:-/opt/ssps}"
SSPS_DATA_DIR="${SSPS_DATA_DIR:-/var/lib/ssps}"
SSPS_BINARY="${SSPS_BINARY:-${SSPS_INSTALL_DIR}/ssps}"
SSPS_ENV_FILE="${SSPS_ENV_FILE:-/etc/default/${SSPS_SERVICE_NAME}}"
SSPS_ADDR="${SSPS_ADDR:-127.0.0.1:8080}"
SSPS_DB_PATH="${SSPS_DB_PATH:-${SSPS_DATA_DIR}/ssps.db}"
SSPS_FLUSH_INTERVAL="${SSPS_FLUSH_INTERVAL:-30m}"
SSPS_DB_CHECKPOINT_INTERVAL="${SSPS_DB_CHECKPOINT_INTERVAL:-5m}"
SSPS_DB_COMPACT_INTERVAL="${SSPS_DB_COMPACT_INTERVAL:-24h}"
SSPS_WS_UPDATE_INTERVAL="${SSPS_WS_UPDATE_INTERVAL:-30s}"
SSPS_LIMIT_NOFILE="${SSPS_LIMIT_NOFILE:-1048576}"
SSPS_TASKS_MAX="${SSPS_TASKS_MAX:-infinity}"
SSPS_ENABLE_SYSCTL="${SSPS_ENABLE_SYSCTL:-1}"
SSPS_SYSCTL_FILE="${SSPS_SYSCTL_FILE:-/etc/sysctl.d/99-ssps.conf}"
SSPS_SYSCTL_FILE_MAX="${SSPS_SYSCTL_FILE_MAX:-2000000}"
SSPS_SYSCTL_NR_OPEN="${SSPS_SYSCTL_NR_OPEN:-2000000}"
SSPS_SYSCTL_SOMAXCONN="${SSPS_SYSCTL_SOMAXCONN:-65535}"
SSPS_SYSCTL_TCP_MAX_SYN_BACKLOG="${SSPS_SYSCTL_TCP_MAX_SYN_BACKLOG:-65535}"
SSPS_APT_PACKAGES="${SSPS_APT_PACKAGES:-build-essential ca-certificates curl golang sqlite3 libsqlite3-0 libsqlite3-dev}"
SSPS_RUN_TESTS="${SSPS_RUN_TESTS:-1}"
SSPS_HEALTH_URL="${SSPS_HEALTH_URL:-http://127.0.0.1:8080/healthz}"
SSPS_HEALTH_RETRIES="${SSPS_HEALTH_RETRIES:-12}"
SSPS_HEALTH_RETRY_DELAY="${SSPS_HEALTH_RETRY_DELAY:-2}"

SERVICE_FILE="/etc/systemd/system/${SSPS_SERVICE_NAME}.service"

require_name() {
  local label="$1"
  local value="$2"
  if [[ ! "${value}" =~ ^[A-Za-z0-9_.@-]+$ ]]; then
    echo "${label} contains unsupported characters: ${value}" >&2
    exit 1
  fi
}

quote_value() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "${value}"
}

target_path() {
  local path="$1"
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    printf '%s/%s' "${OUTPUT_DIR}" "${path#/}"
  else
    printf '%s' "${path}"
  fi
}

write_file() {
  local path="$1"
  local rendered
  rendered="$(target_path "${path}")"
  mkdir -p "$(dirname "${rendered}")"
  cat > "${rendered}"
}

render_env() {
  cat <<ENV
SSPS_ADDR=$(quote_value "${SSPS_ADDR}")
SSPS_DB_PATH=$(quote_value "${SSPS_DB_PATH}")
SSPS_FLUSH_INTERVAL=$(quote_value "${SSPS_FLUSH_INTERVAL}")
SSPS_DB_CHECKPOINT_INTERVAL=$(quote_value "${SSPS_DB_CHECKPOINT_INTERVAL}")
SSPS_DB_COMPACT_INTERVAL=$(quote_value "${SSPS_DB_COMPACT_INTERVAL}")
SSPS_WS_UPDATE_INTERVAL=$(quote_value "${SSPS_WS_UPDATE_INTERVAL}")
ENV
}

render_service() {
  cat <<SERVICE
[Unit]
Description=SSPS presence service
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=${SSPS_USER}
Group=${SSPS_GROUP}
WorkingDirectory=${SSPS_DATA_DIR}
EnvironmentFile=${SSPS_ENV_FILE}
ExecStart=${SSPS_BINARY}
Restart=on-failure
RestartSec=5s
StartLimitIntervalSec=60
StartLimitBurst=10
LimitNOFILE=${SSPS_LIMIT_NOFILE}
TasksMax=${SSPS_TASKS_MAX}
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=${SSPS_DATA_DIR}

[Install]
WantedBy=multi-user.target
SERVICE
}

render_sysctl() {
  cat <<SYSCTL
fs.file-max = ${SSPS_SYSCTL_FILE_MAX}
fs.nr_open = ${SSPS_SYSCTL_NR_OPEN}
net.core.somaxconn = ${SSPS_SYSCTL_SOMAXCONN}
net.ipv4.tcp_max_syn_backlog = ${SSPS_SYSCTL_TCP_MAX_SYN_BACKLOG}
SYSCTL
}

render_plan() {
  cat <<PLAN
SSPS install/update plan

Config: ${CONFIG_FILE}
Service: ${SSPS_SERVICE_NAME}
Binary: ${SSPS_BINARY}
Data: ${SSPS_DATA_DIR}
Environment: ${SSPS_ENV_FILE}
systemd unit: ${SERVICE_FILE}

Actions on a real run:
apt-get update
apt-get install -y ${SSPS_APT_PACKAGES}
useradd --system --home ${SSPS_DATA_DIR} --shell /usr/sbin/nologin ${SSPS_USER}
verify Go 1.22 or newer
GOTOOLCHAIN=local go test ./...: ${SSPS_RUN_TESTS}
GOTOOLCHAIN=local go build -o <tmp> ./cmd/ssps
install binary to ${SSPS_BINARY}
write ${SSPS_ENV_FILE}
write ${SERVICE_FILE}
systemctl daemon-reload
systemctl enable --now ${SSPS_SERVICE_NAME}
systemctl restart ${SSPS_SERVICE_NAME}
PLAN
}

require_root() {
  if [[ "${DRY_RUN}" -eq 0 && "$(id -u)" -ne 0 ]]; then
    echo "run as root, for example: sudo $0 --config ${CONFIG_FILE}" >&2
    exit 1
  fi
}

install_packages() {
  apt-get update
  apt-get install -y ${SSPS_APT_PACKAGES}
}

ensure_user_and_dirs() {
  if ! getent group "${SSPS_GROUP}" >/dev/null; then
    groupadd --system "${SSPS_GROUP}"
  fi
  if ! id -u "${SSPS_USER}" >/dev/null 2>&1; then
    useradd --system --gid "${SSPS_GROUP}" --home "${SSPS_DATA_DIR}" --shell /usr/sbin/nologin "${SSPS_USER}"
  fi

  install -d -m 755 "${SSPS_INSTALL_DIR}"
  install -d -m 755 -o "${SSPS_USER}" -g "${SSPS_GROUP}" "${SSPS_DATA_DIR}"
}

verify_go_version() {
  if ! command -v go >/dev/null 2>&1; then
    echo "go is not installed after apt install; check SSPS_APT_PACKAGES" >&2
    exit 1
  fi

  local version
  version="$(GOTOOLCHAIN=local go env GOVERSION 2>/dev/null || true)"
  if [[ -z "${version}" ]]; then
    version="$(GOTOOLCHAIN=local go version | awk '{print $3}')"
  fi
  version="${version#go}"

  local major
  local minor
  local rest
  major="${version%%.*}"
  rest="${version#*.}"
  minor="${rest%%.*}"
  minor="${minor%%[^0-9]*}"

  if [[ -z "${major}" || -z "${minor}" || ! "${major}" =~ ^[0-9]+$ || ! "${minor}" =~ ^[0-9]+$ ]]; then
    echo "could not determine Go version from: ${version}" >&2
    exit 1
  fi

  if ((major < 1 || (major == 1 && minor < 22))); then
    echo "Go 1.22 or newer is required; install a newer golang package or set SSPS_APT_PACKAGES accordingly" >&2
    exit 1
  fi
}

build_and_install_binary() {
  local build_path
  verify_go_version
  build_path="$(mktemp)"
  (
    cd "${ROOT_DIR}"
    if [[ "${SSPS_RUN_TESTS}" == "1" ]]; then
      GOTOOLCHAIN=local go test ./...
    fi
    GOTOOLCHAIN=local go build -o "${build_path}" ./cmd/ssps
  )
  install -m 755 "${build_path}" "${SSPS_BINARY}"
  rm -f "${build_path}"
}

write_real_files() {
  render_env | write_file "${SSPS_ENV_FILE}"
  chmod 0644 "${SSPS_ENV_FILE}"

  render_service | write_file "${SERVICE_FILE}"
  chmod 0644 "${SERVICE_FILE}"

  if [[ "${SSPS_ENABLE_SYSCTL}" == "1" ]]; then
    render_sysctl | write_file "${SSPS_SYSCTL_FILE}"
    chmod 0644 "${SSPS_SYSCTL_FILE}"
  fi
}

apply_system_config() {
  if [[ "${SSPS_ENABLE_SYSCTL}" == "1" ]]; then
    sysctl --system
  fi
  systemctl daemon-reload
  systemctl enable --now "${SSPS_SERVICE_NAME}"
  systemctl restart "${SSPS_SERVICE_NAME}"
}

wait_for_health() {
  local attempt
  for ((attempt = 1; attempt <= SSPS_HEALTH_RETRIES; attempt++)); do
    if curl -fsS --max-time 5 "${SSPS_HEALTH_URL}" >/dev/null; then
      echo "SSPS is healthy at ${SSPS_HEALTH_URL}"
      return 0
    fi
    sleep "${SSPS_HEALTH_RETRY_DELAY}"
  done

  echo "SSPS did not become healthy at ${SSPS_HEALTH_URL}" >&2
  systemctl status "${SSPS_SERVICE_NAME}" --no-pager || true
  journalctl -u "${SSPS_SERVICE_NAME}" -n 80 --no-pager || true
  return 1
}

dry_run() {
  if [[ -z "${OUTPUT_DIR}" ]]; then
    OUTPUT_DIR="${ROOT_DIR}/.install-dry-run"
  fi
  if [[ "${OUTPUT_DIR}" == "/" || "${OUTPUT_DIR}" == "." ]]; then
    echo "unsafe dry-run output directory: ${OUTPUT_DIR}" >&2
    exit 1
  fi
  rm -rf "${OUTPUT_DIR}"
  mkdir -p "${OUTPUT_DIR}"

  render_env | write_file "${SSPS_ENV_FILE}"
  render_service | write_file "${SERVICE_FILE}"
  if [[ "${SSPS_ENABLE_SYSCTL}" == "1" ]]; then
    render_sysctl | write_file "${SSPS_SYSCTL_FILE}"
  fi
  render_plan > "${OUTPUT_DIR}/install-plan.txt"
  echo "dry-run output written to ${OUTPUT_DIR}"
}

main() {
  require_name "SSPS_SERVICE_NAME" "${SSPS_SERVICE_NAME}"
  require_name "SSPS_USER" "${SSPS_USER}"
  require_name "SSPS_GROUP" "${SSPS_GROUP}"

  if [[ "${DRY_RUN}" -eq 1 ]]; then
    dry_run
    return
  fi

  require_root
  install_packages
  ensure_user_and_dirs
  build_and_install_binary
  write_real_files
  apply_system_config
  wait_for_health
}

main "$@"
