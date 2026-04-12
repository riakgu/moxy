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

type HttpProxyHandler struct {
	Log    *slog.Logger
	server *http.Server
}

func NewHttpProxyHandler(
	log *slog.Logger,
	connect ConnectFunc,
) *HttpProxyHandler {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false

	proxy.ConnectDial = func(network, addr string) (net.Conn, error) {
		return connect(context.Background(), "tcp", addr)
	}

	proxy.Tr = &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return connect(ctx, "tcp", addr)
		},
	}

	return &HttpProxyHandler{
		Log: log,
		server: &http.Server{
			Handler: proxy,
		},
	}
}

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

func (c *HttpProxyHandler) Shutdown(ctx context.Context) error {
	return c.server.Shutdown(ctx)
}

func (c *HttpProxyHandler) ServeConn(conn net.Conn) {
	ln := newSingleConnListener(conn)
	srv := &http.Server{Handler: c.server.Handler}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		c.Log.Warn("single-conn serve failed", "error", err)
	}
	_ = ln.Close()
}

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
