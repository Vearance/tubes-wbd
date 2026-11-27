#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

usage() {
	cat <<'EOF'
Usage: bash scripts/bench-batch.sh [options]

Options:
  --rows N          Rows to seed (default: 1000000)
  --batch-size N    Rows per batch, 1-10000 (default: 5000)
  --skip-seed       Reuse the existing bench database instead of recreating it
  --timeout SEC     Max seconds to wait for each execution (default: 1800)
  -h, --help        Show this help

Environment overrides (otherwise read from .env or defaults):
  API_URL           (default: http://localhost:${APP_PORT:-8080})
  BENCH_DB          Physical database created inside test-db (default: benchdb)
  TARGET_DB_HOST    Host the backend uses to reach test-db (default: test-db;
                    use localhost when the backend runs outside Docker)
  TARGET_DB_PORT    (default: 5432; use ${TEST_DB_HOST_PORT} outside Docker)
  BENCH_USER / BENCH_PASS / BENCH_GROUP / BENCH_DB_NAME
EOF
}

ROWS=1000000
BATCH_SIZE=5000
SKIP_SEED=false
TIMEOUT=1800

while [[ $# -gt 0 ]]; do
	case "$1" in
		--rows) ROWS="${2:?--rows requires a value}"; shift 2 ;;
		--batch-size) BATCH_SIZE="${2:?--batch-size requires a value}"; shift 2 ;;
		--skip-seed) SKIP_SEED=true; shift ;;
		--timeout) TIMEOUT="${2:?--timeout requires a value}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) echo "error: unknown option '$1'" >&2; usage >&2; exit 2 ;;
	esac
done

for n in "$ROWS" "$BATCH_SIZE" "$TIMEOUT"; do
	[[ "$n" =~ ^[1-9][0-9]*$ ]] || { echo "error: '$n' is not a positive integer" >&2; exit 2; }
done
(( BATCH_SIZE <= 10000 )) || { echo "error: --batch-size must be at most 10000" >&2; exit 2; }

for cmd in curl jq docker date; do
	command -v "$cmd" >/dev/null 2>&1 || { echo "error: required command '$cmd' is not installed" >&2; exit 1; }
done

read_dotenv_key() {
	local key="$1" default="$2" val=""
	if [[ -f .env ]]; then
		val="$(grep -E "^${key}=" .env | tail -n 1 | cut -d= -f2- | tr -d '\r' | sed -e 's/^"//' -e 's/"$//' || true)"
	fi
	printf '%s' "${val:-$default}"
}

APP_PORT="${APP_PORT:-$(read_dotenv_key APP_PORT 8080)}"
API_URL="${API_URL:-http://localhost:${APP_PORT}}"
ADMIN_USER="${APP_ADMIN_USERNAME:-$(read_dotenv_key APP_ADMIN_USERNAME admin)}"
ADMIN_PASS="${APP_ADMIN_PASSWORD:-$(read_dotenv_key APP_ADMIN_PASSWORD password)}"
DB_USER="${TEST_DB_USER:-$(read_dotenv_key TEST_DB_USER target)}"
DB_PASS="${TEST_DB_PASSWORD:-$(read_dotenv_key TEST_DB_PASSWORD target_password)}"
BENCH_DB="${BENCH_DB:-benchdb}"
TARGET_DB_HOST="${TARGET_DB_HOST:-test-db}"
TARGET_DB_PORT="${TARGET_DB_PORT:-5432}"
BENCH_USER="${BENCH_USER:-bench_runner}"
BENCH_PASS="${BENCH_PASS:-bench-password-123}"
BENCH_GROUP="${BENCH_GROUP:-bench-team}"
BENCH_DB_NAME="${BENCH_DB_NAME:-bench-target}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
ADMIN_JAR="$WORK/admin.cookies"
USER_JAR="$WORK/user.cookies"
ANON_JAR="$WORK/anon.cookies"
: >"$ANON_JAR"

step() { printf '\n==> %s\n' "$1"; }
info() { printf '  %s\n' "$1"; }
die() { printf '  [FAIL] %s\n' "$1" >&2; exit 1; }

