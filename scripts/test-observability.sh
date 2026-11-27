#!/usr/bin/env bash
# End-to-end smoke test to verify that Grafana, Prometheus, Loki, and Tempo
# receive metrics, logs, and traces from bscheme-api and bscheme-worker.
#
# Usage:
#   bash scripts/test-observability.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${REPO_ROOT}/.env"

# Load scalar key from .env if present; fallback to Compose defaults.
env_or_default() {
	local key="$1" default="$2" val=""
	if [[ -f "$ENV_FILE" ]]; then
		val="$(awk -F= -v k="$key" '$0 !~ /^[[:space:]]*#/ && $1 == k { sub(/^[^=]*=/, ""); v=$0 } END { print v }' "$ENV_FILE" | tr -d '\r"'\''')"
	fi
	printf '%s' "${!key:-${val:-$default}}"
}

API_URL="${API_URL:-http://localhost:$(env_or_default APP_PORT 8080)}"
GRAFANA_URL="${GRAFANA_URL:-http://localhost:$(env_or_default GRAFANA_PORT 3000)}"
PROM_URL="${PROM_URL:-http://localhost:$(env_or_default PROMETHEUS_PORT 9090)}"
LOKI_URL="${LOKI_URL:-http://localhost:$(env_or_default LOKI_PORT 3100)}"
TEMPO_URL="${TEMPO_URL:-http://localhost:$(env_or_default TEMPO_PORT 3200)}"

ADMIN_USER="$(env_or_default APP_ADMIN_USERNAME admin)"
ADMIN_PASS="$(env_or_default APP_ADMIN_PASSWORD password)"
GRAFANA_USER="$(env_or_default GRAFANA_ADMIN_USER admin)"
GRAFANA_PASS="$(env_or_default GRAFANA_ADMIN_PASSWORD admin)"

TARGET_HOST="${TARGET_DB_HOST:-test-db}"
TARGET_PORT="${TARGET_DB_PORT:-5432}"
TARGET_USER="$(env_or_default TEST_DB_USER target)"
TARGET_PASS="$(env_or_default TEST_DB_PASSWORD target_password)"
TARGET_DB="$(env_or_default TEST_DB_NAME targetdb)"

RUN_ID="obs_$(date +%s)"
CORR_ID="obs-test-${RUN_ID}"
TMPDIR_RUN="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_RUN"' EXIT
ADMIN_COOKIES="${TMPDIR_RUN}/admin.cookies"
USER_COOKIES="${TMPDIR_RUN}/user.cookies"

step() { printf '\n\033[1;34m==> %s\033[0m\n' "$1"; }
ok()   { printf '  \033[0;32m[OK]\033[0m %s\n' "$1"; }
fail() { printf '  \033[0;31m[FAIL]\033[0m %s\n' "$1" >&2; exit 1; }

# Helper: call backend API and return JSON body (fails on HTTP >= 400 unless expected).
api() {
	local method="$1" path="$2" cookies="${3:-}" csrf="${4:-}" body="${5:-}" save_cookies="${6:-}"
	local -a args=(-fsS -X "$method" -H "X-Correlation-ID: ${CORR_ID}")
	[[ -n "$cookies" ]]      && args+=(-b "$cookies")
	[[ -n "$save_cookies" ]] && args+=(-c "$save_cookies")
	[[ -n "$csrf" ]]         && args+=(-H "X-CSRF-Token: ${csrf}")
	[[ -n "$body" ]]         && args+=(-H "Content-Type: application/json" -d "$body")
	curl "${args[@]}" "${API_URL}${path}"
}

# Helper: create draft -> submit -> approve -> enqueue -> poll until terminal status.
run_migration() {
	local label="$1" payload="$2" expected="${3:-success}"
	local draft_id exec_id
	draft_id="$(api POST "/databases/${DB_ID}/migrations/drafts" "$USER_COOKIES" "$USER_CSRF" "$payload" | jq -r '.id')"
	exec_id="$(api POST "/migrations/drafts/${draft_id}/execute" "$USER_COOKIES" "$USER_CSRF" "" | jq -r '.id')"
	approve_and_wait "$label" "$exec_id" "$expected"
}

