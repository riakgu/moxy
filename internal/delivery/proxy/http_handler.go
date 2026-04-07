//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"log/slog"

	"github.com/elazarl/goproxy"
)

// HttpProxyHandler wraps elazarl/goproxy with graceful shutdown.
type HttpProxyHandler struct {
	Log    *slog.Logger
	server *http.Server
}

// NewHttpProxyHandler creates a new HTTP proxy handler.
func NewHttpProxyHandler(
	log *slog.Logger,
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
	c.Log.Info("http proxy listener started", "addr", addr)

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

// ServeConn handles a single pre-accepted connection.
func (c *HttpProxyHandler) ServeConn(conn net.Conn) {
	ln := newSingleConnListener(conn)
	srv := &http.Server{Handler: c.server.Handler}
	srv.Serve(ln)
	ln.Close()
}

// singleConnListener is a net.Listener that serves exactly one connection.
type singleConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{conn: conn, done: make(chan struct{})}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.conn })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *singleConnListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}
