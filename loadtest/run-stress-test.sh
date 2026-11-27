#!/usr/bin/env bash
# End-to-end k6 stress test runner for BScheme (API + concurrent Same-DB vs Diff-DB pre-approved migrations).
#
# Usage:
#   bash loadtest/run-stress-test.sh [--quick]

set -euo pipefail

# ----------------------------------------------------------------------------
# 1. Configuration & CLI Flags
# ----------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/docker-compose.yml"
ENV_FILE="${REPO_ROOT}/.env"
[[ -f "$ENV_FILE" ]] || ENV_FILE="${REPO_ROOT}/.env.example"

QUICK_MODE=false
[[ "${1:-}" == "--quick" ]] && QUICK_MODE=true

# Pre-approved execution pool sizes (must exceed total jobs dispatched across all 4 stages)
SAME_JOBS=$([[ "$QUICK_MODE" == true ]] && echo 30 || echo 100)
DIFF_JOBS=$([[ "$QUICK_MODE" == true ]] && echo 55 || echo 180)
SESSIONS=40

read_env() {
	local val=""
	[[ -f "$ENV_FILE" ]] && val="$(awk -F= -v k="$1" '$0 !~ /^[[:space:]]*#/ && $1 == k { sub(/^[^=]*=/, ""); v=$0 } END { print v }' "$ENV_FILE" | tr -d '\r"'\''')"
	printf '%s' "${val:-$2}"
}

APP_PORT="${APP_PORT:-$(read_env APP_PORT 8080)}"
ADMIN_USER="${APP_ADMIN_USERNAME:-$(read_env APP_ADMIN_USERNAME admin)}"
ADMIN_PASS="${APP_ADMIN_PASSWORD:-$(read_env APP_ADMIN_PASSWORD password)}"
TEST_DB_USER="${TEST_DB_USER:-$(read_env TEST_DB_USER target)}"
TEST_DB_PASS="${TEST_DB_PASSWORD:-$(read_env TEST_DB_PASSWORD target_password)}"
API_URL="${API_URL:-http://localhost:${APP_PORT}}"
FIXTURE_FILE="${SCRIPT_DIR}/.k6-fixture.json"
mkdir -p "${SCRIPT_DIR}/results"

for cmd in docker curl python3; do
	command -v "$cmd" >/dev/null || { echo "error: '$cmd' is required" >&2; exit 1; }
done

# ----------------------------------------------------------------------------
# 2. Verify Stack Health (API, Worker, DBs, RabbitMQ)
# ----------------------------------------------------------------------------
echo "==> 1/4 Checking BScheme stack health"
curl -fsS --max-time 5 "${API_URL}/health" >/dev/null || { echo "error: API unreachable at ${API_URL}/health" >&2; exit 1; }
RUNNING="$(docker compose -f "$COMPOSE_FILE" ps --status running --services)"
for svc in internal worker platform-db test-db rabbitmq; do
	grep -qx "$svc" <<<"$RUNNING" || { echo "error: service '$svc' is not running" >&2; exit 1; }
done

# ----------------------------------------------------------------------------
# 3. Create & Seed 4 Target Databases (1 Same-DB + 3 Diff-DBs)
# ----------------------------------------------------------------------------
echo "==> 2/4 Ensuring 4 seeded target databases in test-db"
for db in k6_same_db k6_diff_db_1 k6_diff_db_2 k6_diff_db_3; do
	exists="$(docker compose -f "$COMPOSE_FILE" exec -T test-db psql -U "${TEST_DB_USER}" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${db}'")"
	[[ "$exists" == "1" ]] || docker compose -f "$COMPOSE_FILE" exec -T test-db psql -U "${TEST_DB_USER}" -d postgres -c "CREATE DATABASE \"${db}\";" >/dev/null
	docker compose -f "$COMPOSE_FILE" exec -T test-db psql -U "${TEST_DB_USER}" -d "${db}" < "${REPO_ROOT}/scripts/seed-target.sql" >/dev/null
done

