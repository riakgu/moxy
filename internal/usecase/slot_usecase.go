package usecase

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotProvisioner interface {
	CreateSlot(req *model.CreateSlotRequest) error
	DestroySlot(req *model.DestroySlotRequest) error
	ReattachSlot(req *model.ReattachSlotRequest) error
	EnableNDPProxy(req *model.EnableNDPProxyRequest) error
	AddNDPProxyEntry(req *model.NDPProxyEntryRequest) error
	RemoveNDPProxyEntry(req *model.NDPProxyEntryRequest) error
	ListSlotNamespaces() ([]string, error)
	CleanupNamespaces(req *model.CleanupNamespacesRequest) (int, error)
	ConfigureDHCP(req *model.ConfigureDHCPRequest) error
	ConfigureIPv6SLAAC(req *model.ConfigureIPv6SLAACRequest) error
	BringInterfaceUp(req *model.BringInterfaceUpRequest) error
}

type SlotDiscovery interface {
	ResolveSlotIP(req *model.ResolveSlotRequest) (string, error)
	ResolveSlotIPInfo(req *model.ResolveSlotRequest) (*model.SlotIPInfoResult, error)
	ResolveSlotIPv6(req *model.ResolveSlotRequest) (string, error)
}

const slaacWaitDuration = 5 * time.Second

type SlotUseCase struct {
	Log              *slog.Logger
	SlotRepo         *repository.SlotRepository
	Discovery        SlotDiscovery
	Provisioner      SlotProvisioner
	MaxSlotsPerDevice int
	Monitor          *SlotMonitorUseCase
	EventPub         EventPublisher
}

func NewSlotUseCase(
	log *slog.Logger,
	slotRepo *repository.SlotRepository,
	discovery SlotDiscovery,
	provisioner SlotProvisioner,
	maxSlotsPerDevice int,
) *SlotUseCase {
	if maxSlotsPerDevice <= 0 {
		maxSlotsPerDevice = 250
	}
	return &SlotUseCase{
		Log:              log,
		SlotRepo:         slotRepo,
		Discovery:        discovery,
		Provisioner:      provisioner,
		MaxSlotsPerDevice: maxSlotsPerDevice,
	}
}

// Must be called after construction to break the circular dependency.
func (c *SlotUseCase) SetMonitor(m *SlotMonitorUseCase) {
	c.Monitor = m
}

func (c *SlotUseCase) publishSlot(name string) {
	if c.EventPub == nil {
		return
	}
	slot, ok := c.SlotRepo.Get(name)
	if !ok {
		return
	}
	c.EventPub.Publish("slot_updated", converter.SlotToResponse(slot))
}

func (c *SlotUseCase) publishSlotRemoved(name string) {
	if c.EventPub == nil {
		return
	}
	c.EventPub.Publish("slot_removed", map[string]string{"name": name})
}

func (c *SlotUseCase) GetSlotNames() []string {
	return c.SlotRepo.ListNames()
}

func (c *SlotUseCase) ListAll() []model.SlotResponse {
	slots := c.SlotRepo.ListAll()
	result := make([]model.SlotResponse, 0, len(slots))
	for _, s := range slots {
		result = append(result, *converter.SlotToResponse(s))
	}
	sort.Slice(result, func(i, j int) bool {
		return naturalSlotLess(result[i].Name, result[j].Name)
	})
	return result
}

func (c *SlotUseCase) GetByName(request *model.GetSlotRequest) (*model.SlotResponse, error) {
	slot, ok := c.SlotRepo.Get(request.SlotName)
	if !ok {
		return nil, fmt.Errorf("slot %s not found", request.SlotName)
	}
	return converter.SlotToResponse(slot), nil
}

func parseSlotIndex(slotName string) (int, error) {
	if !strings.HasPrefix(slotName, "slot") {
		return 0, fmt.Errorf("invalid slot name %s: missing slot prefix", slotName)
	}
	idx, err := strconv.Atoi(slotName[4:])
	if err != nil {
		return 0, fmt.Errorf("invalid slot name %s: cannot parse index", slotName)
	}
	return idx, nil
}