STATUS=""
BODY=""
api() {
	local method="$1" path="$2" jar="$3" csrf="${4:-}" body="${5:-}"
	local args=(-sS -o "$WORK/body" -w '%{http_code}' -X "$method" -b "$jar" -c "$jar" -H 'Accept: application/json')
	[[ -n "$csrf" ]] && args+=(-H "X-CSRF-Token: $csrf")
	[[ -n "$body" ]] && args+=(-H 'Content-Type: application/json' --data "$body")
	STATUS="$(curl "${args[@]}" "${API_URL}${path}")" || die "request to ${API_URL}${path} failed"
	BODY="$(cat "$WORK/body")"
}

expect() {
	local allowed="$1" what="$2"
	if [[ ",${allowed}," != *",${STATUS},"* ]]; then
		die "${what}: HTTP ${STATUS} $(jq -r '.message // empty' <<<"$BODY" 2>/dev/null || printf '%s' "$BODY")"
	fi
}

login() {
	local user="$1" pass="$2" jar="$3"
	: >"$jar"
	api POST /auth/login "$jar" "" "$(jq -nc --arg u "$user" --arg p "$pass" '{username: $u, password: $p}')"
	expect 200 "login as ${user}"
	jq -r '.csrf_token' <<<"$BODY"
}

psql_bench() {
	docker compose exec -T test-db psql -U "$DB_USER" -d "$BENCH_DB" -X -v ON_ERROR_STOP=1 "$@"
}

ts_ms() { date -d "$1" +%s%3N; }

fmt_s() { awk -v ms="$1" 'BEGIN { printf "%.2f s", ms / 1000 }'; }

fmt_int() { printf "%'d" "$1" 2>/dev/null || printf '%s' "$1"; }

step "Checking services"
api GET /health "$ANON_JAR"
expect 200 "backend health at ${API_URL}"
info "backend reachable at ${API_URL}"
docker compose exec -T test-db pg_isready -U "$DB_USER" -q || die "test-db container is not ready (run 'docker compose up -d' first)"
info "test-db reachable"

if [[ "$SKIP_SEED" == "false" ]]; then
	step "Seeding Tracen data and ${ROWS} training sessions into ${BENCH_DB}"
	seed_start="$(date +%s%3N)"
	docker compose exec -T test-db psql -U "$DB_USER" -d postgres -X -q -v ON_ERROR_STOP=1 \
		-v bench_db="$BENCH_DB" >/dev/null <<'SQL'
SET client_min_messages = warning;
SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', :'bench_db') \gexec
SELECT format('CREATE DATABASE %I', :'bench_db') \gexec
SQL
	psql_bench -q <scripts/seed-target.sql >/dev/null
	psql_bench -q -v rows="$ROWS" <scripts/seed-bulk.sql >/dev/null
	info "seeded in $(fmt_s $(( $(date +%s%3N) - seed_start )))"
fi

read -r ROW_COUNT TRAINEE_COUNT TABLE_SIZE < <(psql_bench -At -F ' ' -c \
	"SELECT count(*), count(DISTINCT trainee_id), replace(pg_size_pretty(pg_total_relation_size('public.training_sessions')), ' ', '') FROM public.training_sessions")
PG_VERSION="$(psql_bench -At -c "SHOW server_version")"
info "training_sessions=${ROW_COUNT} trainees=${TRAINEE_COUNT} size=${TABLE_SIZE} postgres=${PG_VERSION}"

step "Baseline: full-table UPDATE without batching (psql, BEGIN ... ROLLBACK)"
BASELINE_MS="$(psql_bench -q <<'SQL' | awk '/^Time:/ { n++; if (n == 2) print $2 }'
\timing on
BEGIN;
UPDATE public.training_sessions SET report_status = 'published', updated_at = now();
ROLLBACK;
SQL
)"
[[ -n "$BASELINE_MS" ]] || die "could not read UPDATE timing from psql"
info "UPDATE ${ROW_COUNT} rows: $(fmt_s "$BASELINE_MS")"

step "Bootstrapping bench user, group, managed database, and permission"
ADMIN_CSRF="$(login "$ADMIN_USER" "$ADMIN_PASS" "$ADMIN_JAR")"

