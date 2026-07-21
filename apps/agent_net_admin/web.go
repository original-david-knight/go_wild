package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/original-david-knight/go_wild/data"
)

type adminWeb struct {
	db   gowild_data.Database
	tmpl *template.Template
}

type accountsPageData struct {
	Accounts []accountRow
	Query    string
	Notice   string
	Error    string
}

func runAdminWeb(db gowild_data.Database, addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = strings.TrimSpace(os.Getenv("ADMIN_ADDR"))
	}
	if addr == "" {
		addr = "127.0.0.1:8091"
	}

	app, err := newAdminWeb(db)
	if err != nil {
		return err
	}

	log.Printf("Admin website listening on http://%s/accounts", addr)
	return http.ListenAndServe(addr, app.routes())
}

func newAdminWeb(db gowild_data.Database) (*adminWeb, error) {
	tmpl, err := template.New("accounts").Funcs(template.FuncMap{
		"shortKey":        shortKey,
		"pathEscape":      url.PathEscape,
		"isActivePremium": isActivePremium,
	}).Parse(accountsTemplate)
	if err != nil {
		return nil, err
	}

	return &adminWeb{db: db, tmpl: tmpl}, nil
}

func (a *adminWeb) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.handleRoot)
	mux.HandleFunc("GET /accounts", a.handleAccounts)
	mux.HandleFunc("POST /accounts/{pubkey}/promote", a.handlePromote)
	mux.HandleFunc("POST /accounts/{pubkey}/delete", a.handleDelete)
	return mux
}

func (a *adminWeb) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (a *adminWeb) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := listAccounts(r.Context(), a.db)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to load accounts: %v", err), http.StatusInternalServerError)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query != "" {
		accounts = filterAccounts(accounts, query)
	}

	data := accountsPageData{
		Accounts: accounts,
		Query:    query,
		Notice:   strings.TrimSpace(r.URL.Query().Get("notice")),
		Error:    strings.TrimSpace(r.URL.Query().Get("error")),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("render error: %v", err), http.StatusInternalServerError)
		return
	}
}

func (a *adminWeb) handlePromote(w http.ResponseWriter, r *http.Request) {
	pubkey := normalizePubKey(r.PathValue("pubkey"))
	if pubkey == "" {
		redirectWithMessage(w, r, "error", "Missing public key")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithMessage(w, r, "error", "Invalid form payload")
		return
	}
	chain := strings.TrimSpace(r.FormValue("chain"))
	txHash := strings.TrimSpace(r.FormValue("tx_hash"))

	if err := promoteAccount(r.Context(), a.db, pubkey, chain, txHash); err != nil {
		redirectWithMessage(w, r, "error", fmt.Sprintf("Failed to promote %s: %v", shortKey(pubkey), err))
		return
	}

	redirectWithMessage(w, r, "notice", fmt.Sprintf("Promoted %s to premium", shortKey(pubkey)))
}

func (a *adminWeb) handleDelete(w http.ResponseWriter, r *http.Request) {
	pubkey := normalizePubKey(r.PathValue("pubkey"))
	if pubkey == "" {
		redirectWithMessage(w, r, "error", "Missing public key")
		return
	}

	summary, err := deleteAccountCascade(r.Context(), a.db, pubkey)
	if err != nil {
		redirectWithMessage(w, r, "error", fmt.Sprintf("Failed to delete %s: %v", shortKey(pubkey), err))
		return
	}

	if summary.Total == 0 {
		redirectWithMessage(w, r, "notice", fmt.Sprintf("No records found for %s", shortKey(pubkey)))
		return
	}

	redirectWithMessage(w, r, "notice", fmt.Sprintf("Deleted %s (%d records)", shortKey(pubkey), summary.Total))
}

