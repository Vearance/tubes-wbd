#!/usr/bin/env bash
# End-to-end observability smoke and load test for BScheme dashboards.
#
# Exercises API traffic, auth outcomes (success / invalid_credentials /
# user_disabled / 401 / 403), viewer operations, SQL query + EXPLAIN,
# concurrent pool pressure, and the full migration pipeline (DDL, batch,
# rollback, forward, failed, rejected, pending_review), then verifies
# Prometheus, Loki, and Tempo ingestion.
#
# Usage:
#   bash scripts/test-observability.sh [options]
#
# Options:
#   --quick            Fast 1-wave check (waves=1, interval=5s, scrape-wait=5s)
#   --waves N          Number of traffic waves (default: 2)
#   --interval SEC     Seconds between waves (default: 16; >15s Prometheus scrape)
#   --concurrency N    Parallel requests per wave burst (default: 6)
#   --skip-migrations  Run API/Auth/Viewer/Pool traffic only
#   --no-failures      Skip the intentional failed migration (avoids triggering BSchemeMigrationFailures)
#   --no-verify        Skip Prometheus, Loki, and Tempo verification queries
#   -h, --help         Show this help and exit

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${REPO_ROOT}/.env"

WAVES=2
INTERVAL=16
SCRAPE_WAIT=16
CONCURRENCY=6
SKIP_MIGRATIONS=false
NO_FAILURES=false
NO_VERIFY=false

usage() {
	sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--quick)
			WAVES=1
			INTERVAL=5
			SCRAPE_WAIT=5
			shift
			;;
		--waves)
			[[ $# -ge 2 ]] || { echo "error: --waves requires a value" >&2; exit 2; }
			WAVES="$2"
			shift 2
			;;
		--interval)
			[[ $# -ge 2 ]] || { echo "error: --interval requires a value" >&2; exit 2; }
			INTERVAL="$2"
			shift 2
			;;
		--concurrency)
			[[ $# -ge 2 ]] || { echo "error: --concurrency requires a value" >&2; exit 2; }
			CONCURRENCY="$2"
			shift 2
			;;
		--skip-migrations)
			SKIP_MIGRATIONS=true
			shift
			;;
		--no-failures)
			NO_FAILURES=true
			shift
			;;
		--no-verify)
			NO_VERIFY=true
			shift
			;;
		-h|--help)
			usage
			cat <<'EOF'