api GET /admin/users "$ADMIN_JAR"
expect 200 "list users"
BENCH_USER_ID="$(jq -r --arg u "$BENCH_USER" '.[] | select(.username == $u) | .id' <<<"$BODY")"
if [[ -z "$BENCH_USER_ID" ]]; then
	api POST /admin/users "$ADMIN_JAR" "$ADMIN_CSRF" "$(jq -nc --arg u "$BENCH_USER" --arg p "$BENCH_PASS" '{username: $u, password: $p}')"
	expect 201 "create user ${BENCH_USER}"
	BENCH_USER_ID="$(jq -r '.id' <<<"$BODY")"
fi
info "user ${BENCH_USER} (id=${BENCH_USER_ID})"

api GET /admin/groups "$ADMIN_JAR"
expect 200 "list groups"
BENCH_GROUP_ID="$(jq -r --arg g "$BENCH_GROUP" '.[] | select(.name == $g) | .id' <<<"$BODY")"
if [[ -z "$BENCH_GROUP_ID" ]]; then
	api POST /admin/groups "$ADMIN_JAR" "$ADMIN_CSRF" "$(jq -nc --arg g "$BENCH_GROUP" '{name: $g}')"
	expect 201 "create group ${BENCH_GROUP}"
	BENCH_GROUP_ID="$(jq -r '.id' <<<"$BODY")"
fi
api POST "/admin/groups/${BENCH_GROUP_ID}/members" "$ADMIN_JAR" "$ADMIN_CSRF" "$(jq -nc --argjson id "$BENCH_USER_ID" '{user_id: $id}')"
expect 204,409 "add ${BENCH_USER} to ${BENCH_GROUP}"
api PUT "/admin/groups/${BENCH_GROUP_ID}/leader" "$ADMIN_JAR" "$ADMIN_CSRF" "$(jq -nc --argjson id "$BENCH_USER_ID" '{user_id: $id}')"
expect 200 "set ${BENCH_USER} as leader of ${BENCH_GROUP}"
info "group ${BENCH_GROUP} (id=${BENCH_GROUP_ID}), leader ${BENCH_USER}"

api GET /admin/databases "$ADMIN_JAR"
expect 200 "list managed databases"
DB_ID="$(jq -r --arg n "$BENCH_DB_NAME" '.[] | select(.name == $n) | .id' <<<"$BODY")"
if [[ -z "$DB_ID" ]]; then
	api POST /admin/databases "$ADMIN_JAR" "$ADMIN_CSRF" "$(jq -nc \
		--arg n "$BENCH_DB_NAME" --arg h "$TARGET_DB_HOST" --argjson p "$TARGET_DB_PORT" \
		--arg u "$DB_USER" --arg pw "$DB_PASS" --arg d "$BENCH_DB" \
		'{name: $n, host: $h, port: $p, username: $u, password: $pw, db_name: $d, ssl_mode: "disable"}')"
	expect 201 "register managed database ${BENCH_DB_NAME}"
	DB_ID="$(jq -r '.id' <<<"$BODY")"
fi
api PUT "/admin/groups/${BENCH_GROUP_ID}/databases/${DB_ID}" "$ADMIN_JAR" "$ADMIN_CSRF" '{"permission":"view_edit"}'
expect 200 "grant view_edit on ${BENCH_DB_NAME}"
info "managed database ${BENCH_DB_NAME} (id=${DB_ID}) -> ${TARGET_DB_HOST}:${TARGET_DB_PORT}/${BENCH_DB}"

USER_CSRF="$(login "$BENCH_USER" "$BENCH_PASS" "$USER_JAR")"

