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
