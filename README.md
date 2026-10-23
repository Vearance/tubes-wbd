# tubes-wbd

## start server + Platform DB
```bash
docker compose up --build
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

## integration tests

Integration tests use Testcontainers and require Docker:

```bash
go test -tags=integration ./...
```
