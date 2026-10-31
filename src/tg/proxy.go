package tg

import (
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"golang.org/x/net/http/httpproxy"
)

func newTelegramResolver() dcs.Resolver {
	client := newTelegramHttpClient(httpproxy.FromEnvironment())
	if client == nil {
		return telegram.TDesktopResolver()
	}
	return dcs.Websocket(dcs.WebsocketOptions{
		DialOptions: &websocket.DialOptions{
			HTTPClient:   client,
			Subprotocols: []string{"binary"},
		},
	})
}

func newTelegramHttpClient(config *httpproxy.Config) *http.Client {
	if config.HTTPProxy == "" && config.HTTPSProxy == "" {
		return nil
	}
	proxyConfig := *config
	if proxyConfig.HTTPSProxy == "" {
		proxyConfig.HTTPSProxy = proxyConfig.HTTPProxy
	}
	proxy := proxyConfig.ProxyFunc()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		return proxy(request.URL)
	}
	transport.TLSHandshakeTimeout = 15 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}