EXEC_JSON=""
approve_enqueue_wait() {
	local exec_id="$1" label="$2"
	api POST "/migrations/executions/${exec_id}/approve" "$USER_JAR" "$USER_CSRF" '{"note":"bench-batch.sh"}'
	expect 200 "approve ${label}"
	local approval_status
	approval_status="$(jq -r '.execution_status' <<<"$BODY")"
	[[ "$approval_status" == "approved" ]] || die "${label}: still '${approval_status}' after approval (another group with access to ${BENCH_DB_NAME} must approve too)"
	api POST "/migrations/executions/${exec_id}/enqueue" "$USER_JAR" "$USER_CSRF"
	expect 202 "enqueue ${label}"

	local deadline=$(( $(date +%s) + TIMEOUT )) status=""
	while (( $(date +%s) <= deadline )); do
		api GET "/migrations/executions/${exec_id}" "$USER_JAR"
		expect 200 "poll ${label}"
		status="$(jq -r '.status' <<<"$BODY")"
		if jq -e '.batch != null' <<<"$BODY" >/dev/null; then
			printf '\r  %s: %-10s batches=%s rows=%s   ' "$label" "$status" \
				"$(jq -r '.batch_count // 0' <<<"$BODY")" "$(jq -r '.rows_processed // 0' <<<"$BODY")"
		fi
		case "$status" in
			success)
				[[ "$(jq -r '.batch != null' <<<"$BODY")" == "true" ]] && printf '\n'
				EXEC_JSON="$BODY"
				return 0
				;;
			failed|partial|rejected|compensated|compensation_pending|compensating)
				printf '\n'
				die "${label}: execution #${exec_id} ended as '${status}': $(jq -r '.error_message // empty' <<<"$BODY")"
				;;
		esac
		sleep 1
	done
	die "${label}: timed out after ${TIMEOUT}s (last status '${status}')"
}

submit_draft() {
	local payload="$1" label="$2"
	api POST "/databases/${DB_ID}/migrations/drafts" "$USER_JAR" "$USER_CSRF" "$payload"
	expect 201 "create draft ${label}"
	local draft_id
	draft_id="$(jq -r '.id' <<<"$BODY")"
	api POST "/migrations/drafts/${draft_id}/execute" "$USER_JAR" "$USER_CSRF"
	expect 202 "submit draft ${label}"
	jq -r '.id' <<<"$BODY"
}

history_ms() {
	local exec_id="$1"
	api GET "/databases/${DB_ID}/migrations/history?page_size=100" "$USER_JAR"
	expect 200 "read migration history"
	jq -r --argjson id "$exec_id" '[.entries[] | select(.execution_id == $id and .status == "success")][0].duration_ms // empty' <<<"$BODY"
}

declare -A R
record() {
	local key="$1" exec_id="$2"
	local queued started finished
	queued="$(jq -r '.queued_at' <<<"$EXEC_JSON")"
	started="$(jq -r '.started_at' <<<"$EXEC_JSON")"
	finished="$(jq -r '.finished_at' <<<"$EXEC_JSON")"
	R[${key}_id]="$exec_id"
	R[${key}_sql]="$(history_ms "$exec_id")"
	R[${key}_wall]=$(( $(ts_ms "$finished") - $(ts_ms "$started") ))
	R[${key}_queue]=$(( $(ts_ms "$started") - $(ts_ms "$queued") ))
	R[${key}_batches]="$(jq -r '.batch_count // 0' <<<"$EXEC_JSON")"
	R[${key}_rows]="$(jq -r '.rows_processed // 0' <<<"$EXEC_JSON")"
	info "execution #${exec_id}: SQL $(fmt_s "${R[${key}_sql]:-0}"), worker $(fmt_s "${R[${key}_wall]}"), queue wait ${R[${key}_queue]} ms"
}

count_processed() {
	psql_bench -At -c "SELECT count(*) FILTER (WHERE report_status = 'published') FROM public.training_sessions"
}

RUN_TAG="$(date +%Y%m%d-%H%M%S)"
UP_SQL="UPDATE public.training_sessions SET report_status = 'published', updated_at = now()"
DOWN_SQL="UPDATE public.training_sessions SET report_status = 'draft', updated_at = now()"

step "Normal migration (no batching) through queue + worker"
exec_id="$(submit_draft "$(jq -nc --arg n "bench ${RUN_TAG}: publish all training reports (no batching)" --arg s "$UP_SQL" --arg d "$DOWN_SQL" \
	'{name: $n, sql: $s, down_sql: $d}')" "normal migration")"
