package httpclient

import (
	"context"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestR1ExplicitHTTPProxyFailuresNeverConnectDirect(t *testing.T) {
	for _, code := range []int{http.StatusProxyAuthRequired, http.StatusBadGateway, http.StatusFound} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			var targetCalls, proxyCalls atomic.Int32
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
			defer target.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyCalls.Add(1)
				if r.Method != http.MethodConnect {
					t.Error("expected CONNECT")
				}
				w.Header().Set("Location", target.URL)
				w.WriteHeader(code)
			}))
			defer proxy.Close()
			// Explicit binding must win over process-global defaults, including NO_PROXY.
			t.Setenv("HTTPS_PROXY", target.URL)
			t.Setenv("HTTP_PROXY", target.URL)
			t.Setenv("NO_PROXY", "*")
			client, err := GetClient(Options{ProxyURL: proxy.URL, Timeout: time.Second})
			require.NoError(t, err)
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target.URL, nil)
			require.NoError(t, err)
			resp, err := client.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			require.Error(t, err)
			require.Equal(t, int32(1), proxyCalls.Load())
			require.Zero(t, targetCalls.Load())
		})
	}
}

func TestR1SOCKS5HForwardsHostnameWithoutLocalResolution(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	names := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		greeting := make([]byte, 2)
		if _, err = io.ReadFull(conn, greeting); err != nil {
			return
		}
		methods := make([]byte, int(greeting[1]))
		if _, err = io.ReadFull(conn, methods); err != nil {
			return
		}
		if _, err = conn.Write([]byte{5, 0}); err != nil {
			return
		}
		header := make([]byte, 4)
		if _, err = io.ReadFull(conn, header); err != nil {
			return
		}
		if header[3] != 3 {
			names <- "not-domain"
			return
		}
		length := make([]byte, 1)
		if _, err = io.ReadFull(conn, length); err != nil {
			return
		}
		hostAndPort := make([]byte, int(length[0])+2)
		if _, err = io.ReadFull(conn, hostAndPort); err != nil {
			return
		}
		names <- string(hostAndPort[:len(hostAndPort)-2])
		// A deliberate local SOCKS failure: no destination connection is attempted.
		_, _ = conn.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0})
	}()
	client, err := GetClient(Options{ProxyURL: "socks5h://" + listener.Addr().String(), Timeout: time.Second})
	require.NoError(t, err)
	resp, err := client.Get("https://sub2api-r1-no-such-host.invalid/")
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
	select {
	case host := <-names:
		require.Equal(t, "sub2api-r1-no-such-host.invalid", host)
	case <-time.After(2 * time.Second):
		t.Fatal("SOCKS server did not receive domain")
	}
	<-done
}

func TestR1RedirectPolicyHasIndependentCachedClient(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()
	regular, err := GetClient(Options{Timeout: time.Second})
	require.NoError(t, err)
	protected, err := GetClient(Options{Timeout: time.Second, DisableRedirects: true})
	require.NoError(t, err)
	protectedAgain, err := GetClient(Options{Timeout: time.Second, DisableRedirects: true})
	require.NoError(t, err)
	require.NotSame(t, regular, protected)
	require.Same(t, protected, protectedAgain)
	require.Nil(t, regular.CheckRedirect, "creating a protected client must not mutate the cached default client")
	for _, tt := range []struct {
		client *http.Client
		status int
	}{{regular, 200}, {protected, 302}, {regular, 200}} {
		resp, err := tt.client.Get(redirect.URL)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, tt.status, resp.StatusCode)
	}
}
