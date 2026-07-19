// sbx/internal/netpolicy/proxy.go
package netpolicy

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// containerHostAlias is the DNS name podman injects into containers pointing at
// the host. On an --internal network it's the ONLY endpoint the container can
// reach off-box, and that endpoint is this proxy.
const containerHostAlias = "host.containers.internal"

// Proxy is an in-process HTTP/HTTPS(CONNECT) forward proxy that only permits
// egress to a suffix-matched domain allowlist. Everything else is denied
// (deny-by-default): a leaked prod/homolog hostname simply cannot be reached
// from inside a sandbox env, credential or not.
type Proxy struct {
	allow []string
	ln    net.Listener
	srv   *http.Server
	port  int
}

// StartProxy binds a dynamic host port on 0.0.0.0 (so the container can reach it
// through the internal-network gateway) and serves until Stop.
func StartProxy(allow []string) (*Proxy, error) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("netpolicy: listen: %w", err)
	}
	p := &Proxy{
		allow: normalizeAllow(allow),
		ln:    ln,
		port:  ln.Addr().(*net.TCPAddr).Port,
	}
	p.srv = &http.Server{Handler: p}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// Addr is the value to inject into a container as HTTP(S)_PROXY. It uses the
// podman host alias because the container sits on an --internal network whose
// only reachable off-container endpoint is the host (where this proxy listens).
func (p *Proxy) Addr() string {
	return fmt.Sprintf("http://%s:%d", containerHostAlias, p.port)
}

func (p *Proxy) Stop() error { return p.srv.Close() }

// ProxyEnv builds the four proxy env vars a container needs so every HTTP/HTTPS
// client routes egress through the proxy. Injected via EnvSpec.EnvVars (M2).
func ProxyEnv(addr string) map[string]string {
	return map[string]string{
		"HTTP_PROXY":  addr,
		"HTTPS_PROXY": addr,
		"http_proxy":  addr,
		"https_proxy": addr,
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

// handleConnect tunnels HTTPS after checking the allowlist. On deny it answers
// 403 BEFORE dialing, so a denied host is never contacted.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	if !p.allowed(r.Host) {
		http.Error(w, "sandbox egress denied: "+hostOnly(r.Host), http.StatusForbidden)
		return
	}
	dst, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy: hijack unsupported", http.StatusInternalServerError)
		_ = dst.Close()
		return
	}
	src, _, err := hj.Hijack()
	if err != nil {
		_ = dst.Close()
		return
	}
	_, _ = io.WriteString(src, "HTTP/1.1 200 Connection Established\r\n\r\n")
	done := make(chan struct{}, 2)
	cp := func(a, b net.Conn) { _, _ = io.Copy(a, b); done <- struct{}{} }
	go cp(dst, src)
	go cp(src, dst)
	<-done // primeira ponta a terminar fecha ambas, desbloqueando a outra cópia
	_ = src.Close()
	_ = dst.Close()
	<-done
}

// handleHTTP forwards a plain (non-CONNECT) absolute-URI request after checking
// the allowlist. On deny it answers 403 without forwarding.
func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.allowed(r.Host) {
		http.Error(w, "sandbox egress denied: "+hostOnly(r.Host), http.StatusForbidden)
		return
	}
	r.RequestURI = ""
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *Proxy) allowed(hostport string) bool {
	host := strings.ToLower(strings.TrimSuffix(hostOnly(hostport), "."))
	for _, d := range p.allow {
		if matchDomain(host, d) {
			return true
		}
	}
	return false
}

// matchDomain implements suffix-match. A bare domain matches itself and any
// subdomain (dot-bounded). A "*.foo" pattern matches subdomains only.
func matchDomain(host, pattern string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	pattern = strings.ToLower(pattern)
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".ubuntu.com"
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func normalizeAllow(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			out = append(out, d)
		}
	}
	return out
}
