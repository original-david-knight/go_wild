package feeds

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// buildClient makes the Fetcher's own client when a hardening knob is set:
// a redirect bound via CheckRedirect, and — under rejectPrivate — a transport
// whose dialer refuses non-public addresses at connect time. Redirects go
// back through the same transport, so a redirect to a private address dies
// at the dial for that hop, after the same scheme check the first hop got.
func buildClient(rejectPrivate bool, maxRedirects int) *http.Client {
	c := &http.Client{Timeout: 20 * time.Second}

	limit := maxRedirects
	if limit == 0 && rejectPrivate {
		limit = 5
	}
	if limit > 0 || rejectPrivate {
		c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if rejectPrivate {
				if err := checkScheme(req.URL.Scheme); err != nil {
					return err
				}
			}
			if limit > 0 && len(via) > limit {
				return fmt.Errorf("stopped after %d redirects", limit)
			}
			return nil
		}
	}

	if rejectPrivate {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			// The hook is read through the package variable at each dial so a
			// test can interpose; production never reassigns it.
			Control: func(network, address string, conn syscall.RawConn) error {
				return dialControl(network, address, conn)
			},
		}
		c.Transport = &http.Transport{
			// Deliberately no Proxy: a proxy would dial the target itself and
			// the Control hook would only ever see the proxy's address.
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	return c
}

// dialControl is the dial-time gate RejectPrivate installs. It is a variable
// only so the test suite — whose every listener is necessarily loopback —
// can admit one fixture address while the gate still refuses the rest.
var dialControl = rejectPrivateControl

// rejectPrivateControl runs inside the dialer for every connection attempt,
// against the address actually being connected to — after DNS, which is what
// makes a rebinding answer no better than typing the private IP directly.
func rejectPrivateControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("address %q: %w", address, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("address %q: %w", host, err)
	}
	// Unmap so an IPv4 address smuggled inside IPv6 (::ffff:10.0.0.1) is
	// judged as the IPv4 address it is. IsGlobalUnicast excludes loopback,
	// link-local (both families), multicast, and the unspecified address;
	// IsPrivate excludes RFC1918 and IPv6 unique-local on top of that.
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return fmt.Errorf("%s is not a public address", host)
	}
	return nil
}

// checkScheme is the every-hop scheme gate under RejectPrivate.
func checkScheme(scheme string) error {
	if scheme == "http" || scheme == "https" {
		return nil
	}
	return fmt.Errorf("scheme %q is not http or https", scheme)
}
