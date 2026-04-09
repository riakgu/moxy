package usecase

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

// Ensure trackedConn implements io.Reader and io.Writer explicitly
// so proxy libraries use our byte-counting Read/Write methods.

type SlotDialer interface {
	Dial(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
	DialIPv6(slotName string, addr string, nameserver string, nat64Prefix string) (net.Conn, error)
	DialUDP(slotName string, addr string, nameserver string, nat64Prefix string) (*net.UDPConn, error)
	DialIPv6UDP(slotName string, addr string, nameserver string, nat64Prefix string) (*net.UDPConn, error)
}

type ProxyUseCase struct {
	Log           *slog.Logger
	SlotRepo      *repository.SlotRepository
	DeviceRepo    *repository.DeviceRepository
	Dialer        SlotDialer
	Strategy      SlotStrategy
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
	dialer SlotDialer,
	strategy SlotStrategy,
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
	c.SlotRepo.IncrementConnections(slotName)
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}
	}

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.Dial(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warn("dial failed", "slot", slotName, "target", targetAddr, "error", err)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slotName, err)
	}

	c.Log.Debug("connection established", "slot", slotName, "target", targetAddr, "protocol", "ipv4")

	// Traffic stats
	host, port, _ := net.SplitHostPort(targetAddr)
	deviceAlias := ""
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		deviceAlias = slot.DeviceAlias
	}
	key := entity.TrafficKey{Domain: host, Port: port, DeviceAlias: deviceAlias, Protocol: "ipv4", Transport: "tcp"}
	entry := c.TrafficRepo.Record(key)
	atomic.AddInt64(&entry.ActiveConnections, 1)

	// Publish traffic + dns snapshots (debounced by EventHub)
	if c.EventPub != nil && c.TrafficUC != nil {
		c.EventPub.Publish("traffic_snapshot", c.TrafficUC.ListTop(c.SnapshotLimit))
		if c.DNSUC != nil {
			c.EventPub.Publish("dns_stats", c.DNSUC.GetCacheStats())
		}
	}

	tc := &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
		traffic:  entry,
	}

	return tc, nil
}

// ConnectIPv6 connects through a slot preferring native IPv6.
// Falls back to NAT64 if destination has no IPv6.
func (c *ProxyUseCase) ConnectIPv6(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}
	}

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.DialIPv6(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warn("dial failed", "slot", slotName, "target", targetAddr, "protocol", "ipv6", "error", err)
		return nil, fmt.Errorf("dial-ipv6 %s via %s: %w", targetAddr, slotName, err)
	}

	c.Log.Debug("connection established", "slot", slotName, "target", targetAddr, "protocol", "ipv6")

	// Traffic stats
	host, port, _ := net.SplitHostPort(targetAddr)
	deviceAlias := ""
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		deviceAlias = slot.DeviceAlias
	}
	key := entity.TrafficKey{Domain: host, Port: port, DeviceAlias: deviceAlias, Protocol: "ipv6", Transport: "tcp"}
	entry := c.TrafficRepo.Record(key)
	atomic.AddInt64(&entry.ActiveConnections, 1)

	// Publish traffic + dns snapshots (debounced by EventHub)
	if c.EventPub != nil && c.TrafficUC != nil {
		c.EventPub.Publish("traffic_snapshot", c.TrafficUC.ListTop(c.SnapshotLimit))
		if c.DNSUC != nil {
			c.EventPub.Publish("dns_stats", c.DNSUC.GetCacheStats())
		}
	}

	tc := &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
		traffic:  entry,
	}

	return tc, nil
}

// ConnectUDP connects via UDP through a slot's namespace.
// Returns net.Conn wrapping a *net.UDPConn with byte tracking.
// The go-socks5 library uses Read/Write on the returned conn for its UDP relay.
func (c *ProxyUseCase) ConnectUDP(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}
	}

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.DialUDP(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warn("udp dial failed", "slot", slotName, "target", targetAddr, "error", err)
		return nil, fmt.Errorf("dial-udp %s via %s: %w", targetAddr, slotName, err)
	}

	c.Log.Debug("udp connection established", "slot", slotName, "target", targetAddr, "protocol", "ipv4")

	host, port, _ := net.SplitHostPort(targetAddr)
	deviceAlias := ""
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		deviceAlias = slot.DeviceAlias
	}
	key := entity.TrafficKey{Domain: host, Port: port, DeviceAlias: deviceAlias, Protocol: "ipv4", Transport: "udp"}
	entry := c.TrafficRepo.Record(key)
	atomic.AddInt64(&entry.ActiveConnections, 1)

	if c.EventPub != nil && c.TrafficUC != nil {
		c.EventPub.Publish("traffic_snapshot", c.TrafficUC.ListTop(c.SnapshotLimit))
		if c.DNSUC != nil {
			c.EventPub.Publish("dns_stats", c.DNSUC.GetCacheStats())
		}
	}

	tc := &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
		traffic:  entry,
	}

	return tc, nil
}

// ConnectIPv6UDP connects via UDP through a slot's namespace preferring native IPv6.
func (c *ProxyUseCase) ConnectIPv6UDP(slotName string, targetAddr string) (net.Conn, error) {
	c.SlotRepo.IncrementConnections(slotName)
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
		if c.EventPub != nil {
			c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
		}
	}

	nameserver, nat64Prefix := c.getSlotConfig(slotName)

	conn, err := c.Dialer.DialIPv6UDP(slotName, targetAddr, nameserver, nat64Prefix)
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warn("udp dial failed", "slot", slotName, "target", targetAddr, "protocol", "ipv6", "error", err)
		return nil, fmt.Errorf("dial-udp-ipv6 %s via %s: %w", targetAddr, slotName, err)
	}

	c.Log.Debug("udp connection established", "slot", slotName, "target", targetAddr, "protocol", "ipv6")

	host, port, _ := net.SplitHostPort(targetAddr)
	deviceAlias := ""
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		deviceAlias = slot.DeviceAlias
	}
	key := entity.TrafficKey{Domain: host, Port: port, DeviceAlias: deviceAlias, Protocol: "ipv6", Transport: "udp"}
	entry := c.TrafficRepo.Record(key)
	atomic.AddInt64(&entry.ActiveConnections, 1)

	if c.EventPub != nil && c.TrafficUC != nil {
		c.EventPub.Publish("traffic_snapshot", c.TrafficUC.ListTop(c.SnapshotLimit))
		if c.DNSUC != nil {
			c.EventPub.Publish("dns_stats", c.DNSUC.GetCacheStats())
		}
	}

	tc := &trackedConn{
		Conn:     conn,
		slotName: slotName,
		slotRepo: c.SlotRepo,
		traffic:  entry,
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
