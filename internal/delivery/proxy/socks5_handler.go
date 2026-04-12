//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"log/slog"

	"github.com/things-go/go-socks5"
)

type ConnectFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type Socks5Handler struct {
	server *socks5.Server
	Log    *slog.Logger
	ln     net.Listener
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

type passthroughResolver struct{}

func (r passthroughResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	return ctx, nil, nil
}

func NewSocks5Handler(
	log *slog.Logger,
	connect ConnectFunc,
) *Socks5Handler {
	ctx, cancel := context.WithCancel(context.Background())

	server := socks5.NewServer(
		socks5.WithDial(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			return connect(dialCtx, network, addr)
		}),
		socks5.WithResolver(passthroughResolver{}),
		socks5.WithAuthMethods([]socks5.Authenticator{
			socks5.NoAuthAuthenticator{},
		}),
		socks5.WithLogger(socks5Logger{log: log}),
	)

	return &Socks5Handler{
		server: server,
		Log:    log,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (c *Socks5Handler) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks5 listen: %w", err)
	}
	c.ln = ln
	c.Log.Info("socks5 listener started", "addr", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-c.ctx.Done():
				return nil
			default:
			}
			c.Log.Error("socks5 accept failed", "error", err)
			continue
		}

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.server.ServeConn(conn); err != nil {
				c.Log.Warn("socks5 connection error", "error", err)
			}
		}()
	}
}

func (c *Socks5Handler) ServeConn(conn net.Conn) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := c.server.ServeConn(conn); err != nil {
			c.Log.Warn("socks5 connection error", "error", err)
		}
	}()
}

func (c *Socks5Handler) Shutdown(ctx context.Context) error {
	c.cancel()
	if c.ln != nil {
		_ = c.ln.Close()
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// socks5Logger forwards SOCKS5 library errors to slog.
type socks5Logger struct {
	log *slog.Logger
}

func (l socks5Logger) Errorf(format string, args ...interface{}) {
	l.log.Warn(fmt.Sprintf("socks5: "+format, args...))
}