func (c *SlotUseCase) rerollSlotNamespace(slotName string, slotIndex int, iface string, dns64 string) (newIPv4, newIPv6 string, err error) {
	var oldIPv6 string
	if s, ok := c.SlotRepo.Get(slotName); ok {
		oldIPv6 = s.IPv6Address
		if s.Interface != "" {
			iface = s.Interface
		}
		if s.Nameserver != "" {
			dns64 = s.Nameserver
		}
	}

	if oldIPv6 != "" {
		if err := c.Provisioner.RemoveNDPProxyEntry(&model.NDPProxyEntryRequest{IPv6: oldIPv6, Interface: iface}); err != nil {
			c.Log.Warn("failed to remove old ndp proxy", "slot", slotName, "ipv6", oldIPv6, "error", err)
		}
	}

	if err := c.Provisioner.DestroySlot(&model.DestroySlotRequest{Name: slotName}); err != nil {
		c.Log.Warn("failed to destroy slot for reroll", "slot", slotName, "error", err)
	}
	if err := c.Provisioner.CreateSlot(&model.CreateSlotRequest{SlotIndex: slotIndex, Interface: iface, DNS64: dns64}); err != nil {
		return "", "", fmt.Errorf("recreate %s: %w", slotName, err)
	}

	time.Sleep(slaacWaitDuration)

	resolveReq := &model.ResolveSlotRequest{SlotName: slotName, Nameserver: dns64}
	newIPv4, err = c.Discovery.ResolveSlotIP(resolveReq)
	if err != nil {
		return "", "", fmt.Errorf("resolve %s: %w", slotName, err)
	}
	newIPv6, _ = c.Discovery.ResolveSlotIPv6(resolveReq)

	if newIPv6 != "" {
		if err := c.Provisioner.AddNDPProxyEntry(&model.NDPProxyEntryRequest{IPv6: newIPv6, Interface: iface}); err != nil {
			c.Log.Warn("failed to add ndp proxy", "slot", slotName, "ipv6", newIPv6, "error", err)
		}
	}

	if slot, ok := c.SlotRepo.Get(slotName); ok {
		slot.IPv4Address = newIPv4
		slot.IPv6Address = newIPv6
		slot.Status = entity.SlotStatusHealthy
	}

	return newIPv4, newIPv6, nil
}

func (c *SlotUseCase) RecycleSlot(request *model.ChangeIPRequest) (*model.SlotResponse, error) {

	slotIndex, err := parseSlotIndex(request.SlotName)
	if err != nil {
		return nil, err
	}

	slot, ok := c.SlotRepo.Get(request.SlotName)
	if !ok {
		return nil, model.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		return nil, model.ErrSlotBusy
	}

	slot.Status = entity.SlotStatusDiscovering
	c.Log.Info("recycling slot", "slot", request.SlotName, "index", slotIndex)

	newIPv4, _, err := c.rerollSlotNamespace(request.SlotName, slotIndex, slot.Interface, slot.Nameserver)
	if err != nil {
		slot.Status = entity.SlotStatusUnhealthy
		c.Log.Warn("recycle failed", "slot", request.SlotName, "error", err)
		c.publishSlot(request.SlotName)
		return nil, err
	}

	if c.Monitor != nil {
		c.Monitor.StopSlot(request.SlotName)
		c.Monitor.StartSlot(request.SlotName)
	}
	c.publishSlot(request.SlotName)

	response := converter.SlotToResponse(slot)
	c.Log.Info("slot recycled", "slot", request.SlotName, "ipv4", newIPv4)
	return response, nil
}

