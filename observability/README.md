# BScheme Observability Stack

Centralized logging, distributed tracing, Prometheus metrics, and auto-provisioned Grafana dashboards for the `bscheme-api` (`cmd/server`) and `bscheme-worker` (`cmd/worker`) services.

## Architecture

```mermaid
flowchart LR
  API["bscheme-api (:8080)"] -->|OTLP HTTP :4318 traces & logs| COL["OTel Collector"]
  WRK["bscheme-worker (:9091)"] -->|OTLP HTTP :4318 traces & logs| COL

  COL -->|OTLP gRPC :4317| TEMPO["Grafana Tempo (:3200)"]
  COL -->|OTLP HTTP /otlp/v1/logs| LOKI["Grafana Loki (:3100)"]
  COL -->|Metrics :8889| PROM["Prometheus (:9090)"]

  API -->|/metrics| PROM
  WRK -->|/metrics| PROM
  RMQ["RabbitMQ (:15692)"] -->|/metrics| PROM

  PROM --> GRAF["Grafana (:3000)"]
  TEMPO --> GRAF
  LOKI --> GRAF
```

## Components & Files

| Component | Purpose | Config Path |
| --- | --- | --- |
| OpenTelemetry Collector | Receives OTLP traces & logs (`4317`/`4318`) and routes to Tempo, Loki, and Prometheus | [`otel-collector/config.yaml`](otel-collector/config.yaml) |
| Grafana Loki 3.4 | Centralized log store receiving native OTLP logs at `/otlp/v1/logs` with structured metadata | [`loki/loki.yaml`](loki/loki.yaml) |
| Grafana Tempo 2.7 | Distributed trace store across API -> Outbox -> RabbitMQ -> Worker -> PostgreSQL | [`tempo/tempo.yaml`](tempo/tempo.yaml) |
| Prometheus | Scrapes `internal:8080/metrics`, `worker:9091/metrics`, `rabbitmq:15692/metrics`, and `otel-collector` | [`prometheus/prometheus.yml`](prometheus/prometheus.yml) |
| Prometheus Alert Rules | Alerts on down targets, 5xx bursts, failed migrations, queue/lock waits, outbox backlog, DB pool saturation | [`prometheus/alerts.yml`](prometheus/alerts.yml) |
| Grafana Datasources | Auto-provisions Prometheus, Tempo (`tracesToLogsV2`), and Loki (`derivedFields` TraceID -> Tempo) | [`grafana/provisioning/datasources/datasources.yml`](grafana/provisioning/datasources/datasources.yml) |
| Grafana Dashboards | Provisioned **BScheme — Queue, Migrations & DB Connection Pool** and **BScheme — API, Auth, Viewer & Runtime Overview** | [`grafana/dashboards/`](grafana/dashboards/) |

## Running the Stack

```bash
cp .env.example .env
docker compose up --build
```

Exposed observability endpoints:

- **Grafana**: `http://localhost:3000` (default `admin` / `admin`; dashboards are under folder **BScheme**)
- **Prometheus**: `http://localhost:9090` (`/targets` and `/alerts`)
- **API `/metrics`**: `http://localhost:8080/metrics`
- **Worker `/metrics`**: `http://localhost:9091/metrics` (`/healthz` for liveness)
- **RabbitMQ Prometheus `/metrics`**: `http://localhost:15692/metrics`
- **Loki / Tempo APIs**: `http://localhost:3100`, `http://localhost:3200`

### Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `OTEL_SERVICE_NAME` | `bscheme-api` / `bscheme-worker` | Logical OTel service name resource attribute |
| `OTEL_SERVICE_VERSION` | `dev` | Build/release version resource attribute |
| `OTEL_ENVIRONMENT` / `APP_ENV` | `development` | Deployment environment resource attribute |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `""` (`http://otel-collector:4318` in Compose) | OTLP/HTTP base URL; when unset, OTLP export is skipped gracefully |
| `OTEL_TRACES_EXPORTER` | `""` (OTLP if endpoint set) | Comma-separated trace exporters: `otlp`, `console` (JSON spans to `stderr`), `otlp,console`, or `none` |
| `OTEL_TRACES_SAMPLER_ARG` | `1.0` | Parent-based trace sampling ratio (`0.0`..`1.0`) |
| `APP_METRICS_ADDR` | `:9091` | Worker HTTP bind address serving `/metrics` and `/healthz` |

