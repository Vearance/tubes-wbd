# BScheme Frontend

Go `html/template` + Bootstrap 5.3.3 CDN + custom CSS + vanilla JavaScript.

## Preview

From the repository root:

```sh
go run ./frontend/preview
```

Open `http://localhost:4173/login`.

Preview credentials:

| Permission | Username | Password |
| --- | --- | --- |
| View Only | `viewer` | `preview-view` |
| View + Edit | `editor` | `preview-edit` |

## Login Contract

### Routes

| Method | Route | Behavior |
| --- | --- | --- |
| `GET` | `/login` | Render the login page |
| `POST` | `/login` | Authenticate, set the session cookie, redirect to `/databases` |
| `GET` | `/static/*` | Serve CSS, JavaScript, and assets |

## Testable States

| State | URL |
| --- | --- |
| Normal | `/login` |
| Invalid credentials | `/login?state=invalid` |
| Session expired | `/login?state=expired` |
| Server error | `/login?state=server-error` |
| Empty/invalid fields | `/login?state=fields` |

## Regular-User Flow

Access is determined by the logged-in preview account, not a query parameter.

| Page | URL |
| --- | --- |
| Database list | `/databases` |
| Table workspace | `/databases/1` |
| Schema viewer | `/databases/1/schemas/public` |
| Query | `/databases/1/query` |
| Migration history | `/databases/1/migrations` |
| Migration detail | `/databases/1/migrations/4` |
| New migration | `/databases/1/migrations/new` |

View Only can browse tables, run the read-only query preview, and view migration history/detail. Opening New Migration returns `403`. View + Edit additionally sees and can submit migration actions.

To test access checking:

1. Open `/databases` without logging in; it redirects to `/login?state=expired`.
2. Login as `viewer`, then manually open `/databases/1/migrations/new`; it returns the denied page with status `403`.
3. Logout and login as `editor`; the same URL opens the migration form.
4. Editing `?mode=edit` does not elevate a View Only session.