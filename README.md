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

Stress testing dilakukan untuk menguji ketahanan **BScheme API** (`bscheme-api`) dan **migration worker** saat beban dinaikkan bertahap di atas beban normal lalu diturunkan kembali untuk mengamati pemulihan sistem (*recovery*). Pengujian menjalankan akses API dan pengiriman job migration secara **bersamaan** pada **1 target database yang sama** maupun **3 target database berbeda**.

### Menjalankan Stress Test

```bash
docker compose up --build -d --remove-orphans --scale worker=3
bash loadtest/run-stress-test.sh
```

> Untuk validasi singkat (durasi tahap 20s): `bash loadtest/run-stress-test.sh --quick`. Runner otomatis memakai `k6` lokal jika tersedia, atau menjalankan container `grafana/k6:0.54.0` pada network Docker Compose yang sama jika `k6` belum terpasang di host.

### Ringkasan Script & Skenario

- **Bootstrap fixture ([`loadtest/run-stress-test.sh`](loadtest/run-stress-test.sh))**:
  1. Membuat dan mengisi ulang (`scripts/seed-target.sql`) 4 target database pada `test-db`: `k6_same_db` (1 database untuk skenario *same database*) dan `k6_diff_db_1..3` (3 database untuk skenario *different databases*).
  2. Menyiapkan user & group leader `k6_tester` (`k6-stress-team`) dengan izin eksklusif `view_edit` pada ke-4 target database serta `40` sesi cookie/CSRF terpisah agar *request* konkuren tidak saling mengunci baris sesi tunggal.
  3. Membuat dan menyetujui (*pre-approve*) **100 migration Same-DB** dan **180 migration Diff-DB** (`status = approved`) **sebelum** pengujian k6 dimulai. Setiap SQL migration menjalankan `CREATE TABLE IF NOT EXISTS public.k6_migration_audit ...; INSERT ... 50 rows; SELECT pg_sleep(1.2);` disertai `down_sql` yang dapat di-*rollback*.
- **Script pengujian ([`loadtest/stress-test.js`](loadtest/stress-test.js))**:
  Menjalankan 3 skenario paralel selama total `240s` (4 tahap × `60s`, masing-masing `10s` ramp + `50s` hold, ditambah *graceful drain*):
  1. `api_access` (`ramping-vus`): Akses endpoint terautentikasi (`GET /databases`, `/schemas`, `/tables`, `/table-structure`, `/table-rows`, `/migrations/history`, dan `POST /query`). Mencatat `api_req_duration` (`p50`/`p95`), `api_reqs`, dan `api_error_rate` per tahap tanpa tercampur *request polling* worker.
  2. `migrations_same_db` (`ramping-arrival-rate`): Mengirim migration *pre-approved* (`POST /migrations/executions/{executionId}/enqueue`) khusus ke `k6_same_db`, lalu memantau `GET /migrations/executions/{executionId}` setiap `500ms` sampai status terminal.
  3. `migrations_diff_db` (`ramping-arrival-rate`): Mengirim migration *pre-approved* secara *round-robin* ke `k6_diff_db_1`, `k6_diff_db_2`, dan `k6_diff_db_3`, lalu memantau hingga status terminal.
- **Pemisahan `202 Accepted` vs Eksekusi Migration Selesai**:
  Respons `POST /enqueue` (`202 Accepted`) hanya dicatat sebagai `migration_enqueue_accepted`. Keberhasilan dan throughput penyelesaian job (`migration_jobs_succeeded`, `migration_job_error_rate`) hanya dihitung setelah *polling* mengembalikan status terminal `status == "success"`, dengan waktu tunggu antrean (`started_at - queued_at`) dan durasi eksekusi (`finished_at - started_at`) diambil langsung dari timestamp server pada `MigrationExecution`.

### Profil Beban & Threshold k6

Beban normal tim ditetapkan pada **10 VU API** (~165 req/s) + **13 job migration/menit** (`4/min` Same-DB + `9/min` Diff-DB), lalu dinaikkan bertahap ke **High** (~8× API / ~4.2× migration) → **Peak Stress** (**220 VU API / 22× normal**, **138 job/menit / 10.6× normal**) → **Recovery** (turun kembali ke beban normal):