Environment overrides (otherwise read from .env or Compose defaults):
  API_URL           (default: http://localhost:${APP_PORT:-8080})
  WORKER_URL        (default: http://localhost:${WORKER_METRICS_PORT:-9091})
  PROMETHEUS_URL    (default: http://localhost:${PROMETHEUS_PORT:-9090})
  LOKI_URL          (default: http://localhost:${LOKI_PORT:-3100})
  TEMPO_URL         (default: http://localhost:${TEMPO_PORT:-3200})
  GRAFANA_URL       (default: http://localhost:${GRAFANA_PORT:-3000})
  TARGET_DB_HOST    (default: test-db; set localhost if backend runs outside Docker)
  TARGET_DB_PORT    (default: 5432; set ${TEST_DB_HOST_PORT:-5434} outside Docker)
EOF
			exit 0
			;;
		*)
			echo "error: unknown option '$1'" >&2
			usage >&2
			exit 2
			;;
	esac
done

for num_arg in "$WAVES" "$INTERVAL" "$CONCURRENCY"; do
	if ! [[ "$num_arg" =~ ^[0-9]+$ ]]; then
		echo "error: numeric option expected, got '$num_arg'" >&2
		exit 2
	fi
done
if [[ "$WAVES" -lt 1 || "$CONCURRENCY" -lt 1 ]]; then
	echo "error: --waves and --concurrency must be >= 1" >&2
	exit 2
fi

# Read a scalar key from .env without executing arbitrary shell code.
read_dotenv_key() {
	local key="$1"
	local default="$2"
	local val=""
	if [[ -f "$ENV_FILE" ]]; then
		val="$(awk -F= -v k="$key" '
			$0 !~ /^[[:space:]]*#/ && $1 == k {
				sub(/^[^=]*=/, "", $0)
				line = $0
			}
			END { print line }
		' "$ENV_FILE" | tr -d '\r')"
		if [[ "${val}" =~ ^\"(.*)\"$ ]]; then
			val="${BASH_REMATCH[1]}"
		elif [[ "${val}" =~ ^\'(.*)\'$ ]]; then
			val="${BASH_REMATCH[1]}"
		fi
	fi
	if [[ -n "$val" ]]; then
		printf '%s' "$val"
	else
		printf '%s' "$default"
	fi
}

APP_PORT="${APP_PORT:-$(read_dotenv_key APP_PORT 8080)}"
WORKER_METRICS_PORT="${WORKER_METRICS_PORT:-$(read_dotenv_key WORKER_METRICS_PORT 9091)}"
PROMETHEUS_PORT="${PROMETHEUS_PORT:-$(read_dotenv_key PROMETHEUS_PORT 9090)}"
LOKI_PORT="${LOKI_PORT:-$(read_dotenv_key LOKI_PORT 3100)}"
TEMPO_PORT="${TEMPO_PORT:-$(read_dotenv_key TEMPO_PORT 3200)}"
GRAFANA_PORT="${GRAFANA_PORT:-$(read_dotenv_key GRAFANA_PORT 3000)}"

API_URL="${API_URL:-http://localhost:${APP_PORT}}"
WORKER_URL="${WORKER_URL:-http://localhost:${WORKER_METRICS_PORT}}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:${PROMETHEUS_PORT}}"
LOKI_URL="${LOKI_URL:-http://localhost:${LOKI_PORT}}"
TEMPO_URL="${TEMPO_URL:-http://localhost:${TEMPO_PORT}}"
GRAFANA_URL="${GRAFANA_URL:-http://localhost:${GRAFANA_PORT}}"

ADMIN_USER="${APP_ADMIN_USERNAME:-$(read_dotenv_key APP_ADMIN_USERNAME admin)}"
ADMIN_PASS="${APP_ADMIN_PASSWORD:-$(read_dotenv_key APP_ADMIN_PASSWORD password)}"

TARGET_DB_HOST="${TARGET_DB_HOST:-test-db}"
TARGET_DB_PORT="${TARGET_DB_PORT:-5432}"
TARGET_DB_USER="${TEST_DB_USER:-$(read_dotenv_key TEST_DB_USER target)}"
TARGET_DB_PASS="${TEST_DB_PASSWORD:-$(read_dotenv_key TEST_DB_PASSWORD target_password)}"
TARGET_DB_NAME="${TEST_DB_NAME:-$(read_dotenv_key TEST_DB_NAME targetdb)}"
TARGET_DB_SSL_MODE="${TARGET_DB_SSL_MODE:-disable}"

RUN_ID="obs_$(date +%s)"
CORR_ID="obs-test-${RUN_ID}"
MIGRATION_CORR_ID="obs-mig-${RUN_ID}"

TMPDIR_RUN="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_RUN"' EXIT
ADMIN_COOKIES="${TMPDIR_RUN}/admin.cookies"
USER_COOKIES="${TMPDIR_RUN}/user.cookies"
RESP_FILE="${TMPDIR_RUN}/resp.body"

OBS_TESTER_USER="obs_tester"
OBS_TESTER_PASS="obs-tester-pass-123!"
OBS_DISABLED_USER="obs_disabled"
OBS_DISABLED_PASS="obs-disabled-pass-123!"
OBS_GROUP_NAME="obs-smoke-team"
OBS_DB_NAME="obs-smoke-target"

START_SEC="$(date +%s)"
HTTP_CODE=""
BODY=""

log_phase() {
	printf '\n\033[1;34m==> %s\033[0m\n' "$1"
}

log_ok() {
	printf '  \033[0;32m[OK]\033[0m %s\n' "$1"
}

log_warn() {
	printf '  \033[0;33m[WARN]\033[0m %s\n' "$1"
}

log_fail() {
	printf '  \033[0;31m[FAIL]\033[0m %s\n' "$1" >&2
}

require_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command '$1' is not installed" >&2
		exit 1
	fi
}

# Execute an HTTP request and store status code in HTTP_CODE and response in BODY.
# Usage: http_call METHOD URL [COOKIE_JAR] [CSRF_TOKEN] [JSON_BODY] [CORR_HEADER] [SAVE_COOKIES]
http_call() {
	local method="$1"
	local url="$2"
	local cookie_jar="${3:-}"
	local csrf="${4:-}"
	local json_body="${5:-}"
	local corr="${6:-$CORR_ID}"
	local save_cookies="${7:-}"

	local -a args=(-sS -o "$RESP_FILE" -w '%{http_code}' -X "$method" -H "X-Correlation-ID: ${corr}")
	if [[ -n "$cookie_jar" ]]; then
		args+=(-b "$cookie_jar")
	fi
	if [[ -n "$save_cookies" ]]; then
		args+=(-c "$save_cookies")
	fi
	if [[ -n "$csrf" ]]; then
		args+=(-H "X-CSRF-Token: ${csrf}")
	fi
	if [[ -n "$json_body" ]]; then
		args+=(-H "Content-Type: application/json" -d "$json_body")
	fi

	HTTP_CODE="$(curl "${args[@]}" "$url")"
	BODY="$(cat "$RESP_FILE")"
}

assert_status_in() {
	local expected_csv="$1"
	local context="$2"
	local code="$HTTP_CODE"
	local IFS=','
	local exp
	for exp in $expected_csv; do
		if [[ "$code" == "$exp" ]]; then
			return 0
		fi
	done
	log_fail "${context}: expected HTTP [${expected_csv}], got ${code}: ${BODY}"
	return 1
}

probe_endpoint() {
	local name="$1"
	local url="$2"
	if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
		log_ok "${name} reachable (${url})"
		return 0
	else
		log_warn "${name} NOT reachable (${url})"
		return 1
	fi
}

# Submit -> approve -> enqueue -> wait for a draft-backed migration execution.
run_draft_execution() {
	local label="$1"
	local payload="$2"
	local expected_status="${3:-success}"

	http_call POST "${API_URL}/databases/${OBS_DB_ID}/migrations/drafts" \
		"$USER_COOKIES" "$USER_CSRF" "$payload" "$MIGRATION_CORR_ID"
	assert_status_in "201,200" "create draft (${label})"
	local draft_id
	draft_id="$(jq -r '.id' <<<"$BODY")"

	http_call POST "${API_URL}/migrations/drafts/${draft_id}/execute" \
		"$USER_COOKIES" "$USER_CSRF" "" "$MIGRATION_CORR_ID"
	assert_status_in "202,200" "submit draft (${label})"
	local exec_id
	exec_id="$(jq -r '.id' <<<"$BODY")"

	approve_and_enqueue_execution "$label" "$exec_id" "$expected_status"
}

approve_and_enqueue_execution() {
	local label="$1"
	local exec_id="$2"
	local expected_status="$3"

	local approve_body
	approve_body="$(jq -cn --arg note "observability smoke test (${RUN_ID}): ${label}" '{note:$note}')"
	http_call POST "${API_URL}/migrations/executions/${exec_id}/approve" \
		"$USER_COOKIES" "$USER_CSRF" "$approve_body" "$MIGRATION_CORR_ID"
	assert_status_in "200" "approve execution #${exec_id} (${label})"

	local exec_status
	exec_status="$(jq -r '.execution_status' <<<"$BODY")"
	if [[ "$exec_status" != "approved" ]]; then
		log_fail "execution #${exec_id} (${label}) remained '${exec_status}' after approval (another group with access to '${OBS_DB_NAME}' may need to approve)"
		return 1
	fi

	http_call POST "${API_URL}/migrations/executions/${exec_id}/enqueue" \
		"$USER_COOKIES" "$USER_CSRF" "" "$MIGRATION_CORR_ID"
	assert_status_in "202,200" "enqueue execution #${exec_id} (${label})"

	wait_for_execution "$label" "$exec_id" "$expected_status"
}

wait_for_execution() {
	local label="$1"
	local exec_id="$2"
	local expected_status="$3"
	local deadline=$(( $(date +%s) + 45 ))
	local status=""

	while [[ "$(date +%s)" -le "$deadline" ]]; do
		http_call GET "${API_URL}/migrations/executions/${exec_id}" \
			"$USER_COOKIES" "" "" "$MIGRATION_CORR_ID"
		assert_status_in "200" "poll execution #${exec_id}"
		status="$(jq -r '.status' <<<"$BODY")"
		case "$status" in
			success|failed|partial|compensated|rejected)
				if [[ "$status" == "$expected_status" ]]; then
					log_ok "${label}: execution #${exec_id} -> ${status}"
					return 0
				fi
				log_fail "${label}: execution #${exec_id} ended as '${status}', expected '${expected_status}': $(jq -r '.error_message // empty' <<<"$BODY")"
				return 1
				;;
		esac
		sleep 1
	done

	log_fail "${label}: timed out after 45s waiting for worker (last status: '${status}')"
	return 1
}

# ---------------------------------------------------------------------------
# Phase 1: Dependencies & Health Checks
# ---------------------------------------------------------------------------
log_phase "Phase 1: Checking tools & service health"
require_cmd curl
require_cmd jq

if ! probe_endpoint "bscheme-api" "${API_URL}/metrics"; then
	echo "error: bscheme-api is unreachable at ${API_URL}. Start the stack first (e.g. 'docker compose up --build')." >&2
	exit 1
fi

WORKER_UP=true
probe_endpoint "bscheme-worker" "${WORKER_URL}/healthz" || WORKER_UP=false
PROM_UP=true
probe_endpoint "Prometheus" "${PROMETHEUS_URL}/-/ready" || PROM_UP=false
LOKI_UP=true
probe_endpoint "Loki" "${LOKI_URL}/ready" || LOKI_UP=false
TEMPO_UP=true
probe_endpoint "Tempo" "${TEMPO_URL}/ready" || TEMPO_UP=false
probe_endpoint "Grafana" "${GRAFANA_URL}/api/health" || true

if [[ "$SKIP_MIGRATIONS" == "false" && "$WORKER_UP" == "false" ]]; then
	log_warn "bscheme-worker looks down; migration executions will stay queued unless the worker container is running."
fi

# ---------------------------------------------------------------------------
# Phase 2: Idempotent Admin Bootstrap
# ---------------------------------------------------------------------------
log_phase "Phase 2: Bootstrapping idempotent test users, group, and target DB"

admin_login_payload="$(jq -cn --arg u "$ADMIN_USER" --arg p "$ADMIN_PASS" '{username:$u,password:$p}')"
http_call POST "${API_URL}/auth/login" "" "" "$admin_login_payload" "$CORR_ID" "$ADMIN_COOKIES"
assert_status_in "200" "admin login as '${ADMIN_USER}'"
ADMIN_CSRF="$(jq -r '.csrf_token' <<<"$BODY")"
log_ok "Logged in as admin '${ADMIN_USER}'"

http_call GET "${API_URL}/admin/ping" "$ADMIN_COOKIES"
assert_status_in "200" "GET /admin/ping"

# Ensure obs_tester exists & is enabled.
http_call GET "${API_URL}/admin/users" "$ADMIN_COOKIES"
assert_status_in "200" "GET /admin/users"
OBS_TESTER_ID="$(jq -r --arg u "$OBS_TESTER_USER" '.[] | select(.username == $u) | .id' <<<"$BODY" | head -n1)"
if [[ -z "$OBS_TESTER_ID" ]]; then
	create_user_payload="$(jq -cn --arg u "$OBS_TESTER_USER" --arg p "$OBS_TESTER_PASS" '{username:$u,password:$p}')"
	http_call POST "${API_URL}/admin/users" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$create_user_payload"
	assert_status_in "201,200" "create user '${OBS_TESTER_USER}'"
	OBS_TESTER_ID="$(jq -r '.id' <<<"$BODY")"
	log_ok "Created user '${OBS_TESTER_USER}' (id=${OBS_TESTER_ID})"
else
	tester_disabled="$(jq -r --arg u "$OBS_TESTER_USER" '.[] | select(.username == $u) | .disabled' <<<"$BODY" | head -n1)"
	if [[ "$tester_disabled" == "true" ]]; then
		http_call PATCH "${API_URL}/admin/users/${OBS_TESTER_ID}" "$ADMIN_COOKIES" "$ADMIN_CSRF" '{"disabled":false}'
		assert_status_in "200" "enable user '${OBS_TESTER_USER}'"
	fi
	log_ok "Reusing user '${OBS_TESTER_USER}' (id=${OBS_TESTER_ID})"
fi

# Ensure obs_disabled exists & is disabled (used to generate auth outcome=user_disabled).
http_call GET "${API_URL}/admin/users" "$ADMIN_COOKIES"
OBS_DISABLED_ID="$(jq -r --arg u "$OBS_DISABLED_USER" '.[] | select(.username == $u) | .id' <<<"$BODY" | head -n1)"
if [[ -z "$OBS_DISABLED_ID" ]]; then
	create_dis_payload="$(jq -cn --arg u "$OBS_DISABLED_USER" --arg p "$OBS_DISABLED_PASS" '{username:$u,password:$p}')"
	http_call POST "${API_URL}/admin/users" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$create_dis_payload"
	assert_status_in "201,200" "create user '${OBS_DISABLED_USER}'"
	OBS_DISABLED_ID="$(jq -r '.id' <<<"$BODY")"
fi
http_call PATCH "${API_URL}/admin/users/${OBS_DISABLED_ID}" "$ADMIN_COOKIES" "$ADMIN_CSRF" '{"disabled":true}'
assert_status_in "200" "disable user '${OBS_DISABLED_USER}'"
log_ok "Ensured disabled user '${OBS_DISABLED_USER}' (id=${OBS_DISABLED_ID})"

# Ensure obs-smoke-team exists, has obs_tester as member & leader.
http_call GET "${API_URL}/admin/groups" "$ADMIN_COOKIES"
assert_status_in "200" "GET /admin/groups"
OBS_GROUP_ID="$(jq -r --arg g "$OBS_GROUP_NAME" '.[] | select(.name == $g) | .id' <<<"$BODY" | head -n1)"
OBS_GROUP_LEADER="$(jq -r --arg g "$OBS_GROUP_NAME" '.[] | select(.name == $g) | (.leader_user_id // empty)' <<<"$BODY" | head -n1)"
if [[ -z "$OBS_GROUP_ID" ]]; then
	create_group_payload="$(jq -cn --arg n "$OBS_GROUP_NAME" '{name:$n}')"
	http_call POST "${API_URL}/admin/groups" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$create_group_payload"
	assert_status_in "201,200" "create group '${OBS_GROUP_NAME}'"
	OBS_GROUP_ID="$(jq -r '.id' <<<"$BODY")"
	OBS_GROUP_LEADER=""
	log_ok "Created group '${OBS_GROUP_NAME}' (id=${OBS_GROUP_ID})"
else
	log_ok "Reusing group '${OBS_GROUP_NAME}' (id=${OBS_GROUP_ID})"
fi

http_call GET "${API_URL}/admin/groups/${OBS_GROUP_ID}/members" "$ADMIN_COOKIES"
assert_status_in "200" "GET /admin/groups/${OBS_GROUP_ID}/members"
is_member="$(jq -r --argjson uid "$OBS_TESTER_ID" 'any(.[]; .id == $uid)' <<<"$BODY")"
if [[ "$is_member" != "true" ]]; then
	member_payload="$(jq -cn --argjson uid "$OBS_TESTER_ID" '{user_id:$uid}')"
	http_call POST "${API_URL}/admin/groups/${OBS_GROUP_ID}/members" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$member_payload"
	assert_status_in "201,200,204" "add '${OBS_TESTER_USER}' to '${OBS_GROUP_NAME}'"
fi
if [[ "$OBS_GROUP_LEADER" != "$OBS_TESTER_ID" ]]; then
	leader_payload="$(jq -cn --argjson uid "$OBS_TESTER_ID" '{user_id:$uid}')"
	http_call PUT "${API_URL}/admin/groups/${OBS_GROUP_ID}/leader" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$leader_payload"
	assert_status_in "200" "set '${OBS_TESTER_USER}' as leader of '${OBS_GROUP_NAME}'"
fi
log_ok "Ensured '${OBS_TESTER_USER}' is member & leader of '${OBS_GROUP_NAME}'"

# Ensure obs-smoke-target managed database exists and is reachable.
http_call GET "${API_URL}/admin/databases" "$ADMIN_COOKIES"
assert_status_in "200" "GET /admin/databases"
OBS_DB_ID="$(jq -r --arg n "$OBS_DB_NAME" '.[] | select(.name == $n) | .id' <<<"$BODY" | head -n1)"
if [[ -z "$OBS_DB_ID" ]]; then
	create_db_payload="$(jq -cn \
		--arg name "$OBS_DB_NAME" \
		--arg host "$TARGET_DB_HOST" \
		--argjson port "$TARGET_DB_PORT" \
		--arg user "$TARGET_DB_USER" \
		--arg pass "$TARGET_DB_PASS" \
		--arg dbname "$TARGET_DB_NAME" \
		--arg ssl "$TARGET_DB_SSL_MODE" \
		'{name:$name,host:$host,port:$port,username:$user,password:$pass,db_name:$dbname,ssl_mode:$ssl}')"
	http_call POST "${API_URL}/admin/databases" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$create_db_payload"
	assert_status_in "201,200" "create managed database '${OBS_DB_NAME}'"
	OBS_DB_ID="$(jq -r '.id' <<<"$BODY")"
	log_ok "Registered managed database '${OBS_DB_NAME}' (id=${OBS_DB_ID}, ${TARGET_DB_HOST}:${TARGET_DB_PORT}/${TARGET_DB_NAME})"
else
	log_ok "Reusing managed database '${OBS_DB_NAME}' (id=${OBS_DB_ID})"
fi

http_call POST "${API_URL}/admin/databases/${OBS_DB_ID}/test" "$ADMIN_COOKIES" "$ADMIN_CSRF"
assert_status_in "204,200" "test stored connection for '${OBS_DB_NAME}'"
log_ok "Target DB connection test passed"

# Grant view_edit permission before logging in as obs_tester (avoids session-rotation invalidation).
http_call GET "${API_URL}/admin/groups/${OBS_GROUP_ID}/databases" "$ADMIN_COOKIES"
assert_status_in "200" "GET /admin/groups/${OBS_GROUP_ID}/databases"
has_perm="$(jq -r --argjson dbid "$OBS_DB_ID" 'any(.[]; .database_id == $dbid and .permission == "view_edit")' <<<"$BODY")"
if [[ "$has_perm" != "true" ]]; then
	http_call PUT "${API_URL}/admin/groups/${OBS_GROUP_ID}/databases/${OBS_DB_ID}" \
		"$ADMIN_COOKIES" "$ADMIN_CSRF" '{"permission":"view_edit"}'
	assert_status_in "200" "grant view_edit on '${OBS_DB_NAME}' to '${OBS_GROUP_NAME}'"
fi
log_ok "Ensured 'view_edit' permission on database id=${OBS_DB_ID}"

# ---------------------------------------------------------------------------
# Phase 3: Auth Outcomes & 4xx Guard Traffic (bscheme-overview)
# ---------------------------------------------------------------------------
log_phase "Phase 3: Generating auth outcomes & 401/403 traffic"

bad_pass_payload="$(jq -cn --arg u "$ADMIN_USER" '{username:$u,password:"definitely-wrong-password"}')"
http_call POST "${API_URL}/auth/login" "" "" "$bad_pass_payload"
assert_status_in "401" "wrong password login"
log_ok "Triggered auth outcome=invalid_credentials (wrong password -> 401)"

unknown_user_payload='{"username":"obs_nonexistent_user","password":"wrong-password"}'
http_call POST "${API_URL}/auth/login" "" "" "$unknown_user_payload"
assert_status_in "401" "unknown user login"
log_ok "Triggered auth outcome=invalid_credentials (unknown user -> 401)"

disabled_login_payload="$(jq -cn --arg u "$OBS_DISABLED_USER" --arg p "$OBS_DISABLED_PASS" '{username:$u,password:$p}')"
http_call POST "${API_URL}/auth/login" "" "" "$disabled_login_payload"
assert_status_in "401" "disabled user login"
log_ok "Triggered auth outcome=user_disabled (disabled user -> 401)"

tester_login_payload="$(jq -cn --arg u "$OBS_TESTER_USER" --arg p "$OBS_TESTER_PASS" '{username:$u,password:$p}')"
http_call POST "${API_URL}/auth/login" "" "" "$tester_login_payload" "$CORR_ID" "$USER_COOKIES"
assert_status_in "200" "login as '${OBS_TESTER_USER}'"
USER_CSRF="$(jq -r '.csrf_token' <<<"$BODY")"
log_ok "Triggered auth outcome=success (user='${OBS_TESTER_USER}')"

http_call GET "${API_URL}/databases" ""
assert_status_in "401" "unauthenticated GET /databases"
log_ok "Triggered unauthenticated 401"

http_call POST "${API_URL}/databases/${OBS_DB_ID}/query" "$USER_COOKIES" "invalid-csrf-token" '{"sql":"SELECT 1"}'
assert_status_in "403" "invalid CSRF POST /query"
log_ok "Triggered CSRF rejection 403"

# ---------------------------------------------------------------------------
# Phase 4: Full Migration Pipeline (bscheme-migrations)
# ---------------------------------------------------------------------------
if [[ "$SKIP_MIGRATIONS" == "true" ]]; then
	log_phase "Phase 4: Skipped (--skip-migrations)"
else
	log_phase "Phase 4: Running migration lifecycle (DDL, batch, rollback, forward, rejected, pending)"

	# 1. Create temporary test table and seed 25 rows (will run cleanly across multiple runs via timestamped name).
	ddl_create_payload="$(jq -cn \
		--arg name "obs smoke create ${RUN_ID}" \
		--arg sql "CREATE TABLE public.${RUN_ID} (id BIGSERIAL PRIMARY KEY, label TEXT NOT NULL DEFAULT 'pending'); INSERT INTO public.${RUN_ID} (label) SELECT 'pending' FROM generate_series(1, 25);" \
		--arg down "DROP TABLE IF EXISTS public.${RUN_ID};" \
		'{name:$name,sql:$sql,down_sql:$down}')"
	run_draft_execution "DDL create+seed (up)" "$ddl_create_payload" "success"

	# 2. Batch migration (25 rows in batches of 5 -> 5 committed batches & 25 rows processed).
	batch_payload="$(jq -cn \
		--arg name "obs smoke batch ${RUN_ID}" \
		--arg sql "UPDATE public.${RUN_ID} SET label = 'done' WHERE id = ANY(\$1::bigint[])" \
		--arg down "UPDATE public.${RUN_ID} SET label = 'pending' WHERE id = ANY(\$1::bigint[])" \
		--arg tbl "$RUN_ID" \
		'{name:$name,sql:$sql,down_sql:$down,batch:{schema:"public",table:$tbl,key_column:"id",scope_sql:"",batch_size:5}}')"
	run_draft_execution "Batch update 5x5 (up)" "$batch_payload" "success"

	# 3. Rollback of the batch migration.
	http_call POST "${API_URL}/databases/${OBS_DB_ID}/migrations/rollback" \
		"$USER_COOKIES" "$USER_CSRF" "" "$MIGRATION_CORR_ID"
	assert_status_in "202,200" "submit rollback"
	rollback_exec_id="$(jq -r '.id' <<<"$BODY")"
	approve_and_enqueue_execution "Batch rollback" "$rollback_exec_id" "success"

	# 4. Forward re-application of the rolled-back batch migration.
	http_call POST "${API_URL}/databases/${OBS_DB_ID}/migrations/forward" \
		"$USER_COOKIES" "$USER_CSRF" "" "$MIGRATION_CORR_ID"
	assert_status_in "202,200" "submit forward"
	forward_exec_id="$(jq -r '.id' <<<"$BODY")"
	approve_and_enqueue_execution "Batch forward" "$forward_exec_id" "success"

	# 5. Cleanup temporary test table.
	ddl_drop_payload="$(jq -cn \
		--arg name "obs smoke drop ${RUN_ID}" \
		--arg sql "DROP TABLE IF EXISTS public.${RUN_ID};" \
		'{name:$name,sql:$sql,irreversible:true,irreversible_reason:"observability smoke test cleanup"}')"
	run_draft_execution "DDL cleanup drop (up)" "$ddl_drop_payload" "success"

	# 6. Intentional failed migration (unless --no-failures).
	if [[ "$NO_FAILURES" == "false" ]]; then
		fail_payload="$(jq -cn \
			--arg name "obs smoke intentional failure ${RUN_ID}" \
			--arg sql "ALTER TABLE public.${RUN_ID}_missing ADD COLUMN x INT;" \
			'{name:$name,sql:$sql,irreversible:true,irreversible_reason:"intentional observability failure test"}')"
		run_draft_execution "Intentional SQL failure (failed)" "$fail_payload" "failed"
	else
		log_ok "Skipped intentional failed migration (--no-failures)"
	fi

	# 7. Rejected execution (exercises pending_review -> rejected gauge).
	reject_payload="$(jq -cn \
		--arg name "obs smoke reject ${RUN_ID}" \
		'{name:$name,sql:"SELECT 1;",irreversible:true,irreversible_reason:"observability reject test"}')"
	http_call POST "${API_URL}/databases/${OBS_DB_ID}/migrations/drafts" \
		"$USER_COOKIES" "$USER_CSRF" "$reject_payload" "$MIGRATION_CORR_ID"
	assert_status_in "201,200" "create reject draft"
	reject_draft_id="$(jq -r '.id' <<<"$BODY")"
	http_call POST "${API_URL}/migrations/drafts/${reject_draft_id}/execute" \
		"$USER_COOKIES" "$USER_CSRF" "" "$MIGRATION_CORR_ID"
	assert_status_in "202,200" "submit reject draft"
	reject_exec_id="$(jq -r '.id' <<<"$BODY")"
	http_call POST "${API_URL}/migrations/executions/${reject_exec_id}/reject" \
		"$USER_COOKIES" "$USER_CSRF" '{"note":"observability test rejection"}' "$MIGRATION_CORR_ID"
	assert_status_in "200" "reject execution #${reject_exec_id}"
	log_ok "Rejected execution #${reject_exec_id}"

	# 8. Pending review execution (leaves >= 1 pending_review item for the Pending Review stat panel).
	pending_payload="$(jq -cn \
		--arg name "obs smoke pending ${RUN_ID}" \
		'{name:$name,sql:"SELECT 1;",irreversible:true,irreversible_reason:"observability pending review test"}')"
	http_call POST "${API_URL}/databases/${OBS_DB_ID}/migrations/drafts" \
		"$USER_COOKIES" "$USER_CSRF" "$pending_payload" "$MIGRATION_CORR_ID"
	assert_status_in "201,200" "create pending draft"
	pending_draft_id="$(jq -r '.id' <<<"$BODY")"
	http_call POST "${API_URL}/migrations/drafts/${pending_draft_id}/execute" \
		"$USER_COOKIES" "$USER_CSRF" "" "$MIGRATION_CORR_ID"
	assert_status_in "202,200" "submit pending draft"
	pending_exec_id="$(jq -r '.id' <<<"$BODY")"
	log_ok "Left execution #${pending_exec_id} in pending_review"
fi

# ---------------------------------------------------------------------------
# Phase 5: Multi-Wave Viewer, Query, Explain & Concurrency Burst
# ---------------------------------------------------------------------------
log_phase "Phase 5: Running ${WAVES} traffic wave(s) (concurrency=${CONCURRENCY}, interval=${INTERVAL}s)"

for ((wave = 1; wave <= WAVES; wave++)); do
	printf '  -- Wave %d/%d --\n' "$wave" "$WAVES"

	http_call GET "${API_URL}/databases" "$USER_COOKIES"
	assert_status_in "200" "GET /databases"

	http_call GET "${API_URL}/databases/${OBS_DB_ID}/schemas" "$USER_COOKIES"
	assert_status_in "200" "GET /schemas"

	http_call GET "${API_URL}/databases/${OBS_DB_ID}/tables?schema=public" "$USER_COOKIES"
	assert_status_in "200" "GET /tables?schema=public"
	FIRST_TABLE="$(jq -r '.[0].name // empty' <<<"$BODY")"

	if [[ -n "$FIRST_TABLE" ]]; then
		http_call GET "${API_URL}/databases/${OBS_DB_ID}/table-structure?schema=public&table=${FIRST_TABLE}" "$USER_COOKIES"
		assert_status_in "200" "GET /table-structure (${FIRST_TABLE})"

		http_call GET "${API_URL}/databases/${OBS_DB_ID}/table-rows?schema=public&table=${FIRST_TABLE}&page=1&page_size=20" "$USER_COOKIES"
		assert_status_in "200" "GET /table-rows (${FIRST_TABLE})"

		tbl_query="$(jq -cn --arg sql "SELECT * FROM \"public\".\"${FIRST_TABLE}\"" '{sql:$sql,page:1,page_size:20}')"
		http_call POST "${API_URL}/databases/${OBS_DB_ID}/query" "$USER_COOKIES" "$USER_CSRF" "$tbl_query"
		assert_status_in "200" "POST /query (${FIRST_TABLE})"

		tbl_explain="$(jq -cn --arg sql "SELECT * FROM \"public\".\"${FIRST_TABLE}\" LIMIT 10" '{sql:$sql}')"
		http_call POST "${API_URL}/databases/${OBS_DB_ID}/migrations/explain" "$USER_COOKIES" "$USER_CSRF" "$tbl_explain"
		assert_status_in "200" "POST /migrations/explain (${FIRST_TABLE})"
	else
		http_call POST "${API_URL}/databases/${OBS_DB_ID}/migrations/explain" "$USER_COOKIES" "$USER_CSRF" '{"sql":"SELECT 1"}'
		assert_status_in "200" "POST /migrations/explain (SELECT 1)"
	fi

	http_call POST "${API_URL}/databases/${OBS_DB_ID}/query" "$USER_COOKIES" "$USER_CSRF" \
		'{"sql":"SELECT 1 AS ok, current_database() AS db, NOW() AS ts","page":1,"page_size":10}'
	assert_status_in "200" "POST /query (health select)"

	http_call GET "${API_URL}/databases/${OBS_DB_ID}/migrations/history?page=1&page_size=50&hide_rollback=false" "$USER_COOKIES"
	assert_status_in "200" "GET /migrations/history"
	http_call GET "${API_URL}/databases/${OBS_DB_ID}/migrations/executions" "$USER_COOKIES"
	assert_status_in "200" "GET /migrations/executions"
	http_call GET "${API_URL}/migrations/approvals/pending" "$USER_COOKIES"
	assert_status_in "200" "GET /migrations/approvals/pending"

	# Viewer errors & 400/404 coverage.
	http_call GET "${API_URL}/databases/${OBS_DB_ID}/tables?schema=obs_missing_schema" "$USER_COOKIES"
	assert_status_in "404" "list_tables missing schema (404)"
	http_call GET "${API_URL}/databases/${OBS_DB_ID}/table-structure?schema=public&table=obs_missing_table" "$USER_COOKIES"
	assert_status_in "404" "describe_table missing table (404)"
	http_call GET "${API_URL}/databases/${OBS_DB_ID}/table-rows?schema=public&table=obs_missing_table" "$USER_COOKIES"
	assert_status_in "404" "table_data missing table (404)"
	http_call POST "${API_URL}/databases/${OBS_DB_ID}/query" "$USER_COOKIES" "$USER_CSRF" '{"sql":"DELETE FROM pg_class","page":1,"page_size":10}'
	assert_status_in "400" "read-only query guard (400)"
	http_call POST "${API_URL}/databases/${OBS_DB_ID}/query" "$USER_COOKIES" "$USER_CSRF" '{"sql":"SELECT * FROM public.obs_missing_table_xyz","page":1,"page_size":10}'
	assert_status_in "400" "target DB query syntax/missing error (400)"

	# Wave auth bumps so Grafana rate graphs reflect active auth traffic.
	http_call POST "${API_URL}/auth/login" "" "" "$bad_pass_payload"
	assert_status_in "401" "wave invalid_credentials"
	http_call POST "${API_URL}/auth/login" "" "" "$disabled_login_payload"
	assert_status_in "401" "wave user_disabled"

	# Parallel burst for HTTP in-flight, latency percentiles, and DB pool utilization.
	pids=()
	for ((i = 1; i <= CONCURRENCY; i++)); do
		case $((i % 4)) in
			0)
				curl -fsS -b "$USER_COOKIES" -H "X-Correlation-ID: ${CORR_ID}" \
					"${API_URL}/databases/${OBS_DB_ID}/schemas" >/dev/null &
				;;
			1)
				curl -fsS -b "$USER_COOKIES" -H "X-Correlation-ID: ${CORR_ID}" \
					-H "X-CSRF-Token: ${USER_CSRF}" -H "Content-Type: application/json" \
					-d '{"sql":"SELECT pg_sleep(0.05)::text AS slept, 1 AS ok","page":1,"page_size":10}' \
					"${API_URL}/databases/${OBS_DB_ID}/query" >/dev/null &
				;;
			2)
				curl -fsS -b "$USER_COOKIES" -H "X-Correlation-ID: ${CORR_ID}" \
					"${API_URL}/databases/${OBS_DB_ID}/tables?schema=public" >/dev/null &
				;;
			3)
				curl -fsS -b "$USER_COOKIES" -H "X-Correlation-ID: ${CORR_ID}" \
					"${API_URL}/databases/${OBS_DB_ID}/migrations/history?page=1&page_size=20&hide_rollback=false" >/dev/null &
				;;
		esac
		pids+=("$!")
	done
	burst_failures=0
	for pid in "${pids[@]}"; do
		if ! wait "$pid"; then
			burst_failures=$((burst_failures + 1))
		fi
	done
	if [[ "$burst_failures" -gt 0 ]]; then
		log_warn "Wave ${wave}: ${burst_failures}/${CONCURRENCY} burst request(s) failed"
	else
		log_ok "Wave ${wave}: completed sequential + ${CONCURRENCY} parallel requests"
	fi

	if [[ "$wave" -lt "$WAVES" && "$INTERVAL" -gt 0 ]]; then
		sleep "$INTERVAL"
	fi
