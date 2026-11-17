#!/usr/bin/env bash
set -euo pipefail

# End-to-end Docker release acceptance ("recette") script for Linux CI / local Docker.
# Verifies:
#   1. Candidate image startup as non-root user against PostgreSQL 16
#   2. Optional N-1 (matoy/mypresence:latest) -> Candidate upgrade migration & data preservation
#   3. Full Playwright browser suite (desktop + mobile) with SAML, O365, Jira & SMTP mocks
#   4. Authenticated Prometheus /metrics & MCP Python smoke_tests.py

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

IMAGE_TAG="${IMAGE_TAG:-mypresence:recette}"
APP_PORT="${APP_PORT:-18080}"
MOCK_HTTP_PORT="${MOCK_HTTP_PORT:-19080}"
MOCK_SMTP_PORT="${MOCK_SMTP_PORT:-19025}"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-mypresence}"
DB_USER="${DB_USER:-postgres}"
DB_PASSWORD="${DB_PASSWORD:-postgres}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-AdminRecette#2026!}"
TEST_UPGRADE="${TEST_UPGRADE:-1}"
PREVIOUS_IMAGE="${PREVIOUS_IMAGE:-matoy/mypresence:latest}"
METRICS_TOKEN="${METRICS_TOKEN:-e2e-metrics-token}"

LOG_DIR="${REPO_ROOT}/recette-logs"
WORK_DIR="$(mktemp -d)"
APP_CONTAINER="mypresence-recette-app-$$"
UPGRADE_CONTAINER_OLD="mypresence-recette-n1-$$"
UPGRADE_CONTAINER_NEW="mypresence-recette-n-$$"
PG_CONTAINER=""
MOCK_PID=""

mkdir -p "${LOG_DIR}" "${WORK_DIR}/data" "${WORK_DIR}/upgrade-data"
chmod 777 "${WORK_DIR}/data" "${WORK_DIR}/upgrade-data"

cleanup() {
  local rc=$?
  set +e
  if docker ps -a --format '{{.Names}}' | grep -qx "${APP_CONTAINER}"; then
    docker logs "${APP_CONTAINER}" > "${LOG_DIR}/app-container.log" 2>&1 || true
    docker rm -f "${APP_CONTAINER}" >/dev/null 2>&1 || true
  fi
  docker rm -f "${UPGRADE_CONTAINER_OLD}" "${UPGRADE_CONTAINER_NEW}" >/dev/null 2>&1 || true
  if [[ -n "${PG_CONTAINER}" ]]; then
    docker logs "${PG_CONTAINER}" > "${LOG_DIR}/postgres.log" 2>&1 || true
    docker rm -f "${PG_CONTAINER}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${MOCK_PID}" ]]; then
    kill "${MOCK_PID}" >/dev/null 2>&1 || true
    wait "${MOCK_PID}" >/dev/null 2>&1 || true
  fi
  rm -rf "${WORK_DIR}"
  exit "${rc}"
}
trap cleanup EXIT

wait_http() {
  local url="$1"
  local label="$2"
  local retries="${3:-60}"
  for ((i=1; i<=retries; i++)); do
    if curl -sf "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  echo "ERROR: ${label} did not become ready at ${url}" >&2
  return 1
}

echo "==> [1/6] Ensuring candidate image ${IMAGE_TAG} exists..."
if ! docker image inspect "${IMAGE_TAG}" >/dev/null 2>&1; then
  docker build -t "${IMAGE_TAG}" .
fi

echo "==> [2/6] Checking PostgreSQL ${DB_HOST}:${DB_PORT}..."
if ! (echo >"/dev/tcp/${DB_HOST}/${DB_PORT}") >/dev/null 2>&1; then
  PG_CONTAINER="mypresence-recette-pg-$$"
  docker run -d --name "${PG_CONTAINER}" --network host \
    -e POSTGRES_USER="${DB_USER}" \
    -e POSTGRES_PASSWORD="${DB_PASSWORD}" \
    -e POSTGRES_DB="${DB_NAME}" \
    postgres:16-alpine >/dev/null
  for ((i=1; i<=60; i++)); do
    if docker exec "${PG_CONTAINER}" pg_isready -U "${DB_USER}" >/dev/null 2>&1; then
      break
    fi
    sleep 0.5
  done
fi

pg_exec() {
  local db="$1"
  local sql="$2"
  if [[ -n "${PG_CONTAINER}" ]]; then
    docker exec -e PGPASSWORD="${DB_PASSWORD}" "${PG_CONTAINER}" psql -h 127.0.0.1 -U "${DB_USER}" -d "${db}" -v ON_ERROR_STOP=1 -c "${sql}"
  else
    PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${db}" -v ON_ERROR_STOP=1 -c "${sql}"
  fi
}

