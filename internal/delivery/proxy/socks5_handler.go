//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/things-go/go-socks5"
)

// ConnectFunc dials a target address through a slot's network namespace.
// The implementation handles slot selection and connection tracking.
type ConnectFunc func(ctx context.Context, addr string) (net.Conn, error)

// Socks5Handler wraps things-go/go-socks5 with graceful shutdown.
type Socks5Handler struct {
	server *socks5.Server
	Log    *logrus.Logger
	ln     net.Listener
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSocks5Handler creates a new SOCKS5 proxy handler.
func NewSocks5Handler(
	log *logrus.Logger,
	connect ConnectFunc,
) *Socks5Handler {
	ctx, cancel := context.WithCancel(context.Background())

	server := socks5.NewServer(
		socks5.WithDial(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			return connect(dialCtx, addr)
		}),
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
	c.Log.Infof("SOCKS5 proxy listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-c.ctx.Done():
				return nil
			default:
			}
			c.Log.WithError(err).Error("socks5 accept failed")
			continue
		}

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.server.ServeConn(conn); err != nil {
				c.Log.WithError(err).Warn("socks5 connection ended with error")
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
			c.Log.WithError(err).Warn("socks5 connection ended with error")
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

// socks5Logger forwards SOCKS5 library errors to logrus.
type socks5Logger struct {
	log *logrus.Logger
}

func (l socks5Logger) Errorf(format string, args ...interface{}) {
	l.log.Warnf("socks5: "+format, args...)
}
