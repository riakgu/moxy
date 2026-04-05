package usecase

import (
	"fmt"
	"net"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotDialer interface {
	Dial(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
}

type ProxyUseCase struct {
	Log      *logrus.Logger
	SlotRepo *repository.SlotRepository
	Dialer   SlotDialer
	Strategy SlotStrategy
}

func NewProxyUseCase(log *logrus.Logger, slotRepo *repository.SlotRepository, dialer SlotDialer, strategy SlotStrategy) *ProxyUseCase {
	return &ProxyUseCase{
		Log:      log,
		SlotRepo: slotRepo,
		Dialer:   dialer,
		Strategy: strategy,
	}
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.Dial(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warnf("proxy: dial %s via %s failed: %v", targetAddr, slotName, err)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slotName, err)
	}

	return &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
	}, nil
}

// SelectSlot picks a slot from the given candidates using the configured strategy.
func (c *ProxyUseCase) SelectSlot(slots []*entity.Slot) (*entity.Slot, error) {
	if len(slots) == 0 {
		return nil, model.ErrNoSlotsAvailable
	}
	return c.Strategy.Select(slots), nil
}

func (c *ProxyUseCase) getSlotConfig(name string) (nameserver, nat64Prefix string) {
	if slot, ok := c.SlotRepo.Get(name); ok {
		return slot.Nameserver, slot.NAT64Prefix
	}
	return "", ""
}

type trackedConn struct {
	net.Conn
	slotName string
	slotRepo *repository.SlotRepository
	closed   bool
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotRepo.DecrementConnections(tc.slotName)
	}
	return tc.Conn.Close()
}