## Structured JSON Logging, SQL Lifecycle & Trace Correlation

All application logs are emitted as **structured JSON (`log/slog`) to `stdout`** (and fanned out to OTel Collector -> Loki when OTLP is enabled). Logs are **not persisted in the Platform DB** and do not depend on the SPA. OpenTelemetry spans can also be printed directly to `stderr` by setting `OTEL_TRACES_EXPORTER=console` (or `otlp,console`).

### Log & SQL Attributes

- **Standard attributes on every log record**: `time`, `level`, `msg`, `service`, `event`, `actor` (`user:<id>`, `system:worker`, `system:outbox-relay`, or `anonymous`), `database` (`platform` or `target:<id>`), `status` (`ok`, `rejected`, `error`, `started`, `committed`, `rolled_back`, `retrying`, `queued`), `correlation_id` (from `X-Correlation-ID` / `X-Request-ID` / trace ID), `trace_id`, `span_id`.
- **SQL & transaction attributes**: `operation`, `query` (sanitized), `parameters` (scrubbed, omitted when empty), `duration_ms`, `rows_affected`, `error`, `sqlstate` (from `*pgconn.PgError`), plus `execution_id`, `draft_id`, `attempt`, and `batch_number` when running inside a migration execution or batch.
- **Separate lifecycle events**:
  - Transaction start, commit, and rollback are emitted separately: `sql.tx.started`, `sql.tx.commit`, `sql.tx.rollback` (as well as `sql.tx.failed`).
  - Queries (including read-only Viewer/`EXPLAIN`/history queries, batch cursors/updates, GORM Platform DB queries, retries, and failures) emit separate start and completion/failure events: `sql.query.started` and `sql.query.completed` / `sql.query.failed`.
  - Idle background polling queries (empty outbox ticks, stale recovery ticks, and `/metrics` gauge scrapes) suppress routine SQL logs and spans while still emitting errors and Prometheus metrics.
- **Distributed tracing chain**: `POST /migrations/executions/{executionId}/enqueue` (`SERVER`) → `migration.outbox.publish` (`PRODUCER`, persisted via `ExecutionOutbox.TraceContext`) → `migration.worker.consume` (`CONSUMER`, propagated via AMQP headers) → `migration.execution.run` → `migration.lock.acquire` (`advisory` / `history_table`) → `migration.batch.run` → `db.target.tx.begin` / `db.target.*` / `db.target.tx.commit`.

### Security, Hardening & Redaction Policy

- Passwords, secrets, bearer/CSRF/session tokens, session table query parameters (`sessions`), cookie values (`labpro_sucks`), encryption keys, and embedded URL/DSN credentials (`postgres://user:pass@host`) are scrubbed to `[REDACTED]` in logs, SQL parameters, errors, and span attributes.
- Quoted SQL literals, comments, dollar-quoted blocks, DDL `PASSWORD ...` clauses, and numeric constants are redacted via `SanitizeSQL` / `safeSQLForLog` while preserving positional bind placeholders (`$1`, `$2`).
- Migration status gauge queries executed during `/metrics` scrapes use a 5-second TTL cache (`cmd/server/main.go`); in production deployments, restrict `:8080/metrics` and `:9091/metrics` to internal monitoring networks via reverse-proxy or firewall rules.

## Label Cardinality Policy

All Prometheus metrics strictly restrict labels to bounded enumerations so time series cardinality cannot explode:

