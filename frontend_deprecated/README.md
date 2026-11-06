# BScheme Frontend

The main frontend is server-rendered with Go `html/template`, Bootstrap 5.3.3 CDN,
custom CSS, and vanilla JavaScript. It runs inside the backend process and uses the
real backend services and session cookie.

## Integrated application

Start the backend and Platform PostgreSQL, then open:

```text
http://localhost:8080/app/login
```

The integrated UI is mounted at `/app`; the JSON API keeps its existing routes.
HTML forms use the backend session cookie `labpro_sucks` and submit the CSRF token
as a hidden form field. The login form first receives a separate pre-authentication
CSRF cookie, because the authenticated session CSRF token does not exist until login
succeeds.

## Mock preview

For visual-only work, the old mock preview can still be started from the repository root:

```sh
go run ./frontend/preview
```

Open `http://localhost:4173/login`. This preview uses hard-coded accounts and
in-memory data; it is not connected to Platform PostgreSQL.

Preview credentials:

| Permission | Username | Password |
| --- | --- | --- |
| View Only | `viewer` | `preview-view` |
| View + Edit | `editor` | `preview-edit` |
| Platform Admin | `admin` | `preview-admin` |

## Mock Preview Routes

### Login and assets

| Method | Route | Behavior |
| --- | --- | --- |
| `GET` | `/` | Redirect to login, databases, or admin Users based on the session |
| `GET` | `/login` | Render the login page |
| `POST` | `/login` | Authenticate, set the session cookie, redirect to `/databases` or `/admin/users` |
| `POST` | `/logout` | Revoke the preview session (CSRF required) |
| `GET` | `/static/*` | Serve CSS, JavaScript, and assets |

### Platform Admin Preview Routes

These are frontend preview routes backed by mock data, not endpoints in the integrated main API. The integrated JSON API contract in [`api/openapi.yaml`](../api/openapi.yaml) covers authentication, platform-admin management, database access, viewer, and migration endpoints. The server-rendered frontend routes remain under `/app` and are documented separately in the repository README.

| Method | Route | Behavior |
| --- | --- | --- |
| `GET` | `/admin` | Redirect to Users |
| `GET` | `/admin/users`, `/admin/users/new`, `/admin/users/{username}/edit` | List and render user forms |
| `POST` | `/admin/users/save` | Save a preview user (CSRF required) |
| `GET` | `/admin/groups`, `/admin/groups/new`, `/admin/groups/{groupID}` | List and render group forms |
| `POST` | `/admin/groups/save` | Save preview membership and access (CSRF required) |
| `GET` | `/admin/databases`, `/admin/databases/new`, `/admin/databases/{databaseID}/edit` | List and render connection forms |
| `POST` | `/admin/databases/save` | Save a preview connection (CSRF required) |


The admin preview supports user forms, group membership and database access, and connection forms. Users may belong to multiple groups. Saved changes last only until the preview restarts; newly created users are not added to the hardcoded login accounts. Test connection checks form fields only and does not contact PostgreSQL.

## Testable States

| State | URL |
| --- | --- |
| Normal | `/login` |
| Invalid credentials | `/login?state=invalid` |
| Session expired | `/login?state=expired` |
| Server error | `/login?state=server-error` |
| Empty/invalid fields | `/login?state=fields` |

### Regular-user preview routes

Access is determined by the logged-in preview account, not a query parameter.

| Method | Route | Behavior |
| --- | --- | --- |
| `GET` | `/databases` | List accessible preview databases |
| `GET` | `/databases/{databaseID}` | Table workspace; `schema`, `table`, and `tab` query parameters select the view |
| `GET` | `/databases/{databaseID}/schemas/{schema}` | Schema overview |
| `GET` | `/databases/{databaseID}/query` | Read-only query editor |
| `POST` | `/databases/{databaseID}/query` | Run a bounded read-only query or request its execution plan (CSRF required) |
| `GET` | `/databases/{databaseID}/migrations` | Migration history |
| `GET` | `/databases/{databaseID}/migrations/new` | New migration form; View Only receives `403` |
| `POST` | `/databases/{databaseID}/migrations` | Simulate saving a migration (View + Edit and CSRF required) |
| `GET` | `/databases/{databaseID}/migrations/{migrationID}` | Migration detail |

View Only can browse tables, run bounded read-only queries, inspect execution plans, and view migration history/detail. Opening New Migration returns `403`. View + Edit additionally sees and can submit migration actions.

To test access checking:

1. Open `/databases` without logging in; it redirects to `/login?state=expired`.
2. Login as `viewer`, then manually open `/databases/1/migrations/new`; it returns the denied page with status `403`.
3. Logout and login as `editor`; the same URL opens the migration form.
4. Editing `?mode=edit` does not elevate a View Only session.
