#!/usr/bin/env bash
# End-to-end k6 stress test runner for BScheme (API + concurrent Same-DB vs
# Different-DB pre-approved migrations).
#
# Sets up 4 target databases inside `test-db`, registers/reuses them on the
# platform, creates distinct session cookies + pre-approved migration
# executions, and executes `loadtest/stress-test.js` via local `k6` or Docker
# `grafana/k6`.
#
# Usage:
#   bash loadtest/run-stress-test.sh [options]
#
# Options:
#   --quick            Short smoke stress run (STAGE_SEC=20, RAMP_SEC=5)
#   --same-jobs N      Number of pre-approved Same-DB jobs (default: 100, quick: 30)
#   --diff-jobs N      Number of pre-approved Diff-DB jobs (default: 180, quick: 55)
#   --sessions N       Number of pre-authenticated k6 user sessions (default: 40)
#   --k6-image IMAGE   Docker image if k6 is not installed locally (default: grafana/k6:0.54.0)
#   --skip-bootstrap   Reuse existing loadtest/.k6-fixture.json (warning: approved jobs are single-use)
#   -h, --help         Show this help and exit

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/docker-compose.yml"
ENV_FILE="${REPO_ROOT}/.env"
if [[ ! -f "$ENV_FILE" ]]; then
	ENV_FILE="${REPO_ROOT}/.env.example"
fi

QUICK_MODE=false
SAME_JOBS=""
DIFF_JOBS=""
SESSIONS=40
K6_IMAGE="grafana/k6:0.54.0"
SKIP_BOOTSTRAP=false