| Allowed Bounded Labels | Explicitly Prohibited Labels |
| --- | --- |
| `route` (chi pattern e.g. `/migrations/executions/{executionId}/enqueue`), `method`, `status_code`, `status_class` | `user_id`, `session_id` |
| `type` (`up`, `rollback`, `forward`, `unknown`), `status` (11 lifecycle states), `lock_scope` (`advisory`, `history_table`), `reason` (`retryable_error`, `stale_recovery`, `execution_busy`) | `execution_id` / `job_id`, `draft_id`, `database_id` |
| `db_role` (`platform`, `target`), `state` (`active`, `idle`, `open`, `max_open`, `total`, `max_conns`), `operation` (`select`, `insert`, `update`, `delete`, `ddl`, `lock`, `tx`, `row`, `raw`, `other`) | Raw SQL statements, bind values, or raw request paths with IDs |
| `outcome` / `result` (`success`, `error`, `ack`, `nack`, `reject`, `invalid_credentials`, `user_disabled`) | Stack traces or error messages |

High-cardinality identifiers (`execution_id`, `database_id`, `attempt`, `batch_number`) and redacted SQL shapes (`safeSQLForLog`) are recorded exclusively in structured logs (`slog.*Context`) and OpenTelemetry span attributes.

## Exposed Metrics Reference

### Queue & Migration Metrics
- `bscheme_migration_jobs{status}` — current executions per bounded status (`pending_review`, `rejected`, `approved`, `queued`, `running`, `success`, `failed`, `partial`, `compensation_pending`, `compensating`, `compensated`), emitted by `bscheme-api` from Platform DB.
- `bscheme_migration_outbox_pending` — unpublished records in `migration_execution_outbox`.
- `bscheme_migration_executions_total{type,status}` — terminal execution completions.
- `bscheme_migration_execution_duration_seconds_{bucket,sum,count}{type,status}` — execution runtime distribution.
- `bscheme_migration_retries_total{type,reason}` — retry count (`retryable_error`, `stale_recovery`, `execution_busy`).
- `bscheme_migration_queue_wait_duration_seconds_{bucket,sum,count}{type}` — wait time from `available_at`/`queued_at` until worker claim.
- `bscheme_migration_lock_wait_duration_seconds_{bucket,sum,count}{type,lock_scope}` — time acquiring target advisory locks (`advisory`) and `_dbm.migration_history` exclusive locks (`history_table`).
- `bscheme_migration_batches_processed_total{type}` and `bscheme_migration_batch_rows_processed_total{type}` — committed batch chunks and modified rows.
- `bscheme_migration_outbox_published_total{result}`, `bscheme_migration_worker_messages_total{outcome}`, `bscheme_migration_worker_in_flight{type}`.

### Database Connection Pool & Query Metrics
- `bscheme_db_pool_connections{db_role,state}` — active, idle, open/total, and max connections for both Platform DB (`database/sql.DBStats`) and Target DBs (`pgxpool.Stat`).
- `bscheme_db_pool_wait_count_total{db_role}` and `bscheme_db_pool_wait_duration_seconds_total{db_role}` — cumulative connection pool waits and wait duration.
- `bscheme_targetdb_pools_active` — number of currently open managed target database pools.
- `bscheme_db_queries_total{db_role,operation,status}` and `bscheme_db_query_duration_seconds_{bucket,sum,count}{db_role,operation}`.

### API, Auth, Viewer & Runtime Metrics
- `bscheme_http_requests_total`, `bscheme_http_request_duration_seconds_*`, `bscheme_http_requests_in_flight{method}`
- `bscheme_auth_logins_total{outcome}`, `bscheme_viewer_operations_total{operation,status}`
- Standard `go_*` and `process_*` collectors plus RabbitMQ `rabbitmq_queue_messages_{ready,unacked}`.

## Verified Correlation, Logs, Spans & Metric Examples

### 1. SQL Lifecycle & Read-Only Query Structured Logs

