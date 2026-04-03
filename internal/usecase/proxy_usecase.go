package usecase

import (
	"fmt"
	"net"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
)

type SlotDialer interface {
	Dial(slotName string, addr string) (net.Conn, error)
}

type ProxyUseCase struct {
	Log    *logrus.Logger
	SlotUC *SlotUseCase
	Dialer SlotDialer
}

func NewProxyUseCase(log *logrus.Logger, slotUC *SlotUseCase, dialer SlotDialer) *ProxyUseCase {
	return &ProxyUseCase{
		Log:    log,
		SlotUC: slotUC,
		Dialer: dialer,
	}
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotUC.IncrementConnections(slotName)

	conn, err := c.Dialer.Dial(slotName, targetAddr)
	if err != nil {
		c.SlotUC.DecrementConnections(slotName)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slotName, err)
	}

	return &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotUC:   c.SlotUC,
	}, nil
}

// SelectSlot picks a slot using the configured load balancing strategy.
func (c *ProxyUseCase) SelectSlot(clientIP string) (string, error) {
	slot, err := c.SlotUC.SelectSlot(clientIP)
	if err != nil {
		return "", model.ErrNoSlotsAvailable
	}
	return slot.Name, nil
}

type trackedConn struct {
	net.Conn
	slotName string
	slotUC   *SlotUseCase
	closed   bool
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotUC.DecrementConnections(tc.slotName)
	}
	return tc.Conn.Close()
}
