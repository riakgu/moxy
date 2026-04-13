package usecase

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type ProxyDialer interface {
	Dial(req *model.DialRequest) (net.Conn, error)
	DialIPv6(req *model.DialRequest) (net.Conn, error)
	DialUDP(req *model.DialRequest) (*net.UDPConn, error)
	DialIPv6UDP(req *model.DialRequest) (*net.UDPConn, error)
}

type ProxyUseCase struct {
	Log           *slog.Logger
	SlotRepo      *repository.SlotRepository
	DeviceRepo    *repository.DeviceRepository
	Dialer        ProxyDialer
	strategy      string
	rrIndex       uint64
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
	strategy string,
	trafficRepo *repository.TrafficRepository,
) *ProxyUseCase {
	return &ProxyUseCase{
		Log:         log,
		SlotRepo:    slotRepo,
		DeviceRepo:  deviceRepo,
		Dialer:      dialer,
		strategy:    strategy,
		TrafficRepo: trafficRepo,
	}
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv4", "tcp", func(req *model.DialRequest) (net.Conn, error) {
		return c.Dialer.Dial(req)
	})
}

func (c *ProxyUseCase) ConnectIPv6(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv6", "tcp", func(req *model.DialRequest) (net.Conn, error) {
		return c.Dialer.DialIPv6(req)
	})
}

func (c *ProxyUseCase) ConnectUDP(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv4", "udp", func(req *model.DialRequest) (net.Conn, error) {
		return c.Dialer.DialUDP(req)
	})
}

func (c *ProxyUseCase) ConnectIPv6UDP(slotName string, targetAddr string) (net.Conn, error) {
	return c.dial(slotName, targetAddr, "ipv6", "udp", func(req *model.DialRequest) (net.Conn, error) {
		return c.Dialer.DialIPv6UDP(req)
	})
}

func (c *ProxyUseCase) selectSlot(slots []*entity.Slot) (*entity.Slot, error) {
	if len(slots) == 0 {
		return nil, model.ErrNoSlotsAvailable
	}
	switch c.strategy {
	case "round-robin":
		return c.roundRobin(slots), nil
	case "least-connections":
		return c.leastConnections(slots), nil
	default:
		return c.random(slots), nil
	}
}

func (c *ProxyUseCase) random(slots []*entity.Slot) *entity.Slot {
	return slots[rand.Intn(len(slots))]
}

func (c *ProxyUseCase) roundRobin(slots []*entity.Slot) *entity.Slot {
	idx := atomic.AddUint64(&c.rrIndex, 1)
	return slots[idx%uint64(len(slots))]
}

func (c *ProxyUseCase) leastConnections(slots []*entity.Slot) *entity.Slot {
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

func (c *ProxyUseCase) PickSlot() (string, error) {
	slots := c.SlotRepo.ListHealthy()
	slot, err := c.selectSlot(slots)
	if err != nil {
		return "", err
	}
	return slot.Name, nil
}

func (c *ProxyUseCase) PickSlotForDevice(deviceAlias string) (string, error) {
	slots := c.SlotRepo.ListHealthyForDevice(deviceAlias)
	slot, err := c.selectSlot(slots)
	if err != nil {
		return "", err
	}
	return slot.Name, nil
}

func (c *ProxyUseCase) dial(
	slotName string,
	targetAddr string,
	protocol string,
	transport string,
	dialFn func(req *model.DialRequest) (net.Conn, error),
) (net.Conn, error) {
	// Validate slot exists and is healthy before dialing
	slot, ok := c.SlotRepo.Get(slotName)
	if !ok {
		return nil, fmt.Errorf("slot %s not found", slotName)
	}
	if slot.Status != entity.SlotStatusHealthy {
		return nil, fmt.Errorf("slot %s is %s", slotName, slot.Status)
	}

	c.SlotRepo.IncrementConnections(slotName)
	atomic.StoreInt64(&slot.LastUsedAt, time.Now().UnixMilli())
	if c.EventPub != nil {
		c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
	}

	nameserver, nat64Prefix := slot.Nameserver, slot.NAT64Prefix

	conn, err := dialFn(&model.DialRequest{
		SlotName:    slotName,
		Addr:        targetAddr,
		Nameserver:  nameserver,
		NAT64Prefix: nat64Prefix,
	})
	if err != nil {
		c.SlotRepo.DecrementConnections(slotName)
		c.Log.Warn("dial failed", "slot", slotName, "target", targetAddr, "protocol", protocol, "transport", transport, "error", err)
		return nil, fmt.Errorf("dial-%s-%s %s via %s: %w", protocol, transport, targetAddr, slotName, err)
	}

	c.Log.Debug("connection established", "slot", slotName, "target", targetAddr, "protocol", protocol, "transport", transport)

	host, port, _ := net.SplitHostPort(targetAddr)
	deviceAlias := slot.DeviceAlias
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



type trackedConn struct {
	net.Conn
	slotName string
	slotRepo *repository.SlotRepository
	traffic  *entity.TrafficEntry
	once     sync.Once
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
	tc.once.Do(func() {
		tc.slotRepo.DecrementConnections(tc.slotName)
		if tc.traffic != nil {
			atomic.AddInt64(&tc.traffic.ActiveConnections, -1)
		}
	})
	return tc.Conn.Close()
}