Captured from `TestPGXTracerLogsStartResultCommitRollbackAndSQLState`, demonstrating separate `sql.tx.started`, `sql.query.started`, `sql.query.completed` (read-only `SELECT` with parameters and `rows_affected`), `sql.query.failed` (with `sqlstate`), `sql.tx.rollback`, and `sql.tx.commit` events sharing `trace_id`, `correlation_id`, `actor`, `database`, `execution_id`, `attempt`, and `batch_number`:

```json
{"time":"2026-11-26T13:08:19.471788474+07:00","level":"INFO","msg":"database transaction started","service":"bscheme-worker","event":"sql.tx.started","database":"target:3","status":"started","query":"begin isolation level repeatable read read only","actor":"user:7","correlation_id":"corr-sql-42","trace_id":"eb39a11df7372307ce0892d6cea42ce2","span_id":"00044b7b47544f08","execution_id":42,"draft_id":15,"attempt":1,"batch_number":2}
{"time":"2026-11-26T13:08:19.471920246+07:00","level":"INFO","msg":"sql query started","service":"bscheme-worker","event":"sql.query.started","database":"target:3","operation":"select","status":"started","query":"SELECT id, status FROM public.orders WHERE status = $1 LIMIT [NUMBER]","parameters":["pending"],"actor":"user:7","correlation_id":"corr-sql-42","trace_id":"eb39a11df7372307ce0892d6cea42ce2","span_id":"923b5f19895d4a92","execution_id":42,"draft_id":15,"attempt":1,"batch_number":2}
{"time":"2026-11-26T13:08:19.472009634+07:00","level":"INFO","msg":"sql query completed","service":"bscheme-worker","database":"target:3","operation":"select","query":"SELECT id, status FROM public.orders WHERE status = $1 LIMIT [NUMBER]","duration_ms":0,"parameters":["pending"],"rows_affected":3,"event":"sql.query.completed","status":"ok","actor":"user:7","correlation_id":"corr-sql-42","trace_id":"eb39a11df7372307ce0892d6cea42ce2","span_id":"923b5f19895d4a92","execution_id":42,"draft_id":15,"attempt":1,"batch_number":2}
{"time":"2026-11-26T13:08:19.472128316+07:00","level":"INFO","msg":"sql query started","service":"bscheme-worker","event":"sql.query.started","database":"target:3","operation":"update","status":"started","query":"UPDATE public.orders SET code = '[REDACTED]' WHERE id = $1","parameters":[101],"actor":"user:7","correlation_id":"corr-sql-42","trace_id":"eb39a11df7372307ce0892d6cea42ce2","span_id":"0c187d92745e5108","execution_id":42,"draft_id":15,"attempt":1,"batch_number":2}
{"time":"2026-11-26T13:08:19.472235067+07:00","level":"ERROR","msg":"sql query failed","service":"bscheme-worker","database":"target:3","operation":"update","query":"UPDATE public.orders SET code = '[REDACTED]' WHERE id = $1","duration_ms":0,"parameters":[101],"event":"sql.query.failed","status":"error","error":": duplicate key value violates unique constraint (SQLSTATE 23505)","sqlstate":"23505","actor":"user:7","correlation_id":"corr-sql-42","trace_id":"eb39a11df7372307ce0892d6cea42ce2","span_id":"0c187d92745e5108","execution_id":42,"draft_id":15,"attempt":1,"batch_number":2}
{"time":"2026-11-26T13:08:19.472336581+07:00","level":"INFO","msg":"database transaction rolled back","service":"bscheme-worker","event":"sql.tx.rollback","database":"target:3","status":"rolled_back","query":"rollback","duration_ms":0,"actor":"user:7","correlation_id":"corr-sql-42","trace_id":"eb39a11df7372307ce0892d6cea42ce2","span_id":"e34ece2be74ce484","execution_id":42,"draft_id":15,"attempt":1,"batch_number":2}
{"time":"2026-11-26T13:08:19.472378346+07:00","level":"INFO","msg":"database transaction committed","service":"bscheme-worker","event":"sql.tx.commit","database":"target:3","status":"committed","query":"commit","duration_ms":0,"actor":"user:7","correlation_id":"corr-sql-42","trace_id":"eb39a11df7372307ce0892d6cea42ce2","span_id":"71242908ac20b669","execution_id":42,"draft_id":15,"attempt":1,"batch_number":2}
```