# ----------------------------------------------------------------------------
# 4. Bootstrap User, Group Permissions, Sessions & Pre-Approved Executions
# ----------------------------------------------------------------------------
echo "==> 3/4 Bootstrapping ${SAME_JOBS} Same-DB + ${DIFF_JOBS} Diff-DB pre-approved migrations"
API_URL="$API_URL" ADMIN_USER="$ADMIN_USER" ADMIN_PASS="$ADMIN_PASS" DB_USER="$TEST_DB_USER" DB_PASS="$TEST_DB_PASS" \
SAME_JOBS="$SAME_JOBS" DIFF_JOBS="$DIFF_JOBS" SESSIONS="$SESSIONS" FIXTURE_FILE="$FIXTURE_FILE" \
python3 - <<'PY'
import concurrent.futures, http.cookiejar, json, os, time, urllib.error, urllib.request

API, ADMIN_U, ADMIN_P = os.environ["API_URL"].rstrip("/"), os.environ["ADMIN_USER"], os.environ["ADMIN_PASS"]
DB_U, DB_P = os.environ["DB_USER"], os.environ["DB_PASS"]
SAME_N, DIFF_N, SESS_N = int(os.environ["SAME_JOBS"]), int(os.environ["DIFF_JOBS"]), int(os.environ["SESSIONS"])
TARGETS = [("k6-target-same", "k6_same_db"), ("k6-target-diff-1", "k6_diff_db_1"), ("k6-target-diff-2", "k6_diff_db_2"), ("k6-target-diff-3", "k6_diff_db_3")]

class Client:
    def __init__(self, cookie="", csrf=""):
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        self.cookie, self.csrf = cookie, csrf

    def call(self, method, path, body=None):
        headers = {}
        if self.cookie: headers["Cookie"] = self.cookie
        if self.csrf: headers["X-CSRF-Token"] = self.csrf
        data = json.dumps(body).encode() if body is not None else None
        if data is not None: headers["Content-Type"] = "application/json"
        req = urllib.request.Request(f"{API}{path}", data=data, headers=headers, method=method)
        try:
            with self.opener.open(req, timeout=15) as r:
                raw = r.read().decode()
                return r.status, (json.loads(raw) if raw else None)
        except urllib.error.HTTPError as e:
            return e.code, e.read().decode(errors="replace")

    def login(self, u, p):
        code, data = self.call("POST", "/auth/login", {"username": u, "password": p})
        assert code == 200, f"login failed: {code} {data}"
        self.csrf = data["csrf_token"]
        self.cookie = "; ".join(f"{c.name}={c.value}" for c in self.jar)
        return {"cookie": self.cookie, "csrf": self.csrf}

# 4a. Ensure k6_tester user and k6-stress-team group (with k6_tester as group leader)
admin = Client(); admin.login(ADMIN_U, ADMIN_P)
_, users = admin.call("GET", "/admin/users")
user = next((u for u in users if u["username"] == "k6_tester"), None)
if not user: _, user = admin.call("POST", "/admin/users", {"username": "k6_tester", "password": "k6-stress-pass-123!"})
elif user.get("disabled"): _, user = admin.call("PATCH", f"/admin/users/{user['id']}", {"disabled": False})
uid = user["id"]

_, groups = admin.call("GET", "/admin/groups")
grp = next((g for g in groups if g["name"] == "k6-stress-team"), None)
if not grp: _, grp = admin.call("POST", "/admin/groups", {"name": "k6-stress-team"})
gid = grp["id"]
_, members = admin.call("GET", f"/admin/groups/{gid}/members")
if not any(m["id"] == uid for m in members): admin.call("POST", f"/admin/groups/{gid}/members", {"user_id": uid})
if grp.get("leader_user_id") != uid: admin.call("PUT", f"/admin/groups/{gid}/leader", {"user_id": uid})

# 4b. Register 4 target DBs and grant exclusive view_edit permission to k6-stress-team
_, existing_dbs = admin.call("GET", "/admin/databases")
by_name = {d["name"]: d["id"] for d in existing_dbs}
db_ids = {}
for name, pg_db in TARGETS:
    cfg = {"name": name, "host": "test-db", "port": 5432, "username": DB_U, "password": DB_P, "db_name": pg_db, "ssl_mode": "disable"}
    if name in by_name:
        did = by_name[name]; admin.call("PATCH", f"/admin/databases/{did}", cfg)
    else:
        _, created = admin.call("POST", "/admin/databases", cfg); did = created["id"]
    db_ids[name] = did
    admin.call("PUT", f"/admin/groups/{gid}/databases/{did}", {"permission": "view_edit"})
    for g in groups:
        if g["id"] != gid: admin.call("DELETE", f"/admin/groups/{g['id']}/databases/{did}")

