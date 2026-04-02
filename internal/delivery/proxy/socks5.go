//go:build linux

package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/things-go/go-socks5"

	"github.com/riakgu/moxy/internal/usecase"
)

// Socks5Server wraps things-go/go-socks5 with Moxy's namespace dialer,
// slot routing, concurrency control, and graceful shutdown.
type Socks5Server struct {
	server      *socks5.Server
	log         *logrus.Logger
	sem         chan struct{}
	idleTimeout time.Duration
	ln          net.Listener
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewSocks5Server creates a new SOCKS5 proxy server.
// It uses go-socks5 for protocol handling and our namespace dialer for connections.
func NewSocks5Server(
	log *logrus.Logger,
	slotUC *usecase.SlotUseCase,
	dialer usecase.SlotDialer,
	router SlotRouter,
	sem chan struct{},
	idleTimeout time.Duration,
) *Socks5Server {
	ctx, cancel := context.WithCancel(context.Background())

	server := socks5.NewServer(
		// Custom dialer: route through namespace via SlotRouter
		socks5.WithDial(func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			// Select a slot using the router
			slotName, err := router.Route(dialCtx, "")
			if err != nil {
				return nil, err
			}

			// Track connections
			slotUC.IncrementConnections(slotName)

			conn, err := dialer.Dial(slotName, addr)
			if err != nil {
				slotUC.DecrementConnections(slotName)
				return nil, fmt.Errorf("dial %s via %s: %w", addr, slotName, err)
			}

			// Wrap with connection tracking and idle timeout
			tracked := &trackedConnSocks5{
				Conn:     newIdleTimeoutConn(conn, idleTimeout),
				slotName: slotName,
				slotUC:   slotUC,
			}
			return tracked, nil
		}),

		// No auth — single user, localhost access
		socks5.WithAuthMethods([]socks5.Authenticator{
			socks5.NoAuthAuthenticator{},
		}),

		// Use logrus for go-socks5 logging
		socks5.WithLogger(socks5.NewLogger(log.WithField("component", "socks5").Writer())),
	)

	return &Socks5Server{
		server:      server,
		log:         log,
		sem:         sem,
		idleTimeout: idleTimeout,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// ListenAndServe starts the SOCKS5 proxy on the given address.
func (s *Socks5Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks5 listen: %w", err)
	}
	s.ln = ln
	s.log.Infof("SOCKS5 proxy listening on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return nil
			default:
			}
			s.log.WithError(err).Error("socks5 accept failed")
			continue
		}

		// Concurrency gate
		select {
		case s.sem <- struct{}{}:
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() { <-s.sem }()
				// go-socks5 handles entire SOCKS5 protocol: greeting, auth, connect, relay
				if err := s.server.ServeConn(conn); err != nil {
					s.log.WithError(err).Debug("socks5 connection ended with error")
				}
			}()
		default:
			s.log.Warn("socks5: connection rejected — too many concurrent connections")
			conn.Close()
		}
	}
}

// Shutdown stops accepting new connections and waits for active ones to drain.
func (s *Socks5Server) Shutdown(ctx context.Context) error {
	s.cancel()
	if s.ln != nil {
		s.ln.Close()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// trackedConnSocks5 wraps a connection to decrement active connections on close.
type trackedConnSocks5 struct {
	net.Conn
	slotName string
	slotUC   *usecase.SlotUseCase
	closed   bool
}

func (tc *trackedConnSocks5) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotUC.DecrementConnections(tc.slotName)
	}
	return tc.Conn.Close()
}