approve_enqueue_wait "$exec_id" "normal migration"
record normal "$exec_id"
info "published training reports: $(count_processed)"

step "Rollback of the normal migration (restores the dataset)"
api POST "/databases/${DB_ID}/migrations/rollback" "$USER_JAR" "$USER_CSRF"
expect 202 "submit rollback"
exec_id="$(jq -r '.id' <<<"$BODY")"
approve_enqueue_wait "$exec_id" "rollback"
record rollback "$exec_id"
info "published training reports: $(count_processed)"

step "Per-batch migration on the same dataset (batch size ${BATCH_SIZE})"
exec_id="$(submit_draft "$(jq -nc --arg n "bench ${RUN_TAG}: publish training reports (${BATCH_SIZE}/batch)" \
	--arg s "${UP_SQL} WHERE id = ANY(\$1::bigint[])" --arg d "${DOWN_SQL} WHERE id = ANY(\$1::bigint[])" \
	--argjson b "$BATCH_SIZE" \
	'{name: $n, sql: $s, down_sql: $d, batch: {schema: "public", table: "training_sessions", key_column: "id", scope_sql: "", batch_size: $b}}')" "batch migration")"
approve_enqueue_wait "$exec_id" "batch migration"
record batch "$exec_id"
PROCESSED_AFTER_BATCH="$(count_processed)"
info "published training reports: ${PROCESSED_AFTER_BATCH}"
[[ "$PROCESSED_AFTER_BATCH" == "$ROW_COUNT" ]] || die "batch migration processed ${PROCESSED_AFTER_BATCH} of ${ROW_COUNT} rows"

THROUGHPUT="$(awk -v r="${R[batch_rows]}" -v ms="${R[batch_wall]}" 'BEGIN { if (ms > 0) printf "%d", r / (ms / 1000); else print "n/a" }')"
CPUS="$(docker info --format '{{.NCPU}}' 2>/dev/null || echo '?')"
MEM_GB="$(docker info --format '{{.MemTotal}}' 2>/dev/null | awk '{ printf "%.1f", $1 / 1024 / 1024 / 1024 }')"

step "Results (Markdown, paste into README)"
cat <<EOF

Environment: Docker ${CPUS} CPU / ${MEM_GB} GB RAM, PostgreSQL ${PG_VERSION}, tabel \`public.training_sessions\` (${TRAINEE_COUNT} trainee Tracen) di database \`${BENCH_DB}\`.

| Skenario | Execution | Durasi SQL | Durasi worker | Batch | Row |
| --- | --- | --- | --- | --- | --- |
| UPDATE tanpa batch (psql, \`BEGIN ... ROLLBACK\`) | - | $(fmt_s "$BASELINE_MS") | - | - | $(fmt_int "$ROW_COUNT") |
| Migration biasa (up, tanpa batch) | #${R[normal_id]} | $(fmt_s "${R[normal_sql]:-0}") | $(fmt_s "${R[normal_wall]}") | - | $(fmt_int "$ROW_COUNT") |
| Rollback migration biasa (down) | #${R[rollback_id]} | $(fmt_s "${R[rollback_sql]:-0}") | $(fmt_s "${R[rollback_wall]}") | - | $(fmt_int "$ROW_COUNT") |
| Migration per batch (${BATCH_SIZE} row/batch) | #${R[batch_id]} | $(fmt_s "${R[batch_sql]:-0}") | $(fmt_s "${R[batch_wall]}") | $(fmt_int "${R[batch_batches]}") | $(fmt_int "${R[batch_rows]}") |

- Jumlah row: $(fmt_int "$ROW_COUNT"), ukuran tabel: ${TABLE_SIZE}.
- Throughput migration per batch: $(fmt_int "$THROUGHPUT") row/detik.
- Waktu tunggu queue: biasa ${R[normal_queue]} ms, rollback ${R[rollback_queue]} ms, batch ${R[batch_queue]} ms.
- Durasi SQL diambil dari \`duration_ms\` migration history; durasi worker dari \`started_at\` sampai \`finished_at\` execution.
EOF