### 2. End-to-End Trace & Log Correlation (`API -> Outbox -> RabbitMQ -> Worker -> Lock -> Batch -> DB`)

Captured from `TestEndToEndTraceAndConsoleSpanExport`, showing W3C `traceparent` + `correlation_id` surviving `ExecutionOutbox.TraceContext` and RabbitMQ `amqp.Table` headers, producing correlated structured logs and console JSON spans (`OTEL_TRACES_EXPORTER=console`) across the entire chain:

**Structured JSON logs (`stdout`)**:

```json
{"time":"2026-11-26T13:08:19.473354551+07:00","level":"INFO","msg":"migration execution queued","service":"bscheme-api","event":"migration.execution.queued","status":"queued","actor":"user:7","database":"target:3","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"ca06bb1a74a222a3","execution_id":42,"draft_id":15}
{"time":"2026-11-26T13:08:19.473535312+07:00","level":"INFO","msg":"published migration execution to queue","service":"bscheme-api","event":"migration.outbox.published","queue":"migration.executions","status":"ok","duration_ms":3.412,"actor":"system:outbox-relay","database":"platform","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"d293452fcc70efdc","execution_id":42}
{"time":"2026-11-26T13:08:19.473599625+07:00","level":"INFO","msg":"migration execution started","service":"bscheme-worker","event":"migration.execution.started","queue_wait_ms":18,"status":"started","actor":"user:7","database":"target:3","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"aa080b7e08264808","execution_id":42,"draft_id":15,"attempt":1}
{"time":"2026-11-26T13:08:19.47362904+07:00","level":"INFO","msg":"migration target lock acquired","service":"bscheme-worker","event":"migration.lock.acquired","lock_scope":"advisory","status":"ok","duration_ms":1.205,"actor":"user:7","database":"target:3","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"40a2d88c6a48cb9f","execution_id":42,"draft_id":15,"attempt":1}
{"time":"2026-11-26T13:08:19.473683792+07:00","level":"INFO","msg":"database transaction started","service":"bscheme-worker","event":"sql.tx.started","database":"target:3","status":"started","query":"begin","actor":"user:7","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"48a29b4373d0a83e","execution_id":42,"draft_id":15,"attempt":1,"batch_number":1}
{"time":"2026-11-26T13:08:19.473791269+07:00","level":"INFO","msg":"sql query started","service":"bscheme-worker","event":"sql.query.started","database":"target:3","operation":"update","status":"started","query":"UPDATE public.orders SET status = '[REDACTED]' WHERE id = ANY($1)","parameters":[[101,102]],"actor":"user:7","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"aefcb2a8deb00143","execution_id":42,"draft_id":15,"attempt":1,"batch_number":1}
{"time":"2026-11-26T13:08:19.473878702+07:00","level":"INFO","msg":"sql query completed","service":"bscheme-worker","database":"target:3","operation":"update","query":"UPDATE public.orders SET status = '[REDACTED]' WHERE id = ANY($1)","duration_ms":0,"parameters":[[101,102]],"rows_affected":2,"event":"sql.query.completed","status":"ok","actor":"user:7","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"aefcb2a8deb00143","execution_id":42,"draft_id":15,"attempt":1,"batch_number":1}
{"time":"2026-11-26T13:08:19.473980714+07:00","level":"INFO","msg":"database transaction committed","service":"bscheme-worker","event":"sql.tx.commit","database":"target:3","status":"committed","query":"commit","duration_ms":0,"actor":"user:7","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"deb71cbaed91e008","execution_id":42,"draft_id":15,"attempt":1,"batch_number":1}
{"time":"2026-11-26T13:08:19.474033776+07:00","level":"INFO","msg":"migration batch committed","service":"bscheme-worker","event":"migration.batch.committed","rows_affected":2,"status":"committed","duration_ms":12.6,"actor":"user:7","database":"target:3","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"3b4831946d19eefb","execution_id":42,"draft_id":15,"attempt":1,"batch_number":1}
{"time":"2026-11-26T13:08:19.474069104+07:00","level":"INFO","msg":"migration execution succeeded","service":"bscheme-worker","event":"migration.execution.completed","execution_type":"up","execution_status":"success","version":7,"status":"ok","duration_ms":19.8,"actor":"user:7","database":"target:3","correlation_id":"corr-e2e-42","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"aa080b7e08264808","execution_id":42,"draft_id":15,"attempt":1}
```

