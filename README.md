# BScheme

## description


BScheme is a web-based PostgreSQL management system that helps teams explore schemas and data, manage database access, and review and execute migrations through a simple interface.

Made for IF3110 Web Development Course.

## start server + Platform DB
```bash
docker compose up --build
```

The backend runs at `http://localhost:8080`, and the frontend runs at `http://localhost:5173`.

To run multiple migration workers (jobs for the same target database still run one at a time):

```bash
docker compose up -d --scale worker=3
```

## start server and migration worker locally

Run the API and the durable migration worker in separate terminals. They have separate entrypoints and share the relevant Platform DB and RabbitMQ settings:

```bash
cd backend
go run ./cmd/server
```

```bash
cd backend
go run ./cmd/worker
```

Execution delivery uses RabbitMQ; the Platform DB remains the source of truth. The API writes an outbox record atomically with the queued execution, and a relay publishes only the execution ID to the durable queue. In Compose, RabbitMQ and the separate `worker` service are started alongside the API automatically. For local runs, start RabbitMQ and set `APP_RABBITMQ_URL` (default `amqp://guest:guest@localhost:5672/`); `APP_RABBITMQ_QUEUE` defaults to `migration.executions`.

## OpenAPI

The API contract is defined in [`backend/api/openapi.yaml`](backend/api/openapi.yaml). Generated
handlers and models are written to `backend/internal/http/api.gen.go`; do not edit that
file manually.

Regenerate the OpenAPI code after changing the contract:

```bash
cd backend
go generate ./internal/http
```

## authentication endpoints

- `POST /auth/login` creates the session cookie and returns a CSRF token. The request must use `Content-Type: application/json`; other content types receive `415`, so cross-site HTML forms cannot submit a login.
- `POST /auth/logout` requires the session cookie and `X-CSRF-Token` header.
- `GET /admin/ping` requires a valid session belonging to a platform admin.

## platform admin endpoints

The following endpoints require a valid platform-admin session. Mutation endpoints
also require the `X-CSRF-Token` returned by login:

- `GET /admin/users` lists users.
- `POST /admin/users` creates a regular user.
- `PATCH /admin/users/{userId}` enables or disables a user.
- `GET /admin/groups` lists groups with their leader.
- `POST /admin/groups` creates a group.
- `GET /admin/groups/{groupId}/members` lists group members.
- `POST /admin/groups/{groupId}/members` adds a user to a group.
- `DELETE /admin/groups/{groupId}/members/{userId}` removes a user from a group. The group leader cannot be removed (`409`); assign another leader or clear the leader first.
- `PUT /admin/groups/{groupId}/leader` assigns the group leader. The leader must be an enabled member of the group.
- `DELETE /admin/groups/{groupId}/leader` clears the group leader.
- `GET /admin/groups/{groupId}/databases` lists group database permissions.
- `PUT /admin/groups/{groupId}/databases/{databaseId}` sets View Only or View + Edit permission.
- `DELETE /admin/groups/{groupId}/databases/{databaseId}` removes a group database permission.
- `GET /admin/databases` lists managed target databases.
- `POST /admin/databases` registers a target database.
- `POST /admin/databases/test` tests a target database connection before storing it.
- `GET /admin/databases/{databaseId}` gets managed database metadata.
- `PATCH /admin/databases/{databaseId}` updates a managed target database.
- `DELETE /admin/databases/{databaseId}` removes a managed target database.
- `POST /admin/databases/{databaseId}/test` tests stored target database credentials.

## protected database endpoints

These endpoints require a valid session and database permission. Viewer reads require View Only or View + Edit; migration creation/update/execution requires View + Edit.

Database permission is evaluated independently for each target database. For one specific database, if a user belongs to multiple groups, the effective permission is the highest permission assigned to that same database by any of those groups. There is no single global database permission for a user:

```text
User + Database A: max(permission from the user's groups for Database A)
User + Database B: max(permission from the user's groups for Database B)

None < View Only < View + Edit
```