| Threshold Metric | Aturan SLO | Nilai Aktual | Status |
| --- | --- | --- | --- |
| `http_req_duration{kind:api}` | `p(95) < 500 ms` | `95.7 ms` | **PASS** |
| `api_req_duration` | `p(95) < 500 ms` | `95.7 ms` | **PASS** |
| `api_duration_normal` | `p(95) < 150 ms` | `6.9 ms` | **PASS** |
| `api_duration_recovery` | `p(95) < 250 ms` | `54.1 ms` | **PASS** |
| `http_req_failed` | `rate < 1.00%` | `0.00%` | **PASS** |
| `api_error_rate` | `rate < 1.00%` | `0.00%` | **PASS** |
| `migration_job_error_rate` | `rate < 5.00%` | `0.00%` | **PASS** |

### Hasil Pengujian per Tingkat Beban

Data lengkap tersimpan di [`loadtest/results/summary.json`](loadtest/results/summary.json) (dijalankan dengan `--scale worker=3`, total `238,083` HTTP request / `991.73 req/s`, `234,614` API request / `977.56 req/s`, dan `213 / 213` migration selesai sukses).

| Tingkat Beban | Konfigurasi Beban | API Throughput | API `p50` | API `p95` | Error Rate (API / Job) | Job Accepted / Selesai | Queue Wait `p50` / `p95` | Durasi Exec `p50` / `p95` | Throughput Job |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **Normal** (`0–60s`) | `10` VU API; `4/m` Same + `9/m` Diff | `165.0 req/s` | `4.5 ms` | `6.9 ms` | `0.00%` / `0.00%` | `11` / `11` | `356.0 ms` / `689.5 ms` | `1.23 s` / `1.23 s` | `11.0 jobs/min` (`0.18/s`) |
| **High** (`60–120s`) | `80` VU API; `18/m` Same + `36/m` Diff | `1,248.8 req/s` | `5.6 ms` | `24.3 ms` | `0.00%` / `0.00%` | `51` / `51` | `550.0 ms` / `939.5 ms` | `1.24 s` / `1.26 s` | `51.0 jobs/min` (`0.85/s`) |
| **Peak Stress** (`120–180s`) | `220` VU API; `48/m` Same + `90/m` Diff | `2,089.1 req/s` | `39.6 ms` | `119.3 ms` | `0.00%` / `0.00%` | `129` / `129` | `8.09 s` / `18.48 s` | `1.25 s` / `1.26 s` | `129.0 jobs/min` (`2.15/s`) |
| **Recovery** (`180–240s`) | Ramp turun `220→10` VU; `4/m` Same + `9/m` Diff | `407.3 req/s` | `7.5 ms` | `54.1 ms` | `0.00%` / `0.00%` | `22` / `22` | `16.16 s` / `19.33 s` *(min `19 ms`)* | `1.23 s` / `1.23 s` | `22.0 jobs/min` (`0.37/s`) |
| **Overall** (`240.1s`) | Seluruh skenario + drain | `977.6 req/s` | `21.6 ms` | `95.7 ms` | `0.00%` / `0.00%` | `213` / `213` | `2.79 s` / `18.77 s` | `1.25 s` / `1.26 s` | `53.25 jobs/min` (`0.89/s`) |

#### Perbandingan Same Database vs Different Databases

| Mode Target | Job Accepted / Selesai / Gagal | Job Error Rate | Queue Wait `p50` | Queue Wait `p95` | Queue Wait Max | Exec Dur `p50` / `p95` | E2E `p95` | Throughput Selesai |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **Same DB (`k6_same_db`)** | `73` / `73` / `0` | `0.00%` | `4.05 s` | `22.75 s` | `31.18 s` | `1.25 s` / `1.27 s` | `23.98 s` | `18.25 jobs/min` (`0.30/s`) |
| **Diff DB (`k6_diff_db_1..3`)** | `140` / `140` / `0` | `0.00%` | `2.24 s` | `18.53 s` | `19.33 s` | `1.25 s` / `1.26 s` | `19.76 s` | `35.00 jobs/min` (`0.58/s`) |

### Analisis Hasil per Tingkat Beban

