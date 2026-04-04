package usecase

import (
	"fmt"
	"math/rand"
	"net"
	"sync/atomic"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/repository"
)

const (
	StrategyRandom           = "random"
	StrategyRoundRobin       = "round-robin"
	StrategyLeastConnections = "least-connections"
	StrategyStickyIP         = "sticky-ip"
)

type SlotDialer interface {
	Dial(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
}

type ProxyUseCase struct {
	Log      *logrus.Logger
	SlotRepo *repository.SlotRepository
	Dialer   SlotDialer
	Strategy string
	rrIndex  uint64
}

func NewProxyUseCase(log *logrus.Logger, slotRepo *repository.SlotRepository, dialer SlotDialer, strategy string) *ProxyUseCase {
	return &ProxyUseCase{
		Log:      log,
		SlotRepo: slotRepo,
		Dialer:   dialer,
		Strategy: strategy,
	}
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)

	// Look up slot ISP config for this connection
	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.Dial(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slotName, err)
	}

	return &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
	}, nil
}

// SelectSlot picks a slot using the configured load balancing strategy.
func (c *ProxyUseCase) SelectSlot(clientIP string) (string, error) {
	healthy := c.SlotRepo.ListHealthy()
	if len(healthy) == 0 {
		return "", model.ErrNoSlotsAvailable
	}

	var slot *entity.Slot

	switch c.Strategy {
	case StrategyRoundRobin:
		idx := atomic.AddUint64(&c.rrIndex, 1)
		slot = healthy[idx%uint64(len(healthy))]

	case StrategyLeastConnections:
		best := healthy[0]
		bestConns := atomic.LoadInt64(&best.ActiveConnections)
		for _, s := range healthy[1:] {
			conns := atomic.LoadInt64(&s.ActiveConnections)
			if conns < bestConns {
				best = s
				bestConns = conns
			}
		}
		slot = best

	case StrategyStickyIP:
		if clientIP == "" {
			slot = healthy[rand.Intn(len(healthy))]
		} else {
			hash := fnvHash(clientIP)
			slot = healthy[hash%uint64(len(healthy))]
		}

	default: // random
		slot = healthy[rand.Intn(len(healthy))]
	}

	return slot.Name, nil
}

// SelectSlotForDevice picks a slot from a specific device using the load balancing strategy.
func (c *ProxyUseCase) SelectSlotForDevice(deviceAlias string, clientIP string) (string, error) {
	healthy := c.SlotRepo.ListHealthyForDevice(deviceAlias)
	if len(healthy) == 0 {
		return "", model.ErrNoSlotsAvailable
	}

	var slot *entity.Slot

	switch c.Strategy {
	case StrategyRoundRobin:
		idx := atomic.AddUint64(&c.rrIndex, 1)
		slot = healthy[idx%uint64(len(healthy))]

	case StrategyLeastConnections:
		best := healthy[0]
		bestConns := atomic.LoadInt64(&best.ActiveConnections)
		for _, s := range healthy[1:] {
			conns := atomic.LoadInt64(&s.ActiveConnections)
			if conns < bestConns {
				best = s
				bestConns = conns
			}
		}
		slot = best

	case StrategyStickyIP:
		if clientIP == "" {
			slot = healthy[rand.Intn(len(healthy))]
		} else {
			hash := fnvHash(clientIP)
			slot = healthy[hash%uint64(len(healthy))]
		}

	default: // random
		slot = healthy[rand.Intn(len(healthy))]
	}

	return slot.Name, nil
}

// getSlotConfig returns the ISP config for a slot.
func (c *ProxyUseCase) getSlotConfig(name string) (nameserver, nat64Prefix string) {
	if slot, ok := c.SlotRepo.Get(name); ok {
		return slot.Nameserver, slot.NAT64Prefix
	}
	return "", ""
}

func fnvHash(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
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