approve_and_wait() {
	local label="$1" exec_id="$2" expected="${3:-success}"
	api POST "/migrations/executions/${exec_id}/approve" "$USER_COOKIES" "$USER_CSRF" '{"note":"observability test"}' >/dev/null
	api POST "/migrations/executions/${exec_id}/enqueue" "$USER_COOKIES" "$USER_CSRF" "" >/dev/null
	for _ in $(seq 1 45); do
		local st
		st="$(api GET "/migrations/executions/${exec_id}" "$USER_COOKIES" | jq -r '.status')"
		case "$st" in
			success|failed|partial|compensated|rejected)
				[[ "$st" == "$expected" ]] && { ok "${label}: execution #${exec_id} -> ${st}"; return; }
				fail "${label}: expected '${expected}', got '${st}'"
				;;
		esac
		sleep 1
	done
	fail "${label}: timed out waiting for worker"
}

# ---------------------------------------------------------------------------
# 1. Check service health (API + LGTM stack)
# ---------------------------------------------------------------------------
step "1/5 Checking bscheme-api and LGTM stack health"
for cmd in curl jq; do command -v "$cmd" >/dev/null || fail "'$cmd' is required"; done

curl -fsS --max-time 3 "${API_URL}/metrics" >/dev/null    || fail "bscheme-api unreachable at ${API_URL}"
curl -fsS --max-time 3 "${GRAFANA_URL}/api/health" >/dev/null || fail "Grafana unreachable at ${GRAFANA_URL}"
curl -fsS --max-time 3 "${PROM_URL}/-/ready" >/dev/null       || fail "Prometheus unreachable at ${PROM_URL}"
curl -fsS --max-time 3 "${LOKI_URL}/ready" >/dev/null         || fail "Loki unreachable at ${LOKI_URL}"
curl -fsS --max-time 3 "${TEMPO_URL}/ready" >/dev/null        || fail "Tempo unreachable at ${TEMPO_URL}"
ok "All services reachable (API, Grafana, Prometheus, Loki, Tempo)"

# ---------------------------------------------------------------------------
# 2. Bootstrap idempotent test user, group leader, and target database
# ---------------------------------------------------------------------------
step "2/5 Bootstrapping test user, group, and target database"
ADMIN_CSRF="$(api POST "/auth/login" "" "" "$(jq -cn --arg u "$ADMIN_USER" --arg p "$ADMIN_PASS" '{username:$u,password:$p}')" "$ADMIN_COOKIES" | jq -r '.csrf_token')"

TEST_USER="obs_tester"; TEST_PASS="obs-tester-pass-123!"
USER_ID="$(api GET "/admin/users" "$ADMIN_COOKIES" | jq -r --arg u "$TEST_USER" '.[] | select(.username==$u) | .id' | head -n1)"
if [[ -z "$USER_ID" ]]; then
	USER_ID="$(api POST "/admin/users" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$(jq -cn --arg u "$TEST_USER" --arg p "$TEST_PASS" '{username:$u,password:$p}')" | jq -r '.id')"
else
	api PATCH "/admin/users/${USER_ID}" "$ADMIN_COOKIES" "$ADMIN_CSRF" '{"disabled":false}' >/dev/null
fi

GROUP_ID="$(api GET "/admin/groups" "$ADMIN_COOKIES" | jq -r '.[] | select(.name=="obs-smoke-team") | .id' | head -n1)"
[[ -z "$GROUP_ID" ]] && GROUP_ID="$(api POST "/admin/groups" "$ADMIN_COOKIES" "$ADMIN_CSRF" '{"name":"obs-smoke-team"}' | jq -r '.id')"
if [[ "$(api GET "/admin/groups/${GROUP_ID}/members" "$ADMIN_COOKIES" | jq -r --argjson id "$USER_ID" 'any(.[]; .id==$id)')" != "true" ]]; then
	api POST "/admin/groups/${GROUP_ID}/members" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$(jq -cn --argjson id "$USER_ID" '{user_id:$id}')" >/dev/null
fi
api PUT "/admin/groups/${GROUP_ID}/leader" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$(jq -cn --argjson id "$USER_ID" '{user_id:$id}')" >/dev/null

DB_ID="$(api GET "/admin/databases" "$ADMIN_COOKIES" | jq -r '.[] | select(.name=="obs-smoke-target") | .id' | head -n1)"
if [[ -z "$DB_ID" ]]; then
	DB_ID="$(api POST "/admin/databases" "$ADMIN_COOKIES" "$ADMIN_CSRF" "$(jq -cn \
		--arg host "$TARGET_HOST" --argjson port "$TARGET_PORT" --arg u "$TARGET_USER" --arg p "$TARGET_PASS" --arg db "$TARGET_DB" \
		'{name:"obs-smoke-target",host:$host,port:$port,username:$u,password:$p,db_name:$db,ssl_mode:"disable"}')" | jq -r '.id')"
