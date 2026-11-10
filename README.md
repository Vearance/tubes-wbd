# BScheme

## description


BScheme is a web-based PostgreSQL management system that helps teams explore schemas and data, manage database access, and review and execute migrations through a simple interface.

Made for IF3110 Web Development Course.

## start server + Platform DB
```bash
docker compose up --build
```

The backend runs at `http://localhost:8080`, and the frontend runs at `http://localhost:5173`.

## start server only
```bash
cd backend
go run ./cmd/server
```

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
- `GET /admin/groups` lists groups.
- `POST /admin/groups` creates a group.
- `GET /admin/groups/{groupId}/members` lists group members.
- `POST /admin/groups/{groupId}/members` adds a user to a group.
- `DELETE /admin/groups/{groupId}/members/{userId}` removes a user from a group.
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
- `POST /migrations/drafts/{draftId}/execute` executes a draft.
- `GET /migrations/drafts/{draftId}/runs` lists execution runs.


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
Group            Members                  Suggested permission on targetdb
Student Council  rudolf, airgroove        View + Edit
Team Spica       specialweek, goldship    View Only
(no group)       tachyon                  none
```

The groups receive no database permission from the seed, because a permission needs a registered managed database and its password is encrypted with `TARGET_DB_ENCRYPTION_KEY`. Sign in as the Platform Admin, register `targetdb` with the connection above, then assign it to `Student Council` with View + Edit and to `Team Spica` with View Only. The platform admin manages these assignments but does not receive target-database access automatically.

The script is safe to run repeatedly and does not overwrite an existing user password. If `APP_USER` or `APP_NAME` is customized in `.env`, replace `platform` in the command with the configured values.

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


## contributors

| Nama | NIM |
| --- | --- |
| Jennifer Khang | 13524110 |
| Nathaniel Christian | 13524122 |
| Azri Arzaq Pohan | 13524139 |
