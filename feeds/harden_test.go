package feeds

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// redirectChain serves /hop/N as N redirects ending at the RSS fixture.
func redirectChain(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/hop/", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop/"))
		if n <= 0 {
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(rssFixture))
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/hop/%d", n-1), http.StatusFound)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestRedirectsFollowedWithinBound(t *testing.T) {
	ts := redirectChain(t)
	f := &Fetcher{MaxRedirects: 3}
	ctx := context.Background()

	feed, err := f.Probe(ctx, ts.URL+"/hop/3")
	if err != nil {
		t.Fatalf("three redirects under a bound of three: %v", err)
	}
	if len(feed.Items) != 2 {
		t.Errorf("redirected fetch parsed %d items, want 2", len(feed.Items))
	}

	_, err = f.Probe(ctx, ts.URL+"/hop/4")
	if err == nil {
		t.Fatal("four redirects under a bound of three succeeded")
	}
	if !strings.Contains(err.Error(), "stopped after 3 redirects") {
		t.Errorf("bound error = %v", err)
	}
}

func TestRejectPrivateRefusesLoopbackAtDialTime(t *testing.T) {
	srv := &server{body: rssFixture}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	f := &Fetcher{RejectPrivate: true}

	_, err := f.Probe(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("a loopback feed URL was fetched under RejectPrivate")
	}
	if !strings.Contains(err.Error(), "is not a public address") {
		t.Errorf("refusal = %v", err)
	}
	if srv.hits != 0 {
		t.Errorf("the forbidden server was reached %d times; the dial should have died first", srv.hits)
	}
}

func TestRejectPrivateRefusesRedirectToLoopbackAtTheHop(t *testing.T) {
	forbidden := &server{body: rssFixture}
	target := httptest.NewServer(forbidden)
	t.Cleanup(target.Close)

	entryHits := 0
	entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entryHits++
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(entry.Close)

	// Every listener a test can own is loopback, so the gate is opened for
	// exactly the entry address; the redirect target stays forbidden. What
	// this pins is the mechanism: the hop re-enters the hardened dialer and
	// dies there, before the target sees a byte.
	entryAddr := entry.Listener.Addr().String()
	prev := dialControl
	dialControl = func(network, address string, conn syscall.RawConn) error {
		if address == entryAddr {
			return nil
		}
		return prev(network, address, conn)
	}
	t.Cleanup(func() { dialControl = prev })

	f := &Fetcher{RejectPrivate: true}
	_, err := f.Probe(context.Background(), entry.URL)
	if err == nil {
		t.Fatal("a redirect to a loopback address was followed under RejectPrivate")
	}
	if !strings.Contains(err.Error(), "is not a public address") {
		t.Errorf("refusal = %v", err)
	}
	if entryHits != 1 {
		t.Errorf("entry server hits = %d, want 1", entryHits)
	}
	if forbidden.hits != 0 {
		t.Errorf("the redirect target was reached %d times; the hop's dial should have died first", forbidden.hits)
	}
}

// refusedAddress picks the thing the dial-time gate judged out of its reason.
var refusedAddress = regexp.MustCompile(`(\S+) is not a public address`)

// TestRejectPrivateJudgesTheResolvedAddressNotTheName is the rebinding shape:
// the URL names a host, not an address, so nothing about the URL text says
// where the connection goes. What the gate sees is the address the dialer is
// about to connect to, after resolution and once per attempt — which is why a
// name that answers a public address on the check and a private one on the
// connect gets no further than a name that was private all along.
func TestRejectPrivateJudgesTheResolvedAddressNotTheName(t *testing.T) {
	srv := &server{body: rssFixture}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("listener address: %v", err)
	}

	var seen []string
	prev := dialControl
	dialControl = func(network, address string, conn syscall.RawConn) error {
		seen = append(seen, address)
		return prev(network, address, conn)
	}
	t.Cleanup(func() { dialControl = prev })

	f := &Fetcher{RejectPrivate: true}
	_, err = f.Probe(context.Background(), "http://localhost:"+port+"/feed.xml")
	if err == nil {
		t.Fatal("a name resolving to loopback was fetched under RejectPrivate")
	}
	if !strings.Contains(err.Error(), "is not a public address") {
		t.Errorf("refusal = %v", err)
	}
	// The reason names what was judged. The URL as typed is still in the
	// wrapping — the user has to know which feed was refused — but the clause
	// that states the policy carries a resolved address, never the name.
	judged := refusedAddress.FindStringSubmatch(err.Error())
	if judged == nil {
		t.Fatalf("the refusal states no address: %v", err)
	}
	if _, perr := netip.ParseAddr(judged[1]); perr != nil {
		t.Errorf("the refusal judged %q — a name, not a resolved address", judged[1])
	}
	if len(seen) == 0 {
		t.Fatal("the gate never ran; a dial that skips it is a dial that is not judged")
	}
	for _, address := range seen {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatalf("the gate saw %q, which is not an address:port", address)
		}
		addr, err := netip.ParseAddr(host)
		if err != nil {
			t.Errorf("the gate saw %q — a name, not a resolved address", host)
			continue
		}
		if !addr.IsLoopback() {
			t.Errorf("the gate saw %q, want the loopback address the name resolves to", host)
		}
	}
	if srv.hits != 0 {
		t.Errorf("the forbidden server was reached %d times", srv.hits)
	}
}

func TestRejectPrivateRefusesNonHTTPScheme(t *testing.T) {
	f := &Fetcher{RejectPrivate: true}
	_, err := f.Probe(context.Background(), "file:///etc/passwd")
	if err == nil {
		t.Fatal("a file: URL was fetched under RejectPrivate")
	}
	if !strings.Contains(err.Error(), `scheme "file" is not http or https`) {
		t.Errorf("scheme refusal = %v", err)
	}
}

func TestOversizedFeedIsAnErrorNotATruncation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxBody+1))
	}))
	t.Cleanup(ts.Close)
	f := &Fetcher{}

	_, err := f.Probe(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("an oversized body was accepted")
	}
	if !strings.Contains(err.Error(), "the feed is larger than 8 MB") {
		t.Errorf("oversize error = %v", err)
	}
}
