# googleauth

Multi-account OAuth2 for Google installed apps: the authorization-code flow with a loopback
redirect, a token store the consumer owns, and auto-refreshing Gmail / Calendar / Tasks clients
per account.

It exists because a personal tool routinely has more than one Google account — a personal one
and a work one — and the official helpers assume a single set of credentials per process.
Accounts are keyed by a stable local name (`personal`, `work`), not by email: the email is what
an account turns out to hold, and it must be able to change without orphaning a token.

```
go get github.com/original-david-knight/go_wild/googleauth
```

## Use

```go
cfg, err := googleauth.LoadClientConfig("~/.config/lifedash/google_client.json")
store := googleauth.NewFileTokenStore("~/.config/lifedash")
reg := googleauth.NewRegistry(cfg, store) // no scopes = gmail.modify, calendar.readonly, tasks, userinfo.email

// One-off, interactive: opens the consent page and waits for the redirect.
res, err := reg.Connect(ctx, "personal", browser.Open, googleauth.ConnectOptions{})
// res.Email reports which Google account actually consented.

// Thereafter, per account, non-interactive.
svc, err := reg.Gmail(ctx, "personal")
```

`LoadClientConfig` reads the Cloud console's client-secret JSON. A **Desktop app** client is
required; a web client returns `ErrWebClient`, because a web client needs an exactly
pre-registered redirect URI and an installed app's redirect is a loopback port chosen at runtime.
Missing `auth_uri` / `token_uri` fall back to Google's endpoints.

`Connect` binds `127.0.0.1` only — the authorization code arrives in the redirect's query string
and must never be reachable off the machine — passes a random `state`, and requests
`access_type=offline` with a forced prompt so a re-consent always reissues a refresh token. A
grant that carries no refresh token is rejected rather than stored: it would look connected and
then die at the next restart.

`TokenSource(ctx, account)` refreshes automatically and writes every rotated token back through
the store; a refresh response that omits `refresh_token` keeps the existing one. `Gmail`,
`Calendar` and `Tasks` are thin bindings of the official `google.golang.org/api` services to one
account's token source — deliberately thin, so a consumer that must police what may be called can
wrap exactly that seam.

## Tokens

Nothing here decides where tokens live. `TokenStore` is the seam; `FileTokenStore` writes one
JSON per account as `google_token_<account>.json`, replacing it by atomic rename at mode 0600 in
a directory created at 0700. `MemoryTokenStore` is the non-persistent equivalent.

Account names become filenames, so they are normalised to lower-case letters, digits, `-` and `_`.
Anything else is refused before any file is touched.

## Scopes

Ask for everything the application will ever need at first consent: a later scope addition forces
the user through the whole grant again, and a background job cannot ask.

| Constant | Scope | Why |
| --- | --- | --- |
| `ScopeGmailModify` | `gmail.modify` | Read, label, archive, draft. There is no narrower scope that permits drafting; it does not permit sending. |
| `ScopeCalendarReadonly` | `calendar.readonly` | Deliberately read-only. |
| `ScopeTasks` | `tasks` | Read-write: two-way task sync needs it. |
| `ScopeUserinfoEmail` | `userinfo.email` | Reports which account consented. |

`ConnectResult.Scopes` is what the grant actually carries, which is not necessarily what was
requested — a user can decline individual boxes.

## Tests

```
go vet ./... && go test ./... && go test -race ./...
```

The suite runs offline against `httptest` stubs for the token and userinfo endpoints, and
installs an HTTP client that refuses any non-loopback request, so no test can pass by reaching
the real Google. No credentials and no stored token are needed to run it.