**OpenTelemetry Console Span Exporter JSON (`stderr`)**:

```json
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"ca06bb1a74a222a3","name":"POST /migrations/executions/{executionId}/enqueue","kind":"server","start_time":"2026-11-26T06:08:19.473348383Z","end_time":"2026-11-26T06:08:19.473398575Z","duration_ms":0.05,"status_code":"Unset","attributes":{"http.route":"/migrations/executions/{executionId}/enqueue"}}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"d293452fcc70efdc","parent_span_id":"ca06bb1a74a222a3","name":"migration.outbox.publish","kind":"producer","start_time":"2026-11-26T06:08:19.473530497Z","end_time":"2026-11-26T06:08:19.4735721Z","duration_ms":0.041,"status_code":"Unset","attributes":{"messaging.system":"rabbitmq"}}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"40a2d88c6a48cb9f","parent_span_id":"aa080b7e08264808","name":"migration.lock.acquire","kind":"internal","start_time":"2026-11-26T06:08:19.473623926Z","end_time":"2026-11-26T06:08:19.473653444Z","duration_ms":0.029,"status_code":"Unset","attributes":{"migration.lock_scope":"advisory"}}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"48a29b4373d0a83e","parent_span_id":"3b4831946d19eefb","name":"db.target.tx.begin","kind":"client","start_time":"2026-11-26T06:08:19.473679319Z","end_time":"2026-11-26T06:08:19.473711443Z","duration_ms":0.032,"status_code":"Unset","attributes":{"db.operation.name":"tx","db.role":"target","db.rows_affected":0,"db.system.name":"postgresql","db.target_database_id":3,"migration.attempt":1,"migration.batch_number":1,"migration.execution_id":42}}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"aefcb2a8deb00143","parent_span_id":"3b4831946d19eefb","name":"db.target.update","kind":"client","start_time":"2026-11-26T06:08:19.473786889Z","end_time":"2026-11-26T06:08:19.473938606Z","duration_ms":0.151,"status_code":"Unset","attributes":{"db.operation.name":"update","db.role":"target","db.rows_affected":2,"db.system.name":"postgresql","db.target_database_id":3,"migration.attempt":1,"migration.batch_number":1,"migration.execution_id":42}}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"deb71cbaed91e008","parent_span_id":"3b4831946d19eefb","name":"db.target.tx.commit","kind":"client","start_time":"2026-11-26T06:08:19.473970937Z","end_time":"2026-11-26T06:08:19.474006176Z","duration_ms":0.035,"status_code":"Unset","attributes":{"db.operation.name":"tx","db.role":"target","db.rows_affected":0,"db.system.name":"postgresql","db.target_database_id":3,"migration.attempt":1,"migration.batch_number":1,"migration.execution_id":42}}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"3b4831946d19eefb","parent_span_id":"aa080b7e08264808","name":"migration.batch.run","kind":"internal","start_time":"2026-11-26T06:08:19.473668623Z","end_time":"2026-11-26T06:08:19.474057648Z","duration_ms":0.389,"status_code":"Unset","attributes":{"migration.batch_number":1}}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"aa080b7e08264808","parent_span_id":"d2a0a141bfc3ba0c","name":"migration.execution.run","kind":"internal","start_time":"2026-11-26T06:08:19.473594557Z","end_time":"2026-11-26T06:08:19.474096155Z","duration_ms":0.501,"status_code":"Unset"}
{"type":"otel_span","service":"bscheme","trace_id":"9871a7b640c675d32f440379d6495659","span_id":"d2a0a141bfc3ba0c","parent_span_id":"d293452fcc70efdc","name":"migration.worker.consume","kind":"consumer","start_time":"2026-11-26T06:08:19.473592256Z","end_time":"2026-11-26T06:08:19.474101143Z","duration_ms":0.508,"status_code":"Unset","attributes":{"messaging.system":"rabbitmq"}}
```