done

if [[ "$NO_VERIFY" == "false" && "$SCRAPE_WAIT" -gt 0 ]]; then
	log_phase "Waiting ${SCRAPE_WAIT}s for Prometheus scrape & OTel collector flush..."
	sleep "$SCRAPE_WAIT"
fi

# ---------------------------------------------------------------------------
# Phase 6: Verification Report (Prometheus, Loki, Tempo)
# ---------------------------------------------------------------------------
if [[ "$NO_VERIFY" == "true" ]]; then
	log_phase "Phase 6: Verification skipped (--no-verify)"
else
	log_phase "Phase 6: Verifying Prometheus, Loki & Tempo ingestion"

	if [[ "$PROM_UP" == "true" ]]; then
		targets_json="$(curl -fsS "${PROMETHEUS_URL}/api/v1/targets")"
		printf '  Prometheus scrape targets:\n'
		jq -r '.data.activeTargets[]
			| select(.labels.job | test("^(bscheme-api|bscheme-worker|rabbitmq|otel-collector)$"))
			| "    - \(.labels.job) (\(.scrapeUrl)): \(.health)"' <<<"$targets_json"

		prom_scalar() {
			local expr="$1"
			curl -fsS -G --data-urlencode "query=${expr}" "${PROMETHEUS_URL}/api/v1/query" \
				| jq -r '.data.result[0].value[1] // "0"'
		}

		printf '  Metric counters seen by Prometheus:\n'
		printf '    - sum(bscheme_http_requests_total): %s\n' "$(prom_scalar 'sum(bscheme_http_requests_total)')"
		printf '    - max(bscheme_targetdb_pools_active): %s\n' "$(prom_scalar 'max(bscheme_targetdb_pools_active)')"
		printf '    - bscheme_auth_logins_total:\n'
		curl -fsS -G --data-urlencode 'query=sum by (outcome) (bscheme_auth_logins_total)' "${PROMETHEUS_URL}/api/v1/query" \
			| jq -r '.data.result[] | "        \(.metric.outcome): \(.value[1])"'
		printf '    - bscheme_viewer_operations_total:\n'
		curl -fsS -G --data-urlencode 'query=sum by (operation, status) (bscheme_viewer_operations_total)' "${PROMETHEUS_URL}/api/v1/query" \
			| jq -r '.data.result[] | "        \(.metric.operation) [\(.metric.status)]: \(.value[1])"'
		if [[ "$SKIP_MIGRATIONS" == "false" ]]; then
			printf '    - bscheme_migration_executions_total:\n'
			curl -fsS -G --data-urlencode 'query=sum by (type, status) (bscheme_migration_executions_total)' "${PROMETHEUS_URL}/api/v1/query" \
				| jq -r '.data.result[] | "        \(.metric.type) [\(.metric.status)]: \(.value[1])"'
			printf '    - sum(bscheme_migration_batches_processed_total): %s\n' "$(prom_scalar 'sum(bscheme_migration_batches_processed_total)')"
			printf '    - sum(bscheme_migration_batch_rows_processed_total): %s\n' "$(prom_scalar 'sum(bscheme_migration_batch_rows_processed_total)')"
		fi
		printf '    - sum by (db_role) (bscheme_db_queries_total):\n'
		curl -fsS -G --data-urlencode 'query=sum by (db_role) (bscheme_db_queries_total)' "${PROMETHEUS_URL}/api/v1/query" \
			| jq -r '.data.result[] | "        \(.metric.db_role): \(.value[1])"'
	else
		log_warn "Skipping Prometheus checks (Prometheus unreachable)"
	fi

	TRACE_ID=""
	if [[ "$LOKI_UP" == "true" ]]; then
		start_ns="$((START_SEC - 60))000000000"
		end_ns="$(($(date +%s) + 60))000000000"

		loki_query() {
			local logql="$1"
			curl -fsS -G \
				--data-urlencode "query=${logql}" \
				--data-urlencode "start=${start_ns}" \
				--data-urlencode "end=${end_ns}" \
				--data-urlencode "limit=200" \
				"${LOKI_URL}/loki/api/v1/query_range"
		}

		api_loki="$(loki_query "{service_name=~\"bscheme-api|bscheme-worker\"} | correlation_id = \"${CORR_ID}\"")"
		api_lines="$(jq '[.data.result[].values[]] | length' <<<"$api_loki")"
		log_ok "Loki logs for correlation_id='${CORR_ID}': ${api_lines} line(s)"

		if [[ "$SKIP_MIGRATIONS" == "false" ]]; then
			mig_loki="$(loki_query "{service_name=~\"bscheme-api|bscheme-worker\"} | correlation_id = \"${MIGRATION_CORR_ID}\"")"
			mig_lines="$(jq '[.data.result[].values[]] | length' <<<"$mig_loki")"
			mig_services="$(jq -r '[.data.result[].stream.service_name] | unique | join(", ")' <<<"$mig_loki")"
			log_ok "Loki logs for correlation_id='${MIGRATION_CORR_ID}': ${mig_lines} line(s) across services [${mig_services:-none}]"
			TRACE_ID="$(jq -r '
				[ .data.result[]
				  | (.stream.trace_id // .stream.traceid // (try (.values[0][1] | fromjson | .trace_id) catch empty))
				  | select(. != null and . != "")
				][0] // empty' <<<"$mig_loki")"
		fi

		if [[ -z "$TRACE_ID" ]]; then
			TRACE_ID="$(jq -r '
				[ .data.result[]
				  | (.stream.trace_id // .stream.traceid // (try (.values[0][1] | fromjson | .trace_id) catch empty))
				  | select(. != null and . != "")
				][0] // empty' <<<"$api_loki")"
		fi
	else
		log_warn "Skipping Loki checks (Loki unreachable)"
	fi

	if [[ "$TEMPO_UP" == "true" ]]; then
		if [[ -n "$TRACE_ID" ]]; then
			if tempo_trace="$(curl -fsS "${TEMPO_URL}/api/traces/${TRACE_ID}" 2>/dev/null)"; then
				span_count="$(jq '[.batches[]?.scopeSpans[]?.spans[]?] | length' <<<"$tempo_trace")"
				svc_list="$(jq -r '[.batches[]?.resource.attributes[]? | select(.key=="service.name") | .value.stringValue] | unique | join(", ")' <<<"$tempo_trace")"
				log_ok "Tempo trace ${TRACE_ID}: ${span_count} span(s) across services [${svc_list:-unknown}]"
			else
				log_warn "Tempo trace ${TRACE_ID} not yet queryable (may still be flushing)"
			fi
		else
			log_warn "No trace_id extracted from Loki yet to probe Tempo"
		fi
	else
		log_warn "Skipping Tempo checks (Tempo unreachable)"
	fi
fi

# ---------------------------------------------------------------------------
# Summary Links
# ---------------------------------------------------------------------------
log_phase "Done — Open Grafana & Inspect Your Boards"
cat <<EOF
  Grafana Overview Dashboard : ${GRAFANA_URL}/d/bscheme-overview
  Grafana Migrations Board   : ${GRAFANA_URL}/d/bscheme-migrations
  Prometheus Targets         : ${PROMETHEUS_URL}/targets
  Prometheus Alerts          : ${PROMETHEUS_URL}/alerts

  LogQL queries (Grafana -> Explore -> Loki):
    {service_name=~"bscheme-api|bscheme-worker"} | correlation_id = "${CORR_ID}"
EOF
if [[ "$SKIP_MIGRATIONS" == "false" ]]; then
	printf '    {service_name=~"bscheme-api|bscheme-worker"} | correlation_id = "%s"\n' "$MIGRATION_CORR_ID"
fi
if [[ -n "${TRACE_ID:-}" ]]; then
	printf '\n  Sample Tempo Trace ID      : %s\n' "$TRACE_ID"
fi
