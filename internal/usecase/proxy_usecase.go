package usecase

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/repository"
)

// Ensure trackedConn implements io.Reader and io.Writer explicitly
// so proxy libraries use our byte-counting Read/Write methods.

type SlotDialer interface {
	Dial(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
	DialIPv6(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
}

type ProxyUseCase struct {
	Log        *logrus.Logger
	SlotRepo   *repository.SlotRepository
	DeviceRepo *repository.DeviceRepository
	Dialer     SlotDialer
	Strategy   SlotStrategy
}

func NewProxyUseCase(log *logrus.Logger, slotRepo *repository.SlotRepository, deviceRepo *repository.DeviceRepository, dialer SlotDialer, strategy SlotStrategy) *ProxyUseCase {
	return &ProxyUseCase{
		Log:        log,
		SlotRepo:   slotRepo,
		DeviceRepo: deviceRepo,
		Dialer:     dialer,
		Strategy:   strategy,
	}
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
	}

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.Dial(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warnf("proxy: dial %s via %s failed: %v", targetAddr, slotName, err)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slotName, err)
	}

	tc := &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
	}

	// Wire byte counters to parent device
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		if device, ok := c.DeviceRepo.GetByAlias(slot.DeviceAlias); ok {
			tc.deviceRx = &device.RxBytes
			tc.deviceTx = &device.TxBytes
		}
	}

	return tc, nil
}

// ConnectIPv6 connects through a slot preferring native IPv6.
// Falls back to NAT64 if destination has no IPv6.
func (c *ProxyUseCase) ConnectIPv6(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
	}

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.DialIPv6(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warnf("proxy-ipv6: dial %s via %s failed: %v", targetAddr, slotName, err)
		return nil, fmt.Errorf("dial-ipv6 %s via %s: %w", targetAddr, slotName, err)
	}

	tc := &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
	}

	if slot, ok := c.SlotRepo.Get(slotName); ok {
		if device, ok := c.DeviceRepo.GetByAlias(slot.DeviceAlias); ok {
			tc.deviceRx = &device.RxBytes
			tc.deviceTx = &device.TxBytes
		}
	}

	return tc, nil
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
	deviceRx *uint64 // points to &device.RxBytes for atomic counting
	deviceTx *uint64 // points to &device.TxBytes for atomic counting
	closed   bool
}

func (tc *trackedConn) Read(b []byte) (int, error) {
	n, err := tc.Conn.Read(b)
	if n > 0 && tc.deviceRx != nil {
		atomic.AddUint64(tc.deviceRx, uint64(n))
	}
	return n, err
}

func (tc *trackedConn) Write(b []byte) (int, error) {
	n, err := tc.Conn.Write(b)
	if n > 0 && tc.deviceTx != nil {
		atomic.AddUint64(tc.deviceTx, uint64(n))
	}
	return n, err
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotRepo.DecrementConnections(tc.slotName)
	}
	return tc.Conn.Close()
}
