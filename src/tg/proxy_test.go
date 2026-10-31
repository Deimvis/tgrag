package tg

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gotd/td/telegram/dcs"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http/httpproxy"
)

func clearProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(name, "")
	}
}

func TestTelegramResolverProxyEnvironment(t *testing.T) {
	for _, test := range []struct {
		name        string
		environment map[string]string
		expProxy    bool
	}{
		{name: "direct"},
		{name: "https proxy", environment: map[string]string{"HTTPS_PROXY": "http://proxy.invalid:8080"}, expProxy: true},
		{name: "http proxy fallback", environment: map[string]string{"HTTP_PROXY": "http://proxy.invalid:8080"}, expProxy: true},
		{name: "lowercase proxy", environment: map[string]string{"https_proxy": "http://proxy.invalid:8080"}, expProxy: true},
		{name: "https precedence", environment: map[string]string{"HTTPS_PROXY": "http://proxy.invalid:8080", "HTTP_PROXY": "http://unused.invalid:8080"}, expProxy: true},
		{name: "no proxy", environment: map[string]string{"HTTPS_PROXY": "http://unused.invalid:8080", "NO_PROXY": "telegram.invalid"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearProxyEnvironment(t)
			for key, value := range test.environment {
				t.Setenv(key, value)
			}
			requests := make(chan *http.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests <- request.Clone(request.Context())
				writer.WriteHeader(http.StatusBadGateway)
			}))
			defer server.Close()
			if test.expProxy {
				if test.name == "http proxy fallback" {
					t.Setenv("HTTP_PROXY", server.URL)
				} else if test.name == "lowercase proxy" {
					t.Setenv("https_proxy", server.URL)
				} else {
					t.Setenv("HTTPS_PROXY", server.URL)
				}
			}
			resolver := newTelegramResolver()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, err := resolver.Primary(ctx, 1, dcs.List{Domains: map[int]string{1: "wss://telegram.invalid/apiws"}})
			require.Error(t, err)
			select {
			case request := <-requests:
				require.True(t, test.expProxy)
				require.Equal(t, http.MethodConnect, request.Method)
				require.Equal(t, "telegram.invalid:443", request.Host)
			default:
				require.False(t, test.expProxy)
			}
		})
	}
}

func TestTelegramHttpClientProxySelection(t *testing.T) {
	for _, test := range []struct {
		name     string
		config   httpproxy.Config
		expProxy string
	}{
		{name: "unset"},
		{name: "http fallback", config: httpproxy.Config{HTTPProxy: "http://proxy.invalid:80"}, expProxy: "http://proxy.invalid:80"},
		{name: "https precedence", config: httpproxy.Config{HTTPProxy: "http://first.invalid:80", HTTPSProxy: "http://second.invalid:80"}, expProxy: "http://second.invalid:80"},
		{name: "domain bypass", config: httpproxy.Config{HTTPSProxy: "http://proxy.invalid:80", NoProxy: ".telegram.org"}},
		{name: "wildcard bypass", config: httpproxy.Config{HTTPSProxy: "http://proxy.invalid:80", NoProxy: "*"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newTelegramHttpClient(&test.config)
			if test.name == "unset" {
				require.Nil(t, client)
				return
			}
			require.NotNil(t, client)
			request, err := http.NewRequest(http.MethodGet, "https://venus.web.telegram.org/apiws", nil)
			require.NoError(t, err)
			transport := client.Transport.(*http.Transport)
			proxy, err := transport.Proxy(request)
			require.NoError(t, err)
			if test.expProxy == "" {
				require.Nil(t, proxy)
				return
			}
			require.Equal(t, test.expProxy, proxy.String())
		})
	}
}

func TestTelegramResolverProxyAuthentication(t *testing.T) {
	clearProxyEnvironment(t)
	headers := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		headers <- request.Header.Get("Proxy-Authorization")
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	proxy, err := url.Parse(server.URL)
	require.NoError(t, err)
	proxy.User = url.UserPassword("test-user", "test-password")
	t.Setenv("HTTPS_PROXY", proxy.String())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = newTelegramResolver().Primary(ctx, 1, dcs.List{Domains: map[int]string{1: "wss://telegram.invalid/apiws"}})
	require.Error(t, err)
	select {
	case header := <-headers:
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("test-user:test-password")), header)
	default:
		t.Fatal("proxy did not receive a CONNECT request")
	}
}
