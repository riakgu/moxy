//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/elazarl/goproxy"
	"github.com/sirupsen/logrus"
)

// HttpProxyHandler wraps elazarl/goproxy with graceful shutdown.
type HttpProxyHandler struct {
	Log    *logrus.Logger
	server *http.Server
}

// NewHttpProxyHandler creates a new HTTP proxy handler.
func NewHttpProxyHandler(
	log *logrus.Logger,
	connect ConnectFunc,
) *HttpProxyHandler {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false

	proxy.ConnectDial = func(network, addr string) (net.Conn, error) {
		return connect(context.Background(), addr)
	}

	proxy.Tr = &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return connect(ctx, addr)
		},
	}

	return &HttpProxyHandler{
		Log: log,
		server: &http.Server{
			Handler: proxy,
		},
	}
}

// ListenAndServe starts the HTTP proxy on the given address.
func (c *HttpProxyHandler) ListenAndServe(addr string) error {
	c.server.Addr = addr
	c.Log.Infof("HTTP proxy listening on %s", addr)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("http proxy listen: %w", err)
	}

	err = c.server.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown stops accepting new connections and waits for active ones to drain.
func (c *HttpProxyHandler) Shutdown(ctx context.Context) error {
	return c.server.Shutdown(ctx)
}