fi
api PUT "/admin/groups/${GROUP_ID}/databases/${DB_ID}" "$ADMIN_COOKIES" "$ADMIN_CSRF" '{"permission":"view_edit"}' >/dev/null

USER_CSRF="$(api POST "/auth/login" "" "" "$(jq -cn --arg u "$TEST_USER" --arg p "$TEST_PASS" '{username:$u,password:$p}')" "$USER_COOKIES" | jq -r '.csrf_token')"
ok "Ready as '${TEST_USER}' on target DB #${DB_ID}"

# ---------------------------------------------------------------------------
# 3. Exercise Queue & Migration pipeline (success, batch, rollback, forward, fail, reject, pending)
# ---------------------------------------------------------------------------
step "3/5 Running migration scenarios (DDL, batch 4x5, rollback, forward, failed, rejected, pending)"
START_NS="$(( $(date +%s) - 5 ))000000000"

run_migration "DDL create+seed" "$(jq -cn --arg n "obs create ${RUN_ID}" \
	--arg sql "CREATE TABLE public.${RUN_ID} (id BIGSERIAL PRIMARY KEY, label TEXT); INSERT INTO public.${RUN_ID} (label) SELECT 'pending' FROM generate_series(1,20);" \
	--arg down "DROP TABLE IF EXISTS public.${RUN_ID};" '{name:$n,sql:$sql,down_sql:$down}')" "success"

run_migration "Batch update 4x5" "$(jq -cn --arg n "obs batch ${RUN_ID}" --arg tbl "$RUN_ID" \
	'{name:$n,sql:("UPDATE public."+$tbl+" SET label = '\''done'\'' WHERE id = ANY($1::bigint[])"),down_sql:("UPDATE public."+$tbl+" SET label = '\''pending'\'' WHERE id = ANY($1::bigint[])"),batch:{schema:"public",table:$tbl,key_column:"id",scope_sql:"",batch_size:5}}')" "success"

rb_id="$(api POST "/databases/${DB_ID}/migrations/rollback" "$USER_COOKIES" "$USER_CSRF" "" | jq -r '.id')"
approve_and_wait "Rollback" "$rb_id" "success"

fw_id="$(api POST "/databases/${DB_ID}/migrations/forward" "$USER_COOKIES" "$USER_CSRF" "" | jq -r '.id')"
approve_and_wait "Forward" "$fw_id" "success"

run_migration "Cleanup drop" "$(jq -cn --arg n "obs drop ${RUN_ID}" --arg sql "DROP TABLE IF EXISTS public.${RUN_ID};" \
	'{name:$n,sql:$sql,irreversible:true,irreversible_reason:"cleanup"}')" "success"

run_migration "Intentional failure" "$(jq -cn --arg n "obs fail ${RUN_ID}" --arg sql "ALTER TABLE public.${RUN_ID}_missing ADD COLUMN x INT;" \
	'{name:$n,sql:$sql,irreversible:true,irreversible_reason:"failure test"}')" "failed"

# Leave one rejected and one pending_review execution for status gauge coverage.
rej_draft="$(api POST "/databases/${DB_ID}/migrations/drafts" "$USER_COOKIES" "$USER_CSRF" "$(jq -cn --arg n "obs reject ${RUN_ID}" '{name:$n,sql:"SELECT 1;",irreversible:true,irreversible_reason:"test"}')" | jq -r '.id')"
rej_exec="$(api POST "/migrations/drafts/${rej_draft}/execute" "$USER_COOKIES" "$USER_CSRF" "" | jq -r '.id')"
api POST "/migrations/executions/${rej_exec}/reject" "$USER_COOKIES" "$USER_CSRF" '{"note":"observability test"}' >/dev/null
ok "Rejected: execution #${rej_exec} -> rejected"

pend_draft="$(api POST "/databases/${DB_ID}/migrations/drafts" "$USER_COOKIES" "$USER_CSRF" "$(jq -cn --arg n "obs pending ${RUN_ID}" '{name:$n,sql:"SELECT 1;",irreversible:true,irreversible_reason:"test"}')" | jq -r '.id')"
pend_exec="$(api POST "/migrations/drafts/${pend_draft}/execute" "$USER_COOKIES" "$USER_CSRF" "" | jq -r '.id')"
ok "Pending review: execution #${pend_exec} -> pending_review"

