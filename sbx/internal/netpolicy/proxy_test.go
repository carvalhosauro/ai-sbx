// sbx/internal/netpolicy/proxy_test.go
package netpolicy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMatchDomainSuffix(t *testing.T) {
	require.True(t, matchDomain("api.anthropic.com", "api.anthropic.com"))     // exato
	require.True(t, matchDomain("sub.api.anthropic.com", "api.anthropic.com")) // subdomínio
	require.False(t, matchDomain("evil.com", "api.anthropic.com"))
	require.False(t, matchDomain("notanthropic.com", "anthropic.com")) // sufixo é dot-bounded
	require.True(t, matchDomain("archive.ubuntu.com", "*.ubuntu.com")) // wildcard
	require.False(t, matchDomain("ubuntu.com", "*.ubuntu.com"))        // wildcard exige um label
}

func TestProxyEnvSetsAllFourVars(t *testing.T) {
	env := ProxyEnv("http://host.containers.internal:8080")
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		require.Equal(t, "http://host.containers.internal:8080", env[k])
	}
}

func TestAddrUsesContainerHostAlias(t *testing.T) {
	p, err := StartProxy([]string{"api.anthropic.com"})
	require.NoError(t, err)
	defer p.Stop()
	require.True(t, strings.HasPrefix(p.Addr(), "http://host.containers.internal:"), "got %q", p.Addr())
}

// hostProxyURL is the loopback address the HOST uses to reach the proxy (the
// container-facing Addr() uses host.containers.internal, unreachable from the
// test process). White-box: the test lives in package netpolicy.
func hostProxyURL(p *Proxy) string { return fmt.Sprintf("http://127.0.0.1:%d", p.port) }

func newProxyClient(t *testing.T, p *Proxy) *http.Client {
	t.Helper()
	u, err := url.Parse(hostProxyURL(p))
	require.NoError(t, err)
	return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
}

func TestProxyForwardsAllowedHTTP(t *testing.T) {
	var hits int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer up.Close()
	host := hostOnly(up.Listener.Addr().String()) // "127.0.0.1"

	p, err := StartProxy([]string{host})
	require.NoError(t, err)
	defer p.Stop()

	resp, err := newProxyClient(t, p).Get(up.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(1), atomic.LoadInt32(&hits))
}

func TestProxyBlocksDeniedHTTP(t *testing.T) {
	var hits int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	// allowlist NÃO inclui 127.0.0.1 → deny-by-default.
	p, err := StartProxy([]string{"api.anthropic.com"})
	require.NoError(t, err)
	defer p.Stop()

	resp, err := newProxyClient(t, p).Get(up.URL)
	require.NoError(t, err) // o proxy responde 403; o GET em si sucede no nível HTTP
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	// invariante: credencial vazada não escapa — o upstream negado NUNCA é contatado.
	require.Equal(t, int32(0), atomic.LoadInt32(&hits))
}

// connectStatus abre um CONNECT cru no proxy e devolve o status HTTP da resposta
// ("200" quando o túnel é permitido, "403" quando negado).
func connectStatus(t *testing.T, proxyPort int, target string) string {
	t.Helper()
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 3*time.Second)
	require.NoError(t, err)
	defer c.Close()
	_, err = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	require.NoError(t, err)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	require.NoError(t, err)
	fields := strings.Fields(line) // "HTTP/1.1 200 Connection Established"
	require.GreaterOrEqual(t, len(fields), 2, "bad status line %q", line)
	return fields[1]
}

func TestProxyConnectAllowedAndDenied(t *testing.T) {
	// "upstream" TCP local que o CONNECT permitido vai tunelar.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	target := ln.Addr().String() // "127.0.0.1:<port>"

	pa, err := StartProxy([]string{"127.0.0.1"})
	require.NoError(t, err)
	defer pa.Stop()
	require.Equal(t, "200", connectStatus(t, pa.port, target)) // permitido → túnel

	pd, err := StartProxy([]string{"api.anthropic.com"})
	require.NoError(t, err)
	defer pd.Stop()
	require.Equal(t, "403", connectStatus(t, pd.port, target)) // negado
}
