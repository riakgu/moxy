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

// HttpProxyHandler wraps elazarl/goproxy with concurrency control and graceful shutdown.
type HttpProxyHandler struct {
	Log    *logrus.Logger
	server *http.Server
}

// NewHttpProxyHandler creates a new HTTP proxy handler.
func NewHttpProxyHandler(
	log *logrus.Logger,
	connect ConnectFunc,
	sem chan struct{},
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

	// Concurrency gate
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		select {
		case sem <- struct{}{}:
			ctx.UserData = sem
			return req, nil
		default:
			log.Warn("http proxy: connection rejected — too many concurrent connections")
			return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusServiceUnavailable, "Service Unavailable")
		}
	})

	proxy.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if ctx.UserData != nil {
			<-sem
		}
		return resp
	})

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
