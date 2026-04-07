package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/net/proxy"
)

// ContextDialer dials with context support.
type ContextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// HTTPTransport returns an *http.Transport configured to use the given proxy.
// Supports http, https, socks5, and socks5h schemes.
// Returns nil, nil when proxyURL is empty (no proxy).
func HTTPTransport(proxyURL string) (*http.Transport, error) {
	if proxyURL == "" {
		return nil, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return &http.Transport{
			Proxy: http.ProxyURL(u),
		}, nil
	case "socks5":
		dialer, err := newSOCKS5Dialer(u)
		if err != nil {
			return nil, err
		}
		// socks5:// resolves DNS locally before passing IP to proxy.
		ld := &localDNSDialer{inner: dialer}
		return &http.Transport{
			DialContext: ld.DialContext,
		}, nil
	case "socks5h":
		dialer, err := newSOCKS5Dialer(u)
		if err != nil {
			return nil, err
		}
		// socks5h:// passes hostname to proxy for remote DNS resolution.
		return &http.Transport{
			DialContext: dialer.DialContext,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %s", u.Scheme)
	}
}

// SOCKS5Dialer returns a ContextDialer that routes connections through a SOCKS5 proxy.
// For socks5:// scheme, DNS is resolved locally. For socks5h://, DNS is resolved by the proxy.
// Returns nil, nil when proxyURL is empty (no proxy).
func SOCKS5Dialer(proxyURL string) (ContextDialer, error) {
	if proxyURL == "" {
		return nil, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "socks5":
		dialer, err := newSOCKS5Dialer(u)
		if err != nil {
			return nil, err
		}
		return &localDNSDialer{inner: dialer}, nil
	case "socks5h":
		return newSOCKS5Dialer(u)
	default:
		return nil, fmt.Errorf("SOCKS5Dialer requires socks5 or socks5h scheme, got %s", u.Scheme)
	}
}

func newSOCKS5Dialer(u *url.URL) (ContextDialer, error) {
	var auth *proxy.Auth
	if u.User != nil {
		password, _ := u.User.Password()
		auth = &proxy.Auth{
			User:     u.User.Username(),
			Password: password,
		}
	}
	d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}
	cd, ok := d.(ContextDialer)
	if !ok {
		return nil, fmt.Errorf("SOCKS5 dialer does not support DialContext")
	}
	return cd, nil
}

// localDNSDialer resolves DNS locally before dialing through the inner dialer.
// This implements the socks5:// (as opposed to socks5h://) behavior.
type localDNSDialer struct {
	inner ContextDialer
}

func (d *localDNSDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return d.inner.DialContext(ctx, network, addr)
	}
	// If it's already an IP, skip resolution.
	if net.ParseIP(host) != nil {
		return d.inner.DialContext(ctx, network, addr)
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for host %s", host)
	}
	return d.inner.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
}
