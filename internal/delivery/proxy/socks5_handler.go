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

// Socks5Handler wraps things-go/go-socks5 with concurrency control and graceful shutdown.
type Socks5Handler struct {
	server *socks5.Server
	Log    *logrus.Logger
	sem    chan struct{}
	ln     net.Listener
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSocks5Handler creates a new SOCKS5 proxy handler.
func NewSocks5Handler(
	log *logrus.Logger,
	connect ConnectFunc,
	sem chan struct{},
) *Socks5Handler {
	ctx, cancel := context.WithCancel(context.Background())

	server := socks5.NewServer(
		socks5.WithDial(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			return connect(dialCtx, addr)
		}),
		socks5.WithAuthMethods([]socks5.Authenticator{
			socks5.NoAuthAuthenticator{},
		}),
		socks5.WithLogger(nopLogger{}),
	)

	return &Socks5Handler{
		server: server,
		Log:    log,
		sem:    sem,
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

		select {
		case c.sem <- struct{}{}:
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				defer func() { <-c.sem }()
				if err := c.server.ServeConn(conn); err != nil {
					c.Log.WithError(err).Debug("socks5 connection ended with error")
				}
			}()
		default:
			c.Log.Warn("socks5: connection rejected — too many concurrent connections")
			conn.Close()
		}
	}
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

// nopLogger implements socks5.Logger with no output.
type nopLogger struct{}

func (nopLogger) Errorf(string, ...interface{}) {}
