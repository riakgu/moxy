package usecase

import (
	"fmt"
	"math/rand"
	"net"
	"sync/atomic"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type ProxyDialer interface {
	Dial(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
	DialIPv6(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
	DialUDP(slotName string, addr string, nameserver string, nat64Prefix string) (*net.UDPConn, error)
	DialIPv6UDP(slotName string, addr string, nameserver string, nat64Prefix string) (*net.UDPConn, error)
}

type BalancingStrategy func(slots []*entity.Slot) *entity.Slot

func NewBalancingStrategy(name string) BalancingStrategy {
	switch name {
	case "round-robin":
		return newRoundRobin()
	case "least-connections":
		return leastConnections
	default:
		return randomBalancing
	}
}

func randomBalancing(slots []*entity.Slot) *entity.Slot {
	return slots[rand.Intn(len(slots))]
}

func newRoundRobin() BalancingStrategy {
	var index uint64
	return func(slots []*entity.Slot) *entity.Slot {
		idx := atomic.AddUint64(&index, 1)
		return slots[idx%uint64(len(slots))]
	}
}

func leastConnections(slots []*entity.Slot) *entity.Slot {
	best := slots[0]
	bestConns := atomic.LoadInt64(&best.ActiveConnections)
	for _, slot := range slots[1:] {
		conns := atomic.LoadInt64(&slot.ActiveConnections)
		if conns < bestConns {
			best = slot
			bestConns = conns
		}
	}
	return best
}

type ProxyUseCase struct {
	Log           *slog.Logger
	SlotRepo      *repository.SlotRepository
	DeviceRepo    *repository.DeviceRepository
	Dialer        ProxyDialer
	Strategy      BalancingStrategy
	TrafficRepo   *repository.TrafficRepository
	EventPub      EventPublisher
	TrafficUC     *TrafficUseCase
	DNSUC         *DNSUseCase
	SnapshotLimit int
}

func NewProxyUseCase(
	log *slog.Logger,
	slotRepo *repository.SlotRepository,
	deviceRepo *repository.DeviceRepository,
	dialer ProxyDialer,
	strategy BalancingStrategy,
	trafficRepo *repository.TrafficRepository,
) *ProxyUseCase {
	return &ProxyUseCase{
		Log:         log,
		SlotRepo:    slotRepo,
		DeviceRepo:  deviceRepo,
		Dialer:      dialer,
		Strategy:    strategy,
		TrafficRepo: trafficRepo,
	}
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv4", "tcp", func(ns, addr, dns, nat64 string) (net.Conn, error) {
		return c.Dialer.Dial(ns, addr, dns, nat64)
	})
}

func (c *ProxyUseCase) ConnectIPv6(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv6", "tcp", func(ns, addr, dns, nat64 string) (net.Conn, error) {
		return c.Dialer.DialIPv6(ns, addr, dns, nat64)
	})
}

func (c *ProxyUseCase) ConnectUDP(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv4", "udp", func(ns, addr, dns, nat64 string) (net.Conn, error) {
		return c.Dialer.DialUDP(ns, addr, dns, nat64)
	})
}

func (c *ProxyUseCase) ConnectIPv6UDP(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv6", "udp", func(ns, addr, dns, nat64 string) (net.Conn, error) {
		return c.Dialer.DialIPv6UDP(ns, addr, dns, nat64)
	})
}

func (c *ProxyUseCase) SelectSlot(slots []*entity.Slot) (*entity.Slot, error) {
	if len(slots) == 0 {
		return nil, entity.ErrNoSlotsAvailable
	}
	return c.Strategy(slots), nil
}

func (c *ProxyUseCase) dial(
	slotName string,
	targetAddr string,
	protocol string,
	transport string,
	dialFn func(ns, addr, dns, nat64 string) (net.Conn, error),
) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}
	}

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := dialFn(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warn("dial failed", "slot", slotName, "target", targetAddr, "protocol", protocol, "transport", transport, "error", err)
		return nil, fmt.Errorf("dial-%s-%s %s via %s: %w", protocol, transport, targetAddr, slotName, err)
	}

	c.Log.Debug("connection established", "slot", slotName, "target", targetAddr, "protocol", protocol, "transport", transport)

	host, port, _ := net.SplitHostPort(targetAddr)
	deviceAlias := ""
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		deviceAlias = slot.DeviceAlias
	}
	key := entity.TrafficKey{Domain: host, Port: port, DeviceAlias: deviceAlias, Protocol: protocol, Transport: transport}
	entry := c.TrafficRepo.Record(key)
	atomic.AddInt64(&entry.ActiveConnections, 1)

	if c.EventPub != nil && c.TrafficUC != nil {
		c.EventPub.Publish("traffic_snapshot", c.TrafficUC.ListTop(c.SnapshotLimit))
		if c.DNSUC != nil {
			c.EventPub.Publish("dns_stats", c.DNSUC.GetCacheStats())
		}
	}

	return &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
		traffic:  entry,
	}, nil
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
	traffic  *entity.TrafficEntry
	closed   bool
}

func (tc *trackedConn) Read(b []byte) (int, error) {
	n, err := tc.Conn.Read(b)
	if n > 0 && tc.traffic != nil {
		atomic.AddUint64(&tc.traffic.RxBytes, uint64(n))
	}
	return n, err
}

func (tc *trackedConn) Write(b []byte) (int, error) {
	n, err := tc.Conn.Write(b)
	if n > 0 && tc.traffic != nil {
		atomic.AddUint64(&tc.traffic.TxBytes, uint64(n))
	}
	return n, err
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotRepo.DecrementConnections(tc.slotName)
		if tc.traffic != nil {
			atomic.AddInt64(&tc.traffic.ActiveConnections, -1)
		}
	}
	return tc.Conn.Close()
}