1. **Normal (`0–60s`)**:
   Pada beban normal (`10` VU, `165.0 req/s`), API merespons sangat cepat dengan `p50 = 4.5 ms` dan `p95 = 6.9 ms` tanpa error (`0.00%`). Untuk migration (`11` job), waktu tunggu antrean didominasi oleh siklus polling `OutboxRelay` (interval `1s`, sehingga rata-rata menunggu ~`0.3–0.7s`: `p50 = 356.0 ms`, `p95 = 689.5 ms`), sementara durasi eksekusi konsisten di `~1.23 s`.
2. **High (`60–120s`)**:
   Saat beban naik ke `80` VU (`1,248.8 req/s`, `7.6×` throughput normal), `p50` API tetap rendah (`5.6 ms`), namun `p95` mulai naik menjadi `24.3 ms` (`3.5×` normal) karena *connection pool* Platform DB (`APP_MAX_OPEN_CONNS=10`) dan Target DB (`MaxConns=5` per database) mulai mengalami antrean koneksi singkat pada persentil ekor. Pipeline migration (`51` job) tetap stabil tanpa backlog berlebih (`QWait p50 = 550.0 ms`, `p95 = 939.5 ms`) karena total laju kedatangan (`54/min`) masih di bawah kapasitas gabungan 3 worker.
3. **Peak Stress (`120–180s`)**:
   Pada beban puncak `220` VU (`2,089.1 req/s`, `12.7×` throughput normal), pool koneksi database mengalami saturasi sehingga latensi API melonjak menjadi **`p50 = 39.6 ms`** (`8.8×` normal) dan **`p95 = 119.3 ms`** (`17.3×` normal, `max = 480.7 ms`), meski API tidak mengalami *dropped/failed request* (`0.00%` error).
   Pada sisi worker, laju total pengiriman migration naik hingga `138 jobs/min` (`129` job diterima pada tahap ini), mendekati batas kapasitas 3 worker (`~144 jobs/min` pada durasi `1.25s/job`). Di titik ini perbedaan **Same DB vs Diff DB** terlihat jelas:
   - Pada **`k6_same_db`**, constraint unik `ux_migration_executions_active_database_id` + *advisory lock* menjaga eksekusi berjalan serial (1 job per database pada satu waktu), namun pesan yang bertabrakan mengembalikan `ErrExecutionBusy`, sehingga worker menahan `Qos(1)` selama backoff `2s` sebelum `Nack(requeue=true)`. Akibatnya antrean Same-DB menumpuk hingga `QWait p95 = 22.75 s` (`max = 31.18 s`).
   - Pada **`k6_diff_db_1..3`**, 3 worker mengeksekusi 3 database berbeda secara paralel menghasilkan throughput hampir **2× lebih tinggi** (`35.0` vs `18.25 jobs/min`) dan median waktu tunggu hampir **2× lebih rendah** (`2.24 s` vs `4.05 s`). Ekor `QWait p95` Diff-DB tetap naik (`18.53 s`) karena seluruh worker berbagi satu queue RabbitMQ `migration.executions` sehingga backoff `2s` dari retry Same-DB sempat memperlambat pengambilan pesan Diff-DB (*head-of-line delay*).
4. **Recovery (`180–240s`)**:
   Begitu beban API diturunkan kembali dari `220 → 10` VU, latensi API langsung pulih mendekati baseline (`p50` turun dari `39.6 ms` ke **`7.5 ms`**, `p95` turun dari `119.3 ms` ke **`54.1 ms`**) dengan `0.00%` error dan memenuhi threshold `api_duration_recovery < 250 ms`. Pada awal fase *recovery*, job migration pertama yang masuk masih terdampak pengurasan backlog dari fase *peak stress* (sehingga `QWait p50` tahap recovery tercatat tinggi di `16.16 s`), lalu antrean terkuras tuntas dalam jendela 60 detik tersebut hingga job di akhir fase *recovery* kembali ke `min = 19 ms`, dan seluruh `213 / 213` eksekusi selesai `100%` sukses tanpa ada job gagal atau *timeout*.


## contributors

| Nama | NIM |
| --- | --- |
| Jennifer Khang | 13524110 |
| Nathaniel Christian | 13524122 |
| Azri Arzaq Pohan | 13524139 |