- `GET /databases` lists managed databases visible to the current user.
- `GET /databases/{databaseId}/schemas` lists schemas.
- `GET /databases/{databaseId}/tables` lists tables for a schema.
- `GET /databases/{databaseId}/table-structure` describes a table.
- `GET /databases/{databaseId}/table-rows` reads table data with pagination.
- `GET /databases/{databaseId}/migrations/history` lists migration history.
- `GET /databases/{databaseId}/migrations/history/{version}` gets migration detail.
- `POST /databases/{databaseId}/migrations/explain` returns PostgreSQL `EXPLAIN (FORMAT JSON)` plans without executing SQL. It requires View permission and an `X-CSRF-Token` header.
- `GET /databases/{databaseId}/migrations/drafts` lists drafts.
- `POST /databases/{databaseId}/migrations/drafts` creates a draft.
- `GET /migrations/drafts/{draftId}` gets a draft.
- `PATCH /migrations/drafts/{draftId}` updates a draft.
- `POST /migrations/drafts/{draftId}/execute` submits a draft for leader review. It does not run SQL; see [migration approval workflow](#migration-approval-workflow).
- `POST /databases/{databaseId}/migrations/rollback` submits a rollback of the latest active migration for review.
- `POST /databases/{databaseId}/migrations/forward` submits a forward of the latest rolled-back migration for review.
- `GET /databases/{databaseId}/migrations/executions` lists executions, newest first.
- `GET /migrations/executions/{executionId}` gets an execution and its status.
- `GET /migrations/executions/{executionId}/approvals` lists the approval of every required group and whether the current user can decide.
- `POST /migrations/executions/{executionId}/approve` approves as a group leader. The body `{"note": "..."}` is optional.
- `POST /migrations/executions/{executionId}/reject` rejects as a group leader. The body `{"note": "..."}` is required.
- `POST /migrations/executions/{executionId}/enqueue` queues an approved execution for the worker. Requires View + Edit.
- `GET /migrations/approvals/pending` lists executions waiting for the current user's decision.

## migration approval workflow

Every change, including rollback and forward, needs approval from the leader of each group with access to the database.

```mermaid
stateDiagram-v2
    direction LR
    [*] --> pending_review: submit
    pending_review --> approved: every group approved
    pending_review --> rejected: a leader rejects
    approved --> pending_review: a new group gained access
    approved --> queued: enqueue (View + Edit)
    queued --> running
    running --> success
    running --> failed
    rejected --> [*]: draft becomes editable
```

- Groups to approve are fixed at submit; every one of them needs a leader.
- One approval covers every group the leader leads, including their own submission.
- Rejecting needs a reason. Worker retries do not need approval again.


## frontend

The frontend is a React application built with Vite and uses the backend JSON API. `docker compose up --build` starts both the backend and frontend. To run the frontend outside Docker, start the backend first, then run the frontend in a separate terminal:

```bash
cd frontend
pnpm install
pnpm dev
```

Open `http://localhost:5173`. During development, Vite reads `APP_PORT` and `BACKEND_HOST` from the repository-root `.env` file. Use `BACKEND_HOST=localhost` when running locally; Docker Compose overrides it to `internal`. The backend does not serve the frontend build.

## local test databases and development seed

`docker compose up --build` starts two PostgreSQL services:

- `platform-db`: the Platform DB used by the backend. Its schema is created by GORM `AutoMigrate` when the backend starts.
- `test-db`: a separate target database named `targetdb`, seeded automatically from [`scripts/seed-target.sql`](scripts/seed-target.sql) when its volume is initialized. The seed is a Tracen Academy record: `teams`, `trainees` (60 rows, so the viewer shows more than one page), `races`, `race_results`, and the `race_winners` view.

PostgreSQL runs the seed only when the volume is empty. After changing the seed, recreate the volumes with `docker compose down -v`, which deletes all data in both databases.

The backend connects to the target database through the Compose network, so register it in the UI with:

```text
Host: test-db
Port: 5432
Database: targetdb
Username: target
Password: target_password
SSL mode: Disable
```

To add development users and groups to the Platform DB, run this after the backend has started and completed `AutoMigrate`:

```bash
docker compose exec -T platform-db psql -U platform -d platform < scripts/seed-platform.sql
```

The seed creates these users, all with the password `password`:

```text
Group            Members                  Leader     Suggested permission on targetdb
Student Council  rudolf, airgroove        rudolf     View + Edit
Team Spica       specialweek, goldship    goldship   View Only
(no group)       tachyon                  -          none
```

With both permissions assigned, a migration on `targetdb` needs approval from rudolf and goldship before anyone with View + Edit can queue it. The seed only sets a leader on groups that have none, so a leader changed by the admin is kept.

The groups receive no database permission from the seed, because a permission needs a registered managed database and its password is encrypted with `TARGET_DB_ENCRYPTION_KEY`. Sign in as the Platform Admin, register `targetdb` with the connection above, then assign it to `Student Council` with View + Edit and to `Team Spica` with View Only. The platform admin manages these assignments but does not receive target-database access automatically.

The script is safe to run repeatedly and does not overwrite an existing user password. If `APP_USER` or `APP_NAME` is customized in `.env`, replace `platform` in the command with the configured values.

## migration execution workflow

The migration workflow/approval component must call the migration service's `ApproveExecution` only after all required group-leader approvals, then call `EnqueueApprovedExecution`. Enqueueing updates execution state and inserts the outbox message in one Platform-DB transaction. A relay publishes the execution ID to RabbitMQ with publisher confirms; workers load immutable execution snapshots from the Platform DB and acknowledge only after the result is persisted. Up, rollback, and forward each create immutable execution snapshots; target history is append-only.

- Migration SQL cannot reference `_dbm`; the worker also fails any attempt that changes `_dbm`, and guard triggers make its history tables append-only.
- Per-batch up/down SQL must be a single `UPDATE ... = ANY($1::bigint[])`. Compensation runs the approved Down SQL and is rejected if the recorded pre-batch values are not restored.
- Permission changes and approval checks for the same database are serialized, so a newly granted group always becomes a required approver.
- Transient errors (`40001`, `40P01`) are retried with backoff.

### lock recovery when a worker fails

If a worker dies, PostgreSQL rolls back its open transaction and releases the target advisory lock; committed batches and checkpoints stay. Every 30 s, other workers pick up `running` executions that have gone stale: if the target history shows the attempt committed, it is marked `success`; otherwise it is requeued (batches resume from the checkpoint), up to 3 attempts.

## large dataset and batch migration test

[`scripts/seed-bulk.sql`](scripts/seed-bulk.sql) adds `training_sessions` to the Tracen seed: 1,000,000 training sessions for the 60 trainees, each linked to a matching G1 target race. [`scripts/bench-batch.sh`](scripts/bench-batch.sh) seeds a separate `benchdb`, then runs `UPDATE training_sessions SET report_status = 'published'` without batching (psql), as a normal migration, its rollback, and a per-batch migration on the same data.

```bash
docker compose up -d --build
bash scripts/bench-batch.sh            # options: --rows N --batch-size N
```

Result (Docker 16 CPU / 7.5 GB RAM, PostgreSQL 17, 1,000,000 rows):

| Scenario | Duration |
| --- | --- |
| `UPDATE` without batching (psql) | 9.21 s |
| Normal migration | 11.45 s |
| Rollback (down) | 10.73 s |
| Per-batch migration (200 × 5,000 rows) | 43.47 s |

The per-batch migration is slower in total because each batch is its own transaction with a checkpoint, but it locks only 5,000 rows at a time and resumes from the last committed batch after a failure.

## integration tests

Integration tests use Testcontainers and require Docker:

```bash
cd backend
go test -tags=integration ./...
```

## coverage report

```bash
bash scripts/coverage.sh          # unit + integration tests (requires Docker)
bash scripts/coverage.sh --unit   # unit tests only
```

The script prints statement coverage per package and writes `coverage.txt` (per package and per function) and `coverage.html` (source view with covered and uncovered lines) to the repository root. It runs Go coverage from `backend/` and excludes the generated `backend/internal/http/api.gen.go` from the report.

## stress testing (k6)

Stress testing menguji **BScheme API** (`bscheme-api`) dan **migration worker** secara bersamaan ketika beban dinaikkan bertahap di atas beban normal lalu diturunkan kembali untuk mengamati pemulihan sistem (*recovery*), baik pada **1 database yang sama** (`k6_same_db`) maupun **3 database berbeda** (`k6_diff_db_1..3`).

### Menjalankan Pengujian

```bash
docker compose up --build -d --remove-orphans --scale worker=3
bash loadtest/run-stress-test.sh        # Full run (240s, 4 tahap x 60s)
bash loadtest/run-stress-test.sh --quick # Quick run (80s, 4 tahap x 20s)
```

### Penjelasan Singkat Script

- **[`loadtest/run-stress-test.sh`](loadtest/run-stress-test.sh)**: membuat & mengisi 4 target database pada `test-db`, menyiapkan `40` sesi login terautentikasi (`k6_tester`), serta membuat dan men-approve `100` job *Same-DB* + `180` job *Diff-DB* (`SELECT pg_sleep(1.2)`) sebelum tes dimulai.
- **[`loadtest/stress-test.js`](loadtest/stress-test.js)**: menjalankan 3 skenario paralel selama 4 tahap (`Normal` -> `High` -> `Peak Stress` -> `Recovery`):
  1. `api_access`: akses endpoint API (`/databases`, `/schemas`, `/tables`, `/table-structure`, `/table-rows`, `/migrations/history`, `POST /query`).
  2. `migrations_same_db` & `migrations_diff_db`: memanggil `POST /migrations/executions/{id}/enqueue` lalu melakukan *polling* `GET /migrations/executions/{id}` setiap `500ms` sampai status terminal (`success`/`failed`). Respons `202 Accepted` saat enqueue **tidak** dihitung sebagai keberhasilan eksekusi migration.
- **Threshold k6**: `api_req_duration` / `http_req_duration{kind:api}` `p(95) < 500 ms`, `api_duration_normal` `p(95) < 150 ms`, `api_duration_recovery` `p(95) < 250 ms`, `api_error_rate` & `http_req_failed` `< 1%`, serta `migration_job_error_rate < 5%` (**semua PASS**, ringkasan di [`loadtest/results/summary.json`](loadtest/results/summary.json)).

### Hasil Pengujian per Tingkat Beban

| Tingkat Beban | Konfigurasi Beban | API Throughput | API `p50` / `p95` | Error (API / Job) | Job Acc / Selesai | Queue Wait `p50` / `p95` | Exec `p50` / `p95` | Job Throughput |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **Normal** (`0–60s`) | `10` VU; `4/m` Same + `9/m` Diff | `165.0 req/s` | `4.5 ms` / `6.9 ms` | `0.00%` / `0.00%` | `11` / `11` | `356 ms` / `690 ms` | `1.23 s` / `1.23 s` | `11.0 jobs/min` |
| **High** (`60–120s`) | `80` VU; `18/m` Same + `36/m` Diff | `1,248.8 req/s` | `5.6 ms` / `24.3 ms` | `0.00%` / `0.00%` | `51` / `51` | `550 ms` / `940 ms` | `1.24 s` / `1.26 s` | `51.0 jobs/min` |
| **Peak Stress** (`120–180s`) | `220` VU; `48/m` Same + `90/m` Diff | `2,089.1 req/s` | `39.6 ms` / `119.3 ms` | `0.00%` / `0.00%` | `129` / `129` | `8.09 s` / `18.48 s` | `1.25 s` / `1.26 s` | `129.0 jobs/min` |
| **Recovery** (`180–240s`) | `10` VU; `4/m` Same + `9/m` Diff | `407.3 req/s` | `7.5 ms` / `54.1 ms` | `0.00%` / `0.00%` | `22` / `22` | `16.16 s` / `19.33 s` *(min `19 ms`)* | `1.23 s` / `1.23 s` | `22.0 jobs/min` |
| **Overall** (`240s`) | Total `238,083` HTTP (`234,614` API) | `977.6 req/s` | `21.6 ms` / `95.7 ms` | `0.00%` / `0.00%` | `213` / `213` | `2.79 s` / `18.77 s` | `1.25 s` / `1.26 s` | `53.25 jobs/min` |

**Perbandingan Same DB vs Different DBs**:
- **Same DB (`k6_same_db`)**: `73/73` sukses (`0.00%` error), `QWait p50 = 4.05 s`, `p95 = 22.75 s` (`max = 31.18 s`), throughput **`18.25 jobs/min`**.
- **Diff DB (`k6_diff_db_1..3`)**: `140/140` sukses (`0.00%` error), `QWait p50 = 2.24 s`, `p95 = 18.53 s` (`max = 19.33 s`), throughput **`35.00 jobs/min`**.

### Analisis per Tingkat Beban

1. **Normal (`0–60s`)**: Pada beban normal (`10` VU, `165 req/s`, `11` job), API sangat responsif (`p50 = 4.5 ms`, `p95 = 6.9 ms`, `0%` error). Waktu tunggu queue (~`0.36–0.69s`) berasal dari interval polling `OutboxRelay` (`1s`), dengan durasi eksekusi konsisten `1.23 s`.
2. **High (`60–120s`)**: Beban naik `7.6×` menjadi `1,248.8 req/s`. `p50` tetap `5.6 ms`, sedangkan `p95` naik ke `24.3 ms` karena mulai terjadi antrean singkat pada connection pool DB. Queue wait worker masih stabil (`p95 = 940 ms`) karena `51` job/menit masih di bawah kapasitas 3 worker.
3. **Peak Stress (`120–180s`)**: Pada `220` VU (`2,089.1 req/s`), saturasi connection pool mendorong latensi API ke `p50 = 39.6 ms` dan `p95 = 119.3 ms` (tetap di bawah threshold `500 ms` dengan `0%` error). Di sisi worker (`129` job), constraint serial per database pada `k6_same_db` memicu `ErrExecutionBusy` + backoff `2s` sehingga antrean Same-DB menumpuk (`QWait p95 = 22.75 s`, `18.25 jobs/min`), sementara `k6_diff_db_1..3` dieksekusi paralel oleh 3 worker hingga `~2×` lebih cepat (`35.00 jobs/min`, `QWait p50 = 2.24 s`).
4. **Recovery (`180–240s`)**: Setelah beban diturunkan ke `10` VU, latensi API langsung pulih (`p50 = 7.5 ms`, `p95 = 54.1 ms < 250 ms`, `0%` error). Job awal di tahap *recovery* masih menghabiskan sisa antrean dari *peak stress* (`QWait p50 = 16.16 s`), lalu antrean terkuras habis hingga job di akhir tahap kembali ke `min = 19 ms` dengan total `213 / 213` job selesai `100%` sukses.

## observability

Centralized logs (Loki `:3100`), distributed traces across API -> Outbox -> RabbitMQ -> Worker -> PostgreSQL (Tempo `:3200` via OpenTelemetry Collector), Prometheus metrics (`:9090`, scraping `bscheme-api` `:8080/metrics` and every scaled `bscheme-worker` replica on `:9091/metrics` via `dns_sd_configs`), and an auto-provisioned Grafana dashboard (`:3000`, default `admin` / `admin`) run inside a single `lgtm` container (`grafana/otel-lgtm`) in the Compose stack:

```bash
docker compose up --build -d --scale worker=3
bash scripts/test-observability.sh   # generate API, migration & DB pool traffic and verify LGTM ingestion
```

### Queue & Migration Metrics (Prometheus -> Grafana)

| Requirement | Prometheus Metric(s) & Labels | Source | Grafana Panel (`bscheme-migrations`) |
| --- | --- | --- | --- |
| Job menunggu / berjalan / selesai / gagal | `bscheme_migration_jobs{status="queued\|running\|pending_review\|approved\|success\|failed\|partial\|rejected"}` (gauge), `bscheme_migration_executions_total{type,status}` (counter) | `bscheme-api`, `bscheme-worker` | **Migration Jobs per Status** |
| Waktu tunggu queue & lock | `bscheme_migration_queue_wait_duration_seconds_{bucket,sum,count}{type}`, `bscheme_migration_lock_wait_duration_seconds_{bucket,sum,count}{type,lock_scope}` (`advisory`, `history_table`) | `bscheme-worker` | **Queue & Lock Wait Duration (p50 / p95)** |
| Durasi migration | `bscheme_migration_execution_duration_seconds_{bucket,sum,count}{type,status}` (histogram) | `bscheme-worker` | **Migration Execution Duration (p50 / p95)** |
| Batch, row diproses & retry | `bscheme_migration_batches_processed_total{type}`, `bscheme_migration_batch_rows_processed_total{type}`, `bscheme_migration_retries_total{type,reason}` | `bscheme-worker` | **Batches, Rows Processed & Retries** |

### Database Connection Pool Metrics (Prometheus -> Grafana)

| Requirement | Prometheus Metric(s) & Labels | Source | Grafana Panel (`bscheme-migrations`) |
| --- | --- | --- | --- |
| Koneksi aktif / idle (Platform & Target DB) | `bscheme_db_pool_connections{db_role="platform\|target",state="active\|idle"}` | `bscheme-api`, `bscheme-worker` | **DB Connection Pool — Active vs Idle Connections** |
| Waktu tunggu & jumlah tunggu koneksi | `bscheme_db_pool_wait_duration_seconds_total{db_role}`, `bscheme_db_pool_wait_count_total{db_role}` | `bscheme-api`, `bscheme-worker` | **DB Connection Pool — Connection Wait Time**, **DB Connection Pool — Connection Wait Count** |

### Grafana Provisioning & Dashboard Files

- **Prometheus scrape config** (mounted to `/otel-lgtm/prometheus.yaml`, with `dns_sd_configs` for scaled `worker` replicas): [`observability/prometheus.yml`](observability/prometheus.yml)
- **Dashboard auto-provisioning** (`BScheme` folder in Grafana): [`observability/dashboards.yml`](observability/dashboards.yml)
- **Queue, Migrations & DB Pool Dashboard** (`http://localhost:3000/d/bscheme-migrations`): [`observability/bscheme-migrations.json`](observability/bscheme-migrations.json)
- **Smoke Test Script**: [`scripts/test-observability.sh`](scripts/test-observability.sh)

See [`observability/README.md`](observability/README.md) for architecture details, label cardinality guardrails, screenshots, and verified log/trace correlation and `/metrics` output examples.

## codeql

Automated static analysis runs via GitHub Actions ([`.github/workflows/codeql.yml`](.github/workflows/codeql.yml)) for **Go** and **JavaScript/TypeScript**. Below is the summary of CodeQL alerts reported on `main`:

| Alert | Rule | Severity | Code Location | Potential Impact | Decision |
| --- | --- | --- | --- | --- | --- |
| [#1](https://github.com/Vearance/tubes-wbd/security/code-scanning/1) | `go/cookie-secure-not-set` (CWE-614) | Medium (`warning`) | [`backend/internal/http/middleware/session.go:40-48`](backend/internal/http/middleware/session.go#L40-L48) | Session cookies sent without the `Secure` flag can be transmitted over cleartext HTTP and intercepted by an attacker (session hijacking). | **Not a false positive — Dismissed (`used in tests`).** `Secure` is intentionally configurable via `APP_COOKIE_SECURE` (`CookieOptions.Secure`) to support local HTTP development and automated tests. Production deployments over HTTPS should set `APP_COOKIE_SECURE=true`. This is referred in the specification made by the assistant. |
| [#2](https://github.com/Vearance/tubes-wbd/security/code-scanning/2) | `go/uncontrolled-allocation-size` (CWE-770) | High (`error`) | [`backend/internal/viewer/query.go:129`](backend/internal/viewer/query.go#L129) | Allocating a slice capacity directly from untrusted user input could allow an attacker to trigger excessive memory allocation and cause an OOM Denial of Service (DoS). | **False positive — Dismissed (`false positive`).** `pageSize` is validated (`>= 0`) and clamped to the server-configured `s.config.MaxPageSize` (default `100`) in `normalizePage` before `executeQuery` allocates the slice. |

## contributors

| Nama | NIM |
| --- | --- |
| Jennifer Khang | 13524110 |
| Nathaniel Christian | 13524122 |
| Azri Arzaq Pohan | 13524139 |
