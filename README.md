# tubes-wbd

## start server + Platform DB
```bash
docker compose up --build
```

The server-rendered frontend is served by the same backend at:

```text
http://localhost:8080/app/login
```

## start server only
```bash
go run ./cmd/server
```

## OpenAPI

The API contract is defined in [`api/openapi.yaml`](api/openapi.yaml). Generated
handlers and models are written to `backend/http/api.gen.go`; do not edit that
file manually.

Regenerate the OpenAPI code after changing the contract:

```bash
go generate ./backend/http
```

## authentication endpoints

- `POST /auth/login` creates the session cookie and returns a CSRF token.
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

These endpoints require a valid session and database permission. Viewer reads require View Only or View + Edit; migration creation/update/execution requires View + Edit:

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


## server-rendered frontend

The frontend uses the backend's real authentication, authorization, managed-database, viewer, and migration services. It is mounted at `/app` so the existing JSON API routes remain unchanged. The session cookie is shared with the backend and HTML mutation forms include the backend CSRF token.

The old `frontend/preview` command remains a mock-only visual preview and is not connected to Platform PostgreSQL.

## integration tests

Integration tests use Testcontainers and require Docker:

```bash
go test -tags=integration ./...
```