func filterAccounts(accounts []accountRow, query string) []accountRow {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return accounts
	}
	filtered := make([]accountRow, 0, len(accounts))
	for _, account := range accounts {
		if strings.Contains(strings.ToLower(account.PublicKey), q) || strings.Contains(strings.ToLower(account.Name), q) {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func redirectWithMessage(w http.ResponseWriter, r *http.Request, key string, msg string) {
	q := url.Values{}
	q.Set(key, msg)
	http.Redirect(w, r, "/accounts?"+q.Encode(), http.StatusSeeOther)
}

func shortKey(pubkey string) string {
	if len(pubkey) <= 18 {
		return pubkey
	}
	return pubkey[:8] + "..." + pubkey[len(pubkey)-8:]
}

func isActivePremium(account accountRow) bool {
	return account.Tier == "premium" && !account.Revoked
}

const accountsTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GoWild Agent Net Admin</title>
  <style>
    :root {
      --bg-a: #f3f8f7;
      --bg-b: #e3eef5;
      --ink: #112227;
      --muted: #4d646a;
      --card: #ffffff;
      --line: #d5e2e0;
      --ok-bg: #dcf4e8;
      --ok-ink: #1f6f4f;
      --err-bg: #fde7e9;
      --err-ink: #8d2430;
      --accent: #0e7c86;
      --danger: #b23a48;
      --shadow: rgba(24, 42, 45, 0.12);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      color: var(--ink);
      background: radial-gradient(circle at 10% 10%, var(--bg-b), var(--bg-a) 60%);
      min-height: 100vh;
    }
    .wrap {
      max-width: 1320px;
      margin: 24px auto;
      padding: 0 18px;
    }
    .card {
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: 14px;
      box-shadow: 0 8px 28px var(--shadow);
      overflow: hidden;
    }
    .header {
      padding: 18px 20px 14px;
      border-bottom: 1px solid var(--line);
      background: linear-gradient(140deg, #f7fbfa, #f0f7fa);
    }
    .title {
      margin: 0 0 4px;
      font-size: 1.35rem;
      letter-spacing: 0.02em;
      font-family: "Space Grotesk", "IBM Plex Sans", sans-serif;
    }
    .subtitle {
      margin: 0;
      color: var(--muted);
      font-size: 0.92rem;
    }
    .toolbar {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      padding: 14px 20px;
      border-bottom: 1px solid var(--line);
      align-items: center;
    }
    .toolbar input[type="search"] {
      flex: 1;
      min-width: 280px;
      border: 1px solid var(--line);
      border-radius: 10px;
      padding: 10px 12px;
      font: inherit;
      color: var(--ink);
      background: #fbfdfd;
    }
    .btn {
      border: 1px solid transparent;
      border-radius: 10px;
      padding: 9px 12px;
      cursor: pointer;
      font: inherit;
      font-weight: 600;
      white-space: nowrap;
      transition: transform 0.08s ease, filter 0.08s ease;
    }
    .btn:active { transform: translateY(1px); }
    .btn-primary {
      background: var(--accent);
      color: #fff;
    }
    .btn-danger {
      background: var(--danger);
      color: #fff;
    }
    .btn-neutral {
      border-color: var(--line);
      background: #f8fcfc;
      color: var(--ink);
    }
    .flash {
      margin: 16px 20px 0;
      padding: 10px 12px;
      border-radius: 10px;
      font-size: 0.92rem;
    }
    .flash-ok {
      background: var(--ok-bg);
      color: var(--ok-ink);
      border: 1px solid #b7e8d1;
    }
    .flash-err {
      background: var(--err-bg);
      color: var(--err-ink);
      border: 1px solid #f3bdc4;
    }
    .table-wrap {
      overflow-x: auto;
      padding: 14px 20px 20px;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      min-width: 980px;
    }
    th, td {
      text-align: left;
      padding: 11px 8px;
      border-bottom: 1px solid var(--line);
      vertical-align: middle;
      font-size: 0.92rem;
    }
    th {
      font-size: 0.78rem;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--muted);
    }
    .mono {
      font-family: "IBM Plex Mono", ui-monospace, monospace;
      font-size: 0.84rem;
    }
    .badge {
      display: inline-block;
      border-radius: 99px;
      padding: 4px 9px;
      font-size: 0.78rem;
      font-weight: 700;
      letter-spacing: 0.03em;
    }
    .badge-premium {
      background: #e4f4ff;
      color: #155a80;
      border: 1px solid #b9dff6;
    }
    .badge-free {
      background: #ecf3f3;
      color: #436065;
      border: 1px solid #c9dcda;
    }
    .badge-revoked {
      background: #ffe8eb;
      color: #9a2f3c;
      border: 1px solid #f4bdc4;
    }
    .actions {
      display: flex;
      gap: 8px;
      flex-wrap: wrap;
      align-items: center;
    }
    .actions form {
      display: inline-flex;
      gap: 6px;
      align-items: center;
      margin: 0;
    }
    .actions input[type="text"], .actions select {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 7px 8px;
      font: inherit;
      font-size: 0.84rem;
      background: #fff;
      color: var(--ink);
      min-width: 96px;
    }
    @media (max-width: 900px) {
      .wrap { margin: 14px auto; }
      .header { padding: 14px; }
      .toolbar { padding: 12px 14px; }
      .table-wrap { padding: 10px 14px 14px; }
      .toolbar input[type="search"] { min-width: 100%; }
    }
  </style>
</head>
<body>
  <main class="wrap">
    <section class="card">
      <header class="header">
        <h1 class="title">GoWild Agent Net Admin</h1>
        <p class="subtitle">Local-only account controls (browse, promote, delete with post cascade)</p>
      </header>

      <form class="toolbar" method="get" action="/accounts">
        <input type="search" name="q" value="{{.Query}}" placeholder="Search by public key or profile name">
        <button class="btn btn-neutral" type="submit">Search</button>
        <a class="btn btn-neutral" href="/accounts" style="text-decoration:none; display:inline-flex; align-items:center;">Clear</a>
      </form>

      {{if .Notice}}<div class="flash flash-ok">{{.Notice}}</div>{{end}}
      {{if .Error}}<div class="flash flash-err">{{.Error}}</div>{{end}}

      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Public Key</th>
              <th>Name</th>
              <th>Tier</th>
              <th>Posts</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {{if .Accounts}}
              {{range .Accounts}}
                <tr>
                  <td class="mono">{{.PublicKey}}</td>
                  <td>{{if .Name}}{{.Name}}{{else}}<span style="color:#7a8f95;">(none)</span>{{end}}</td>
                  <td>
                    {{if eq .Tier "premium"}}
                      <span class="badge badge-premium">premium</span>
                    {{else}}
                      <span class="badge badge-free">free</span>
                    {{end}}
                    {{if .Revoked}} <span class="badge badge-revoked">revoked</span>{{end}}
                  </td>
                  <td>{{.PostCount}}</td>
                  <td>
                    <div class="actions">
                      {{if isActivePremium .}}
                        <span class="badge badge-premium">active premium</span>
                      {{else}}
                        <form method="post" action="/accounts/{{pathEscape .PublicKey}}/promote">
                          <select name="chain">
                            <option value="solana">solana</option>
                            <option value="ethereum">ethereum</option>
                            <option value="base">base</option>
                          </select>
                          <input type="text" name="tx_hash" placeholder="tx hash (optional)">
                          <button class="btn btn-primary" type="submit">Promote</button>
                        </form>
                      {{end}}
                      <form method="post" action="/accounts/{{pathEscape .PublicKey}}/delete" onsubmit="return confirm('Delete account {{shortKey .PublicKey}} and cascade-delete posts?')">
                        <button class="btn btn-danger" type="submit">Delete</button>
                      </form>
                    </div>
                  </td>
                </tr>
              {{end}}
            {{else}}
              <tr>
                <td colspan="5" style="padding:18px 8px; color:#7a8f95;">No accounts found.</td>
              </tr>
            {{end}}
          </tbody>
        </table>
      </div>
    </section>
  </main>
</body>
</html>
`