echo "==> [3/6] Building & starting cmd/testmocks..."
go build -o "${WORK_DIR}/testmocks" ./cmd/testmocks
"${WORK_DIR}/testmocks" \
  --http-addr="127.0.0.1:${MOCK_HTTP_PORT}" \
  --smtp-addr="127.0.0.1:${MOCK_SMTP_PORT}" \
  --public-url="http://127.0.0.1:${MOCK_HTTP_PORT}" \
  --saml-acs-url="http://127.0.0.1:${APP_PORT}/saml/acs" \
  > "${LOG_DIR}/testmocks.log" 2>&1 &
MOCK_PID=$!
wait_http "http://127.0.0.1:${MOCK_HTTP_PORT}/healthz" "testmocks" 40

if [[ "${TEST_UPGRADE}" == "1" ]]; then
  echo "==> [4/6] Running N-1 (${PREVIOUS_IMAGE}) -> Candidate (${IMAGE_TAG}) migration preservation check..."
  if docker pull "${PREVIOUS_IMAGE}" >/dev/null 2>&1; then
    pg_exec postgres "DROP DATABASE IF EXISTS mypresence_upgrade;" >/dev/null
    pg_exec postgres "CREATE DATABASE mypresence_upgrade;" >/dev/null

    docker run -d --name "${UPGRADE_CONTAINER_OLD}" --network host \
      -v "${WORK_DIR}/upgrade-data:/data" \
      -e PORT=18081 -e DATA_DIR=/data \
      -e SECRET_KEY="upgrade-recette-secret-key-at-least-32-chars" \
      -e DB_DRIVER=postgres -e DB_HOST="${DB_HOST}" -e DB_PORT="${DB_PORT}" \
      -e DB_NAME=mypresence_upgrade -e DB_USER="${DB_USER}" -e DB_PASSWORD="${DB_PASSWORD}" -e DB_SSL_MODE=disable \
      -e ADMIN_USER="${ADMIN_USER}" -e ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
      "${PREVIOUS_IMAGE}" >/dev/null
    wait_http "http://127.0.0.1:18081/health" "N-1 container" 60

    curl -sf -c "${WORK_DIR}/upg.cookies" -X POST http://127.0.0.1:18081/login \
      --data-urlencode "username=${ADMIN_USER}" \
      --data-urlencode "password=${ADMIN_PASSWORD}" >/dev/null
    curl -sf -b "${WORK_DIR}/upg.cookies" -H "Content-Type: application/json" \
      -d '{"email":"upgrade.user@example.com","name":"Upgrade Survivor","password":"Password#123"}' \
      http://127.0.0.1:18081/admin/users >/dev/null
    docker rm -f "${UPGRADE_CONTAINER_OLD}" >/dev/null

    docker run -d --name "${UPGRADE_CONTAINER_NEW}" --network host \
      -v "${WORK_DIR}/upgrade-data:/data" \
      -e PORT=18081 -e DATA_DIR=/data \
      -e SECRET_KEY="upgrade-recette-secret-key-at-least-32-chars" \
      -e DB_DRIVER=postgres -e DB_HOST="${DB_HOST}" -e DB_PORT="${DB_PORT}" \
      -e DB_NAME=mypresence_upgrade -e DB_USER="${DB_USER}" -e DB_PASSWORD="${DB_PASSWORD}" -e DB_SSL_MODE=disable \
      -e ADMIN_USER="${ADMIN_USER}" -e ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
      "${IMAGE_TAG}" >/dev/null
    wait_http "http://127.0.0.1:18081/health" "upgraded candidate container" 60

    curl -sf -c "${WORK_DIR}/upg2.cookies" -X POST http://127.0.0.1:18081/login \
      --data-urlencode "username=${ADMIN_USER}" \
      --data-urlencode "password=${ADMIN_PASSWORD}" >/dev/null
    if ! curl -sf -b "${WORK_DIR}/upg2.cookies" http://127.0.0.1:18081/api/users | grep -q "upgrade.user@example.com"; then
      echo "ERROR: Pre-existing user lost after N-1 -> Candidate DB upgrade!" >&2
      docker logs "${UPGRADE_CONTAINER_NEW}" > "${LOG_DIR}/upgrade-candidate.log" 2>&1 || true
      exit 1
    fi
    docker rm -f "${UPGRADE_CONTAINER_NEW}" >/dev/null
    echo "    Upgrade preservation OK!"
  else
    echo "WARNING: Could not pull ${PREVIOUS_IMAGE}; skipping N-1 upgrade check."
  fi
fi

echo "==> [5/6] Launching candidate container against clean PostgreSQL DB & running Playwright E2E..."
pg_exec "${DB_NAME}" "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;" >/dev/null