usage() {
	sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--quick)
			QUICK_MODE=true
			shift
			;;
		--same-jobs)
			[[ $# -ge 2 ]] || { echo "error: --same-jobs requires a number" >&2; exit 2; }
			SAME_JOBS="$2"
			shift 2
			;;
		--diff-jobs)
			[[ $# -ge 2 ]] || { echo "error: --diff-jobs requires a number" >&2; exit 2; }
			DIFF_JOBS="$2"
			shift 2
			;;
		--sessions)
			[[ $# -ge 2 ]] || { echo "error: --sessions requires a number" >&2; exit 2; }
			SESSIONS="$2"
			shift 2
			;;
		--k6-image)
			[[ $# -ge 2 ]] || { echo "error: --k6-image requires an image" >&2; exit 2; }
			K6_IMAGE="$2"
			shift 2
			;;
		--skip-bootstrap)
			SKIP_BOOTSTRAP=true
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "error: unknown option '$1'" >&2
			usage >&2
			exit 2
			;;
	esac
done

if [[ -z "$SAME_JOBS" ]]; then
	if [[ "$QUICK_MODE" == true ]]; then SAME_JOBS=30; else SAME_JOBS=100; fi
fi
if [[ -z "$DIFF_JOBS" ]]; then
	if [[ "$QUICK_MODE" == true ]]; then DIFF_JOBS=55; else DIFF_JOBS=180; fi
fi

for num_arg in "$SAME_JOBS" "$DIFF_JOBS" "$SESSIONS"; do
	if ! [[ "$num_arg" =~ ^[0-9]+$ ]] || [[ "$num_arg" -lt 1 ]]; then
		echo "error: positive integer expected, got '$num_arg'" >&2
		exit 2
	fi
done

read_dotenv_key() {
	local key="$1"
	local default="$2"
	local val=""
	if [[ -f "$ENV_FILE" ]]; then
		val="$(awk -F= -v k="$key" '
			$0 !~ /^[[:space:]]*#/ && $1 == k {
				sub(/^[^=]*=/, "", $0)
				line = $0
				sub(/^[[:space:]]+/, "", line)
				sub(/[[:space:]]+$/, "", line)
				print line
			}
		' "$ENV_FILE" | tail -n1)"
	fi
	if [[ -n "$val" ]]; then
		printf '%s' "$val"
	else
		printf '%s' "$default"
	fi
}

APP_PORT="${APP_PORT:-$(read_dotenv_key APP_PORT 8080)}"
ADMIN_USER="${APP_ADMIN_USERNAME:-$(read_dotenv_key APP_ADMIN_USERNAME admin)}"
ADMIN_PASS="${APP_ADMIN_PASSWORD:-$(read_dotenv_key APP_ADMIN_PASSWORD password)}"
TEST_DB_USER="${TEST_DB_USER:-$(read_dotenv_key TEST_DB_USER target)}"
TEST_DB_PASSWORD="${TEST_DB_PASSWORD:-$(read_dotenv_key TEST_DB_PASSWORD target_password)}"
API_URL="${API_URL:-http://localhost:${APP_PORT}}"

FIXTURE_FILE="${SCRIPT_DIR}/.k6-fixture.json"
RESULTS_DIR="${SCRIPT_DIR}/results"
mkdir -p "$RESULTS_DIR"

require_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command '$1' is not installed" >&2
		exit 1
	fi
}

require_cmd docker
require_cmd curl
require_cmd python3

echo "==> Phase 1: Verifying BScheme stack health"
if ! curl -fsS --max-time 5 "${API_URL}/health" >/dev/null 2>&1; then
	echo "error: BScheme API is unreachable at ${API_URL}/health." >&2
	echo "Start the stack first: docker compose up --build -d --remove-orphans --scale worker=3" >&2
	exit 1
fi

RUNNING_SERVICES="$(docker compose -f "$COMPOSE_FILE" ps --status running --services)"
for svc in internal worker platform-db test-db rabbitmq; do
	if ! grep -qx "$svc" <<<"$RUNNING_SERVICES"; then
		echo "error: Compose service '$svc' is not running" >&2
		exit 1
	fi
done
WORKER_COUNT="$(docker compose -f "$COMPOSE_FILE" ps --status running --format '{{.Service}}' | grep -cx 'worker' || true)"
echo "  [OK] API reachable at ${API_URL} | Running workers: ${WORKER_COUNT}"

if [[ "$SKIP_BOOTSTRAP" == false ]]; then
	echo "==> Phase 2: Ensuring 4 target databases exist and are seeded in test-db"
	for db in k6_same_db k6_diff_db_1 k6_diff_db_2 k6_diff_db_3; do
		exists="$(docker compose -f "$COMPOSE_FILE" exec -T test-db \
			psql -U "${TEST_DB_USER}" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${db}'")"
		if [[ "$exists" != "1" ]]; then
			docker compose -f "$COMPOSE_FILE" exec -T test-db \
				psql -U "${TEST_DB_USER}" -d postgres -c "CREATE DATABASE \"${db}\";" >/dev/null
		fi
		docker compose -f "$COMPOSE_FILE" exec -T test-db \
			psql -U "${TEST_DB_USER}" -d "${db}" < "${REPO_ROOT}/scripts/seed-target.sql" >/dev/null
		echo "  [OK] Ready target database '${db}'"
	done

	echo "==> Phase 3: Bootstrapping user/group/DB permissions + ${SAME_JOBS} Same-DB + ${DIFF_JOBS} Diff-DB approved executions"
	BOOTSTRAP_API_URL="$API_URL" \
	BOOTSTRAP_ADMIN_USER="$ADMIN_USER" \
	BOOTSTRAP_ADMIN_PASS="$ADMIN_PASS" \
	BOOTSTRAP_TEST_DB_USER="$TEST_DB_USER" \
	BOOTSTRAP_TEST_DB_PASS="$TEST_DB_PASSWORD" \
	BOOTSTRAP_SAME_JOBS="$SAME_JOBS" \
	BOOTSTRAP_DIFF_JOBS="$DIFF_JOBS" \
	BOOTSTRAP_SESSIONS="$SESSIONS" \
	BOOTSTRAP_FIXTURE_PATH="$FIXTURE_FILE" \
	python3 - <<'PY'
import concurrent.futures
import http.cookiejar
import json
import os
import time
import urllib.error
import urllib.request

API_URL = os.environ["BOOTSTRAP_API_URL"].rstrip("/")
ADMIN_USER = os.environ["BOOTSTRAP_ADMIN_USER"]
ADMIN_PASS = os.environ["BOOTSTRAP_ADMIN_PASS"]
DB_USER = os.environ["BOOTSTRAP_TEST_DB_USER"]
DB_PASS = os.environ["BOOTSTRAP_TEST_DB_PASS"]
SAME_JOBS = int(os.environ["BOOTSTRAP_SAME_JOBS"])
DIFF_JOBS = int(os.environ["BOOTSTRAP_DIFF_JOBS"])
SESSION_COUNT = int(os.environ["BOOTSTRAP_SESSIONS"])
FIXTURE_PATH = os.environ["BOOTSTRAP_FIXTURE_PATH"]

K6_USER = "k6_tester"
K6_PASS = "k6-stress-pass-123!"
K6_GROUP = "k6-stress-team"

TARGET_SPECS = [
    ("k6-target-same", "k6_same_db"),
    ("k6-target-diff-1", "k6_diff_db_1"),
    ("k6-target-diff-2", "k6_diff_db_2"),
    ("k6-target-diff-3", "k6_diff_db_3"),
]


class Client:
    def __init__(self):
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf = ""

    def request(self, method, path, body=None):
        url = f"{API_URL}{path}"
        headers = {}
        data = None
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        if self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with self.opener.open(req, timeout=15) as resp:
                raw = resp.read().decode("utf-8")
                return resp.status, (json.loads(raw) if raw else None)
        except urllib.error.HTTPError as e:
            raw = e.read().decode("utf-8", errors="replace")
            try:
                parsed = json.loads(raw)
            except Exception:
                parsed = {"raw": raw}
            return e.code, parsed

    def login(self, username, password):
        code, data = self.request("POST", "/auth/login", {"username": username, "password": password})
        if code != 200:
            raise RuntimeError(f"Login failed for {username}: HTTP {code} {data}")
        self.csrf = data["csrf_token"]
        cookies = [f"{c.name}={c.value}" for c in self.jar]
        return {
            "cookie": "; ".join(cookies),
            "csrf": self.csrf,
            "userId": data["user"]["id"],
        }


admin = Client()
admin.login(ADMIN_USER, ADMIN_PASS)

# Ensure k6_tester user
code, users = admin.request("GET", "/admin/users")
assert code == 200, f"GET /admin/users failed: {code} {users}"
user = next((u for u in users if u["username"] == K6_USER), None)
if not user:
    code, user = admin.request("POST", "/admin/users", {"username": K6_USER, "password": K6_PASS})
    assert code in (200, 201), f"Create user failed: {code} {user}"
elif user.get("disabled"):
    code, user = admin.request("PATCH", f"/admin/users/{user['id']}", {"disabled": False})
    assert code == 200, f"Enable user failed: {code} {user}"
user_id = user["id"]

# Ensure k6-stress-team group + member + leader
code, groups = admin.request("GET", "/admin/groups")
assert code == 200, f"GET /admin/groups failed: {code} {groups}"
group = next((g for g in groups if g["name"] == K6_GROUP), None)
if not group:
    code, group = admin.request("POST", "/admin/groups", {"name": K6_GROUP})
    assert code in (200, 201), f"Create group failed: {code} {group}"
group_id = group["id"]

code, members = admin.request("GET", f"/admin/groups/{group_id}/members")
assert code == 200, f"GET group members failed: {code} {members}"
if not any(m["id"] == user_id for m in members):
    code, resp = admin.request("POST", f"/admin/groups/{group_id}/members", {"user_id": user_id})
    assert code in (200, 201, 204), f"Add member failed: {code} {resp}"

if group.get("leader_user_id") != user_id:
    code, resp = admin.request("PUT", f"/admin/groups/{group_id}/leader", {"user_id": user_id})
    assert code == 200, f"Set group leader failed: {code} {resp}"

# Register/reuse 4 target DBs & assign exclusive view_edit access to k6-stress-team
code, existing_dbs = admin.request("GET", "/admin/databases")
assert code == 200, f"GET /admin/databases failed: {code} {existing_dbs}"
db_by_name = {d["name"]: d for d in existing_dbs}

managed_ids = {}
for reg_name, pg_db_name in TARGET_SPECS:
    payload = {
        "name": reg_name,
        "host": "test-db",
        "port": 5432,
        "username": DB_USER,
        "password": DB_PASS,
        "db_name": pg_db_name,
        "ssl_mode": "disable",
    }
    if reg_name in db_by_name:
        db_id = db_by_name[reg_name]["id"]
        code, updated = admin.request("PATCH", f"/admin/databases/{db_id}", payload)
        assert code == 200, f"Update DB {reg_name} failed: {code} {updated}"
    else:
        code, created = admin.request("POST", "/admin/databases", payload)
        assert code in (200, 201), f"Create DB {reg_name} failed: {code} {created}"
        db_id = created["id"]
    managed_ids[reg_name] = db_id

    # Grant view_edit explicitly to k6-stress-team, and revoke any other groups
    code, perm = admin.request("PUT", f"/admin/groups/{group_id}/databases/{db_id}", {"permission": "view_edit"})
    assert code == 200, f"Grant permission on {reg_name} failed: {code} {perm}"
    for g in groups:
        if g["id"] != group_id:
            admin.request("DELETE", f"/admin/groups/{g['id']}/databases/{db_id}")

# Create distinct k6_tester sessions for concurrent VUs
sessions = []
for _ in range(SESSION_COUNT):
    c = Client()
    s = c.login(K6_USER, K6_PASS)
    sessions.append({"cookie": s["cookie"], "csrf": s["csrf"]})

run_stamp = int(time.time())
same_db_id = managed_ids["k6-target-same"]
diff_db_ids = [
    managed_ids["k6-target-diff-1"],
    managed_ids["k6-target-diff-2"],
    managed_ids["k6-target-diff-3"],
]

def create_and_approve_execution(job_idx, db_id, mode_label):
    tag = f"k6_{run_stamp}_{mode_label}_{job_idx}"
    s = sessions[job_idx % len(sessions)]
    opener = urllib.request.build_opener()

    def call(method, path, body=None):
        headers = {"Cookie": s["cookie"], "X-CSRF-Token": s["csrf"]}
        data = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(f"{API_URL}{path}", data=data, headers=headers, method=method)
        try:
            with opener.open(req, timeout=15) as resp:
                raw = resp.read().decode("utf-8")
                return resp.status, (json.loads(raw) if raw else None)
        except urllib.error.HTTPError as e:
            raw = e.read().decode("utf-8", errors="replace")
            try:
                parsed = json.loads(raw)
            except Exception:
                parsed = {"raw": raw}
            return e.code, parsed

    sql = (
        "CREATE TABLE IF NOT EXISTS public.k6_migration_audit ("
        "id BIGSERIAL PRIMARY KEY, run_tag TEXT NOT NULL, seq INTEGER NOT NULL, "
        "created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()); "
        f"INSERT INTO public.k6_migration_audit (run_tag, seq) "
        f"SELECT '{tag}', gs FROM generate_series(1, 50) AS gs; "
        "SELECT pg_sleep(1.2);"
    )
    down_sql = f"DELETE FROM public.k6_migration_audit WHERE run_tag = '{tag}';"

    c_code, draft = call("POST", f"/databases/{db_id}/migrations/drafts", {
        "name": f"k6-{mode_label}-{run_stamp}-{job_idx}",
        "sql": sql,
        "down_sql": down_sql,
    })
    assert c_code in (200, 201), f"Draft failed: {c_code} {draft}"

    e_code, execution = call("POST", f"/migrations/drafts/{draft['id']}/execute")
    assert e_code in (200, 202), f"Execute submit failed: {e_code} {execution}"

    a_code, approval = call("POST", f"/migrations/executions/{execution['id']}/approve", {
        "note": f"pre-approved for k6 stress test ({tag})",
    })
    assert a_code == 200 and approval.get("execution_status") == "approved", (
        f"Approve failed for exec {execution['id']}: {a_code} {approval}"
    )
    return execution["id"]

tasks = []
for i in range(SAME_JOBS):
    tasks.append(("same", i, same_db_id))
for i in range(DIFF_JOBS):
    tasks.append(("diff", i, diff_db_ids[i % len(diff_db_ids)]))

same_exec_ids = [None] * SAME_JOBS
diff_exec_ids = [None] * DIFF_JOBS

with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
    fut_map = {
        pool.submit(create_and_approve_execution, idx, db_id, mode): (mode, idx)
        for (mode, idx, db_id) in tasks
    }
    for fut in concurrent.futures.as_completed(fut_map):
        mode, idx = fut_map[fut]
        exec_id = fut.result()
        if mode == "same":
            same_exec_ids[idx] = exec_id
        else:
            diff_exec_ids[idx] = exec_id

fixture_obj = {
    "createdAt": run_stamp,
    "sameDbId": same_db_id,
    "diffDbIds": diff_db_ids,
    "databaseIds": [same_db_id] + diff_db_ids,
    "sessions": sessions,
    "sameDbExecutionIds": same_exec_ids,
    "diffDbExecutionIds": diff_exec_ids,
}
with open(FIXTURE_PATH, "w", encoding="utf-8") as f:
    json.dump(fixture_obj, f, indent=2)

print(
    f"  [OK] Fixture ready: sessions={len(sessions)}, same_db_approved={len(same_exec_ids)}, "
    f"diff_db_approved={len(diff_exec_ids)}, db_ids={[same_db_id] + diff_db_ids}"
)
PY
else
	if [[ ! -f "$FIXTURE_FILE" ]]; then
		echo "error: --skip-bootstrap specified but ${FIXTURE_FILE} does not exist" >&2
		exit 1
	fi
	echo "==> Reusing existing fixture at ${FIXTURE_FILE}"
fi

echo "==> Phase 4: Running k6 stress test"
QUICK_ENV=0
if [[ "$QUICK_MODE" == true ]]; then
	QUICK_ENV=1
fi

set +e
if command -v k6 >/dev/null 2>&1; then
	k6 run \
		-e "BASE_URL=${API_URL}" \
		-e "FIXTURE_PATH=${FIXTURE_FILE}" \
		-e "SUMMARY_PATH=${RESULTS_DIR}/summary.json" \
		-e "QUICK=${QUICK_ENV}" \
		"${SCRIPT_DIR}/stress-test.js"
	K6_EXIT=$?
else
	INTERNAL_CID="$(docker compose -f "$COMPOSE_FILE" ps -q internal)"
	COMPOSE_NET="$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$INTERNAL_CID" | awk '{print $1}')"
	if [[ -z "$COMPOSE_NET" ]]; then
		echo "error: failed to detect Docker network of internal container" >&2
		exit 1
	fi
	docker run --rm \
		--network "$COMPOSE_NET" \
		-u "$(id -u):$(id -g)" \
		-v "${SCRIPT_DIR}:/scripts" \
		-e "BASE_URL=http://internal:${APP_PORT}" \
		-e "FIXTURE_PATH=/scripts/.k6-fixture.json" \
		-e "SUMMARY_PATH=/scripts/results/summary.json" \
		-e "QUICK=${QUICK_ENV}" \
		"$K6_IMAGE" run /scripts/stress-test.js
	K6_EXIT=$?
fi
set -e

echo "==> Summary written to ${RESULTS_DIR}/summary.json (k6 exit code: ${K6_EXIT})"
exit "$K6_EXIT"
