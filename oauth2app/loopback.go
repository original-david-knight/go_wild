package oauth2app

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// LoopbackOptions tune ConnectLoopback. The zero value is the normal case.
type LoopbackOptions struct {
	// Timeout bounds the wait for the user to finish consenting.
	Timeout time.Duration
	// Port pins the loopback port. Zero picks a free one, which is what an
	// installed app should do.
	Port int
	// CallbackPath is the path the ephemeral listener answers on; zero value
	// "/oauth/callback".
	CallbackPath string
}

// ConnectLoopback runs the whole ephemeral-redirect authorization-code flow:
// it starts a loopback listener, hands the consent URL to opener, waits for
// the provider to redirect back with a code, exchanges it under the two
// refresh-token rules, and stores the token under account.
//
// The listener binds 127.0.0.1 — the redirect must never be reachable off the
// machine, because the authorization code arrives in its query string.
func (f *Flow) ConnectLoopback(ctx context.Context, account string, opener Opener, opts LoopbackOptions) (*oauth2.Token, error) {
	name, err := NormalizeAccount(account)
	if err != nil {
		return nil, err
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.CallbackPath == "" {
		opts.CallbackPath = "/oauth/callback"
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return nil, fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()

	redirectURL := fmt.Sprintf("http://127.0.0.1:%d%s", listener.Addr().(*net.TCPAddr).Port, opts.CallbackPath)

	// The state parameter is what stops another page on this machine from
	// feeding a code of its own into the listener.
	states := NewStates(opts.Timeout)
	state, err := states.New()
	if err != nil {
		return nil, err
	}

	type callback struct {
		code string
		err  error
	}
	results := make(chan callback, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(opts.CallbackPath, func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if !states.Take(q.Get("state")) {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			results <- callback{err: fmt.Errorf("state mismatch: the redirect did not come from this request")}
			return
		}
		if errMsg := q.Get("error"); errMsg != "" {
			writeClosePage(w, "Consent was declined.", html.EscapeString(errMsg))
			results <- callback{err: fmt.Errorf("consent declined: %s", errMsg)}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "no code", http.StatusBadRequest)
			results <- callback{err: fmt.Errorf("redirect carried no authorization code")}
			return
		}
		writeClosePage(w, "Connected.", "You can close this tab and return to the terminal.")
		results <- callback{code: code}
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := opener(f.AuthURL(redirectURL, state)); err != nil {
		return nil, fmt.Errorf("open consent page: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var result callback
	select {
	case result = <-results:
	case <-waitCtx.Done():
		return nil, fmt.Errorf("timed out waiting for consent after %s", opts.Timeout)
	}
	if result.err != nil {
		return nil, result.err
	}
	return f.Exchange(ctx, name, redirectURL, result.code)
}

// writeClosePage answers the browser with a plain page. It is the last thing
// the flow shows, so it says what happened and nothing else.
func writeClosePage(w http.ResponseWriter, title, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title>
<body style="font-family:system-ui;background:#161826;color:#e9e9ed;padding:3rem">
<h1 style="font-weight:500;font-size:1.5rem">%s</h1><p style="color:#9397ab">%s</p>`,
		html.EscapeString(title), html.EscapeString(title), detail)
}