func (c *SlotUseCase) ProvisionSlots(deviceAlias string, iface string, count int, nameserver string, nat64Prefix string) (*model.ProvisionResponse, error) {
	dns64 := nameserver

	if err := c.Provisioner.EnableNDPProxy(&model.EnableNDPProxyRequest{Interface: iface}); err != nil {
		return nil, fmt.Errorf("enable NDP proxy: %w", err)
	}

	existingCount := c.SlotRepo.CountByDevice(deviceAlias)

	// Per-device cap
	maxForDevice := c.MaxSlotsPerDevice
	if count > maxForDevice {
		c.Log.Warn("slot count capped by per-device limit", "device", deviceAlias, "requested", count, "max", maxForDevice)
		count = maxForDevice
	}

	if existingCount >= count {
		c.Log.Info("slots sufficient, skipping", "device", deviceAlias, "existing", existingCount, "requested", count)
		return &model.ProvisionResponse{
			Created: 0,
			Failed:  0,
			Total:   existingCount,
		}, nil
	}

	created := 0
	failed := 0
	toCreate := count - existingCount

	var createdNames []string
	for i := 0; i < toCreate; i++ {
		idx, err := c.SlotRepo.NextSlotIndex()
		if err != nil {
			c.Log.Warn("slot pool exhausted", "device", deviceAlias, "created_so_far", created, "error", err)
			break
		}
		slotName := fmt.Sprintf("slot%d", idx)
		c.Log.Info("provisioning slot", "slot", slotName, "progress", fmt.Sprintf("%d/%d", i+1, toCreate))
		if err := c.Provisioner.CreateSlot(&model.CreateSlotRequest{SlotIndex: idx, Interface: iface, DNS64: dns64}); err != nil {
			c.Log.Error("provision failed", "slot", slotName, "error", err)
			c.SlotRepo.ReleaseIndex(idx)
			failed++
			continue
		}
		// Pre-register slot in repo so the monitor can find it
		c.SlotRepo.Put(&entity.Slot{
			Name:        slotName,
			DeviceAlias: deviceAlias,
			Interface:   iface,
			Nameserver:  nameserver,
			NAT64Prefix: nat64Prefix,
			Status:      entity.SlotStatusDiscovering,
		})
		createdNames = append(createdNames, slotName)
		c.publishSlot(slotName)
		created++
	}

	time.Sleep(slaacWaitDuration)

	for _, name := range createdNames {
		if c.Monitor != nil {
			c.Monitor.StartSlot(name)
		}
	}

	pairSet := make(map[string]bool)
	for _, s := range c.SlotRepo.ListAll() {
		if s.Status == entity.SlotStatusHealthy && s.IPv4Address != "" {
			pairSet[s.IPv4Address] = true
		}
	}

	return &model.ProvisionResponse{
		Created:   created,
		Failed:    failed,
		Total:     existingCount + created,
		UniqueIPs: len(pairSet),
	}, nil
}

func (c *SlotUseCase) DestroySlot(req *model.DeleteSlotRequest) error {
	slotName := req.SlotName
	if c.Monitor != nil {
		c.Monitor.StopSlot(slotName)
	}

	slot, ok := c.SlotRepo.Get(slotName)
	if !ok {
		return model.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		return model.ErrSlotBusy
	}

	ipv6 := slot.IPv6Address
	iface := slot.Interface
	c.SlotRepo.Delete(slotName)
	c.publishSlotRemoved(slotName)

	if ipv6 != "" && iface != "" {
		if err := c.Provisioner.RemoveNDPProxyEntry(&model.NDPProxyEntryRequest{IPv6: ipv6, Interface: iface}); err != nil {
			c.Log.Warn("ndp proxy removal failed", "slot", slotName, "error", err)
		}
	}

	if err := c.Provisioner.DestroySlot(&model.DestroySlotRequest{Name: slotName}); err != nil {
		return fmt.Errorf("destroy %s: %w", slotName, err)
	}

	c.Log.Info("slot destroyed", "slot", slotName)
	return nil
}

func (uc *SlotUseCase) CleanupOrphans() (int, error) {
	tracked := uc.SlotRepo.ListAllNames()
	cleaned, err := uc.Provisioner.CleanupNamespaces(&model.CleanupNamespacesRequest{Keep: tracked})
	if err != nil {
		return 0, fmt.Errorf("cleanup orphans: %w", err)
	}
	return cleaned, nil
}

