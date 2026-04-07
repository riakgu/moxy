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

// ConnectFunc dials a target address through a slot's network namespace.
// The implementation handles slot selection and connection tracking.
type ConnectFunc func(ctx context.Context, addr string) (net.Conn, error)

// Socks5Handler wraps things-go/go-socks5 with graceful shutdown.
type Socks5Handler struct {
	server *socks5.Server
	Log    *slog.Logger
	ln     net.Listener
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// passthroughResolver is a no-op DNS resolver for go-socks5.
// It returns nil IP so that the FQDN passes through to our Dial function,
// where the CachingResolver handles DNS resolution with caching.
type passthroughResolver struct{}

func (r passthroughResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	return ctx, nil, nil
}

// NewSocks5Handler creates a new SOCKS5 proxy handler.
func NewSocks5Handler(
	log *slog.Logger,
	connect ConnectFunc,
) *Socks5Handler {
	ctx, cancel := context.WithCancel(context.Background())

	server := socks5.NewServer(
		socks5.WithDial(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			return connect(dialCtx, addr)
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

// ListenAndServe starts the SOCKS5 proxy on the given address.
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

// ServeConn handles a single pre-accepted connection.
func (c *Socks5Handler) ServeConn(conn net.Conn) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := c.server.ServeConn(conn); err != nil {
			c.Log.Warn("socks5 connection error", "error", err)
		}
	}()
}

// Shutdown stops accepting new connections and waits for active ones to drain.
func (c *Socks5Handler) Shutdown(ctx context.Context) error {
	c.cancel()
	if c.ln != nil {
		c.ln.Close()
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