# ---------------------------------------------------------------------------
# 4. Trigger DB Connection Pool activity & wait time (15 concurrent > MaxConns=10)
# ---------------------------------------------------------------------------
step "4/5 Generating concurrent DB connection pool load (15 parallel queries)"
pids=()
for _ in $(seq 1 15); do
	api POST "/databases/${DB_ID}/query" "$USER_COOKIES" "$USER_CSRF" \
		'{"sql":"SELECT pg_sleep(0.1)::text AS slept","page":1,"page_size":10}' >/dev/null &
	pids+=("$!")
done
for pid in "${pids[@]}"; do wait "$pid"; done
ok "Completed 15 concurrent queries"

# ---------------------------------------------------------------------------
# 5. Verify Grafana Dashboard, Prometheus Metrics, Loki Logs & Tempo Traces
# ---------------------------------------------------------------------------
step "5/5 Waiting 6s for scrape/flush, then verifying Grafana, Prometheus, Loki & Tempo"
sleep 6

# Verify provisioned Grafana dashboard exists
dash_title="$(curl -fsS -u "${GRAFANA_USER}:${GRAFANA_PASS}" "${GRAFANA_URL}/api/dashboards/uid/bscheme-migrations" | jq -r '.dashboard.title // empty')"
[[ -n "$dash_title" ]] || fail "Grafana dashboard 'bscheme-migrations' not provisioned"
ok "Grafana dashboard provisioned: ${dash_title}"

# Verify Prometheus scraped both targets and recorded metrics
prom_val() { curl -fsS -G --data-urlencode "query=$1" "${PROM_URL}/api/v1/query" | jq -r '.data.result[0].value[1] // "0"'; }
api_up="$(prom_val 'sum(up{job="bscheme-api"})')"
wrk_up="$(prom_val 'sum(up{job="bscheme-worker"})')"
[[ "${api_up:-0}" -ge 1 && "${wrk_up:-0}" -ge 1 ]] || fail "Prometheus targets not up (api=${api_up:-0}, worker=${wrk_up:-0})"
batches="$(prom_val 'sum(bscheme_migration_batches_processed_total)')"
rows="$(prom_val 'sum(bscheme_migration_batch_rows_processed_total)')"
waits="$(prom_val 'sum(bscheme_db_pool_wait_count_total{db_role="target"})')"
active_peak="$(prom_val 'max_over_time(bscheme_db_pool_connections{state="active"}[5m])')"
ok "Prometheus targets UP | batches=${batches}, rows=${rows}, pool_waits=${waits}, peak_active_conns=${active_peak}"

# Verify Loki received correlated logs and extract a trace_id for Tempo
END_NS="$(( $(date +%s) + 10 ))000000000"
loki_json="$(curl -fsS -G \
	--data-urlencode "query={service_name=~\"bscheme-api|bscheme-worker\"} | correlation_id = \"${CORR_ID}\"" \
	--data-urlencode "start=${START_NS}" --data-urlencode "end=${END_NS}" --data-urlencode "limit=50" \
	"${LOKI_URL}/loki/api/v1/query_range")"
log_count="$(jq '[.data.result[].values[]] | length' <<<"$loki_json")"
[[ "$log_count" -gt 0 ]] || fail "No Loki logs found for correlation_id=${CORR_ID}"
TRACE_ID="$(jq -r '[.data.result[] | (.stream.trace_id // .stream.traceid // (try (.values[0][1] | fromjson | .trace_id) catch empty)) | select(. != null and . != "")][0] // empty' <<<"$loki_json")"
ok "Loki ingested ${log_count} log(s) for correlation_id=${CORR_ID}"

# Verify Tempo has the correlated trace
[[ -n "$TRACE_ID" ]] || fail "Could not extract trace_id from Loki logs"
span_count="$(curl -fsS "${TEMPO_URL}/api/traces/${TRACE_ID}" | jq '[.batches[]?.scopeSpans[]?.spans[]?] | length')"
[[ "$span_count" -gt 0 ]] || fail "Trace ${TRACE_ID} has 0 spans in Tempo"
ok "Tempo trace ${TRACE_ID} verified (${span_count} span(s))"

printf '\n\033[1;32mPASS\033[0m — Open Grafana: %s/d/bscheme-migrations\n' "$GRAFANA_URL"