func (c *SlotUseCase) SuspendByDevice(deviceAlias string) {
	slotNames := c.SlotRepo.ListNamesForDevice(deviceAlias)
	for _, name := range slotNames {
		c.SlotRepo.SetStatus(name, entity.SlotStatusSuspended)
		if c.Monitor != nil {
			c.Monitor.StopSlot(name)
		}
	}
}

func (c *SlotUseCase) ResumeByDevice(deviceAlias string) {
	slotNames := c.SlotRepo.ListNamesForDevice(deviceAlias)
	for _, name := range slotNames {
		c.SlotRepo.CompareAndSetStatus(name, entity.SlotStatusSuspended, entity.SlotStatusHealthy)
	}
}

func (c *SlotUseCase) TeardownByDevice(deviceAlias string, drainTimeout time.Duration) int {
	slotNames := c.SlotRepo.ListNamesForDevice(deviceAlias)
	for _, name := range slotNames {
		if c.Monitor != nil {
			c.Monitor.StopSlot(name)
		}
		if drainTimeout > 0 {
			if remaining := c.drainSlot(name, drainTimeout); remaining > 0 {
				c.Log.Warn("forcing slot destroy", "device", deviceAlias, "slot", name, "active_connections", remaining)
			}
		}
		// Remove NDP proxy entry before destroying namespace
		if slot, ok := c.SlotRepo.Get(name); ok && slot.IPv6Address != "" && slot.Interface != "" {
			if err := c.Provisioner.RemoveNDPProxyEntry(&model.NDPProxyEntryRequest{IPv6: slot.IPv6Address, Interface: slot.Interface}); err != nil {
				c.Log.Warn("ndp proxy removal failed", "slot", name, "error", err)
			}
		}
		if err := c.Provisioner.DestroySlot(&model.DestroySlotRequest{Name: name}); err != nil {
			c.Log.Warn("slot destroy failed", "device", deviceAlias, "slot", name, "error", err)
		}
		c.publishSlotRemoved(name)
	}
	removed := c.SlotRepo.DeleteByDevice(deviceAlias)
	c.Log.Info("slots teardown complete", "device", deviceAlias, "slots_removed", removed)
	return removed
}

func (c *SlotUseCase) ReattachByDevice(deviceAlias string, iface string) int {
	slotNames := c.SlotRepo.ListNamesForDevice(deviceAlias)
	reattached := 0
	for _, name := range slotNames {
		if err := c.Provisioner.ReattachSlot(&model.ReattachSlotRequest{SlotName: name, Interface: iface}); err != nil {
			c.Log.Warn("slot re-attach failed", "slot", name, "error", err)
			c.SlotRepo.SetStatus(name, entity.SlotStatusUnhealthy)
			continue
		}
		reattached++
	}

	c.ResumeByDevice(deviceAlias)

	if c.Monitor != nil {
		for _, name := range slotNames {
			if slot, ok := c.SlotRepo.Get(name); ok && slot.Status == entity.SlotStatusHealthy {
				c.Monitor.StopSlot(name)
				c.Monitor.StartSlot(name)
			}
		}
	}

	return reattached
}

func (c *SlotUseCase) drainSlot(name string, timeout time.Duration) int64 {
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		slot, ok := c.SlotRepo.Get(name)
		if !ok {
			return 0
		}
		conns := atomic.LoadInt64(&slot.ActiveConnections)
		if conns <= 0 {
			return 0
		}
		select {
		case <-deadline:
			return conns
		case <-ticker.C:
		}
	}
}

func naturalSlotLess(a, b string) bool {
	aNum, aErr := strconv.Atoi(strings.TrimPrefix(a, "slot"))
	bNum, bErr := strconv.Atoi(strings.TrimPrefix(b, "slot"))
	if aErr == nil && bErr == nil {
		return aNum < bNum
	}
	return a < b
}