LogQL correlation query in Grafana -> Explore -> **Loki**:

```logql
{service_name=~"bscheme-api|bscheme-worker"} | trace_id = "9871a7b640c675d32f440379d6495659"
```

Every log line in Loki exposes a **View Trace in Tempo** link via `derivedFields`, and every Tempo span exposes **Logs for this span** via `tracesToLogsV2`.

### 3. Verified Prometheus `/metrics` Exposition Excerpt

Captured from `TestPrometheusMetricsExpositionAndCardinalityGuard`:

```prometheus
bscheme_auth_logins_total{outcome="success"} 1
bscheme_db_pool_connections{db_role="platform",state="active"} 0
bscheme_db_pool_connections{db_role="platform",state="idle"} 0
bscheme_db_pool_connections{db_role="platform",state="max_open"} 0
bscheme_db_pool_connections{db_role="platform",state="open"} 0
bscheme_db_pool_connections{db_role="target",state="active"} 3
bscheme_db_pool_connections{db_role="target",state="idle"} 4
bscheme_db_pool_connections{db_role="target",state="max_conns"} 10
bscheme_db_pool_connections{db_role="target",state="total"} 7
bscheme_db_pool_wait_count_total{db_role="platform"} 0
bscheme_db_pool_wait_count_total{db_role="target"} 1
bscheme_db_pool_wait_duration_seconds_total{db_role="platform"} 0
bscheme_db_pool_wait_duration_seconds_total{db_role="target"} 0.045
bscheme_db_queries_total{db_role="platform",operation="select",status="ok"} 1
bscheme_db_queries_total{db_role="target",operation="ddl",status="ok"} 1
bscheme_http_requests_total{method="POST",route="/migrations/executions/{executionId}/enqueue",status_class="2xx",status_code="202"} 1
bscheme_http_requests_in_flight{method="POST"} 0
bscheme_migration_jobs{status="pending_review"} 1
bscheme_migration_jobs{status="rejected"} 0
bscheme_migration_jobs{status="approved"} 2
bscheme_migration_jobs{status="queued"} 3
bscheme_migration_jobs{status="running"} 1
bscheme_migration_jobs{status="success"} 14
bscheme_migration_jobs{status="failed"} 1
bscheme_migration_jobs{status="partial"} 0
bscheme_migration_executions_total{status="success",type="up"} 1
bscheme_migration_execution_duration_seconds_sum{status="success",type="up"} 2.4
bscheme_migration_execution_duration_seconds_count{status="success",type="up"} 1
bscheme_migration_queue_wait_duration_seconds_sum{type="up"} 0.12
bscheme_migration_queue_wait_duration_seconds_count{type="up"} 1
bscheme_migration_lock_wait_duration_seconds_sum{lock_scope="advisory",type="up"} 0.018
bscheme_migration_lock_wait_duration_seconds_sum{lock_scope="history_table",type="up"} 0.009
bscheme_migration_retries_total{reason="retryable_error",type="up"} 1
bscheme_migration_batches_processed_total{type="up"} 1
bscheme_migration_batch_rows_processed_total{type="up"} 500
bscheme_migration_outbox_pending 2
bscheme_migration_outbox_published_total{result="success"} 1
bscheme_migration_worker_messages_total{outcome="ack"} 1
bscheme_migration_worker_in_flight{type="up"} 0
bscheme_targetdb_pools_active 2
bscheme_viewer_operations_total{operation="table_data",status="ok"} 1
```
