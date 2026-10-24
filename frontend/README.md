# BScheme Frontend

Go `html/template` + Bootstrap 5.3.3 CDN + custom CSS + vanilla JavaScript.

## Preview

From the repository root:

```sh
go run ./frontend/preview
```

Open `http://localhost:4173/login`.

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