# 4c. Create distinct sessions so concurrent VUs do not contend on a single session row
sessions = [Client().login("k6_tester", "k6-stress-pass-123!") for _ in range(SESS_N)]
stamp = int(time.time())
same_id = db_ids["k6-target-same"]
diff_ids = [db_ids["k6-target-diff-1"], db_ids["k6-target-diff-2"], db_ids["k6-target-diff-3"]]

# 4d. Pre-create and approve migration executions before k6 starts
def make_approved(idx, did, mode):
    s = sessions[idx % len(sessions)]
    c = Client(s["cookie"], s["csrf"])
    tag = f"k6_{stamp}_{mode}_{idx}"
    sql = (
        "CREATE TABLE IF NOT EXISTS public.k6_migration_audit (id BIGSERIAL PRIMARY KEY, run_tag TEXT NOT NULL, seq INT NOT NULL);"
        f" INSERT INTO public.k6_migration_audit (run_tag, seq) SELECT '{tag}', gs FROM generate_series(1,50) gs; SELECT pg_sleep(1.2);"
    )
    _, d = c.call("POST", f"/databases/{did}/migrations/drafts", {"name": f"k6-{mode}-{stamp}-{idx}", "sql": sql, "down_sql": f"DELETE FROM public.k6_migration_audit WHERE run_tag='{tag}';"})
    _, ex = c.call("POST", f"/migrations/drafts/{d['id']}/execute")
    code, ap = c.call("POST", f"/migrations/executions/{ex['id']}/approve", {"note": f"k6 {tag}"})
    assert code == 200 and ap.get("execution_status") == "approved", f"approve failed: {code} {ap}"
    return ex["id"]

tasks = [("same", i, same_id) for i in range(SAME_N)] + [("diff", i, diff_ids[i % 3]) for i in range(DIFF_N)]
same_exec, diff_exec = [None] * SAME_N, [None] * DIFF_N
with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
    futs = {pool.submit(make_approved, i, did, m): (m, i) for m, i, did in tasks}
    for f in concurrent.futures.as_completed(futs):
        m, i = futs[f]
        (same_exec if m == "same" else diff_exec)[i] = f.result()

with open(os.environ["FIXTURE_FILE"], "w") as f:
    json.dump({"databaseIds": [same_id] + diff_ids, "sessions": sessions, "sameDbExecutionIds": same_exec, "diffDbExecutionIds": diff_exec}, f)
print(f"  [OK] Fixture ready: sessions={len(sessions)}, same_db={len(same_exec)}, diff_db={len(diff_exec)}")
PY

# ----------------------------------------------------------------------------
# 5. Execute k6 Stress Test (Local Binary or Docker Container)
# ----------------------------------------------------------------------------
echo "==> 4/4 Running k6 stress test"
QUICK_ENV=$([[ "$QUICK_MODE" == true ]] && echo 1 || echo 0)
if command -v k6 >/dev/null 2>&1; then
	k6 run -e "BASE_URL=${API_URL}" -e "FIXTURE_PATH=${FIXTURE_FILE}" -e "SUMMARY_PATH=${SCRIPT_DIR}/results/summary.json" -e "QUICK=${QUICK_ENV}" "${SCRIPT_DIR}/stress-test.js"
else
	CID="$(docker compose -f "$COMPOSE_FILE" ps -q internal)"
	NET="$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}' "$CID" | awk '{print $1}')"
	docker run --rm --network "$NET" -u "$(id -u):$(id -g)" -v "${SCRIPT_DIR}:/scripts" \
		-e "BASE_URL=http://internal:${APP_PORT}" -e "FIXTURE_PATH=/scripts/.k6-fixture.json" \
		-e "SUMMARY_PATH=/scripts/results/summary.json" -e "QUICK=${QUICK_ENV}" \
		grafana/k6:0.54.0 run /scripts/stress-test.js
fi