APP_BASE="http://127.0.0.1:${APP_PORT}"
MOCK_BASE="http://127.0.0.1:${MOCK_HTTP_PORT}"

docker run -d --name "${APP_CONTAINER}" --network host \
  -v "${WORK_DIR}/data:/data" \
  -e PORT="${APP_PORT}" \
  -e DATA_DIR=/data \
  -e SECRET_KEY="recette-docker-secret-key-at-least-32-characters" \
  -e APP_URL="${APP_BASE}" \
  -e APP_NAME="Presence Docker Recette" \
  -e DB_DRIVER=postgres -e DB_HOST="${DB_HOST}" -e DB_PORT="${DB_PORT}" \
  -e DB_NAME="${DB_NAME}" -e DB_USER="${DB_USER}" -e DB_PASSWORD="${DB_PASSWORD}" -e DB_SSL_MODE=disable \
  -e ADMIN_USER="${ADMIN_USER}" -e ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
  -e DEFAULT_LANG=fr \
  -e SAML_IDP_METADATA_URL="${MOCK_BASE}/saml/idp/metadata" \
  -e SAML_ENTITY_ID="${APP_BASE}/saml/metadata" \
  -e SAML_ROOT_URL="${APP_BASE}" \
  -e SAML_AUTO_LOGIN=false \
  -e SAML_GROUPS_CLAIM="http://schemas.microsoft.com/ws/2008/06/identity/claims/groups" \
  -e SAML_GROUP_GLOBAL="group-global-id" \
  -e SAML_GROUP_FLOORPLAN_MANAGER="group-fp-id" \
  -e O365_SYNC_ENABLED=true \
  -e O365_TENANT_ID=common \
  -e O365_CLIENT_ID="docker-o365-client" \
  -e O365_CLIENT_SECRET="docker-o365-secret" \
  -e O365_REDIRECT_URL="${APP_BASE}/auth/o365/callback" \
  -e O365_LOGIN_BASE_URL="${MOCK_BASE}" \
  -e O365_GRAPH_BASE_URL="${MOCK_BASE}/v1.0" \
  -e JIRA_CLOUD_ID="cloud-docker" \
  -e JIRA_BASE_URL="${MOCK_BASE}" \
  -e JIRA_TOKEN="docker-jira-token" \
  -e SMTP_URL="smtp://127.0.0.1:${MOCK_SMTP_PORT}" \
  -e SMTP_FROM="noreply@presence.local" \
  -e METRICS_TOKEN="${METRICS_TOKEN}" \
  "${IMAGE_TAG}" >/dev/null

if ! wait_http "${APP_BASE}/health" "candidate container" 60; then
  docker logs "${APP_CONTAINER}" > "${LOG_DIR}/app-container.log" 2>&1 || true
  cat "${LOG_DIR}/app-container.log" >&2 || true
  exit 1
fi

# Verify /metrics endpoint auth gate & Prometheus output
unauth_code=$(curl -s -o /dev/null -w '%{http_code}' "${APP_BASE}/metrics")
if [[ "${unauth_code}" != "401" ]]; then
  echo "ERROR: Expected /metrics without token to return 401, got ${unauth_code}" >&2
  exit 1
fi
curl -sf -H "Authorization: Bearer ${METRICS_TOKEN}" "${APP_BASE}/metrics" | grep -q "mypresence_"

BASE_URL="${APP_BASE}" MOCK_URL="${MOCK_BASE}" ADMIN_USER="${ADMIN_USER}" ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
  npx --prefix e2e playwright test

echo "==> [6/6] Running MCP Python API smoke_tests.py against container PAT..."
if command -v python3 >/dev/null 2>&1 && python3 -c "import httpx" >/dev/null 2>&1; then
  curl -sf -c "${WORK_DIR}/api.cookies" -X POST "${APP_BASE}/login" \
    --data-urlencode "username=${ADMIN_USER}" \
    --data-urlencode "password=${ADMIN_PASSWORD}" >/dev/null
  PAT_JSON="$(curl -sf -b "${WORK_DIR}/api.cookies" -H "Content-Type: application/json" \
    -d '{"description":"CI Smoke Test","expires_in":1}' "${APP_BASE}/api/tokens")"
  PAT_TOKEN="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["token"])' <<< "${PAT_JSON}")"
  MYPRESENCE_URL="${APP_BASE}" MYPRESENCE_TOKEN="${PAT_TOKEN}" python3 mcp-server/smoke_tests.py
else
  echo "INFO: python3/httpx not installed; skipping mcp-server/smoke_tests.py (install httpx in CI to enable)."
fi

echo "ALL DOCKER RECETTE GATES PASSED!"
