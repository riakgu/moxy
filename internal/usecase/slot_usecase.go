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
	CreateSlot(slotIndex int, iface string, dns64 string) error
	DestroySlot(name string) error
	ReattachSlot(slotName string, iface string) error
	EnableNDPProxy(iface string) error
	AddNDPProxyEntry(ipv6 string, iface string) error
	RemoveNDPProxyEntry(ipv6 string, iface string) error
	ListSlotNamespaces() ([]string, error)
	CleanupNamespaces(keep []string) (int, error)
	ConfigureDHCP(iface string) error
	ConfigureIPv6SLAAC(iface string) error
}

type SlotDiscovery interface {
	ResolveSlotIP(slotName string) (string, error)
	ResolveSlotIPInfo(slotName string) (ip, city, asn, org, rtt string, err error)
	ResolveSlotIPv6(slotName string) (string, error)
}

const slaacWaitDuration = 5 * time.Second

type SlotUseCase struct {
	Log         *slog.Logger
	SlotRepo    *repository.SlotRepository
	Discovery   SlotDiscovery
	Provisioner SlotProvisioner
	MaxSlots    int
	Monitor     *SlotMonitorUseCase
	EventPub    EventPublisher
}

func NewSlotUseCase(
	log *slog.Logger,
	slotRepo *repository.SlotRepository,
	discovery SlotDiscovery,
	provisioner SlotProvisioner,
	maxSlots int,
) *SlotUseCase {
	return &SlotUseCase{
		Log:         log,
		SlotRepo:    slotRepo,
		Discovery:   discovery,
		Provisioner: provisioner,
		MaxSlots:    maxSlots,
	}
}

// SetMonitor sets the slot monitor for this use case.
// Must be called after construction to break the circular dependency.
func (c *SlotUseCase) SetMonitor(m *SlotMonitorUseCase) {
	c.Monitor = m
}

// publishSlot publishes the current state of a slot as a slot_updated event.
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

// publishSlotRemoved publishes a slot_removed event.
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

// parseSlotIndex extracts the slot index from names like "slot3"
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

// rerollSlotNamespace destroys and recreates a slot's namespace to get a new IP.
// Returns the new IPv4, IPv6, and any error.
func (c *SlotUseCase) rerollSlotNamespace(slotName string, slotIndex int, iface string, dns64 string) (newIPv4, newIPv6 string, err error) {
	// Get current state
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

	// Remove old NDP proxy
	if oldIPv6 != "" {
		if err := c.Provisioner.RemoveNDPProxyEntry(oldIPv6, iface); err != nil {
			c.Log.Warn("failed to remove old ndp proxy", "slot", slotName, "ipv6", oldIPv6, "error", err)
		}
	}

	// Destroy + recreate
	if err := c.Provisioner.DestroySlot(slotName); err != nil {
		c.Log.Warn("failed to destroy slot for reroll", "slot", slotName, "error", err)
	}
	if err := c.Provisioner.CreateSlot(slotIndex, iface, dns64); err != nil {
		return "", "", fmt.Errorf("recreate %s: %w", slotName, err)
	}

	time.Sleep(slaacWaitDuration)

	// Resolve new IPs
	newIPv4, err = c.Discovery.ResolveSlotIP(slotName)
	if err != nil {
		return "", "", fmt.Errorf("resolve %s: %w", slotName, err)
	}
	newIPv6, _ = c.Discovery.ResolveSlotIPv6(slotName)

	// Add new NDP proxy
	if newIPv6 != "" {
		if err := c.Provisioner.AddNDPProxyEntry(newIPv6, iface); err != nil {
			c.Log.Warn("failed to add ndp proxy", "slot", slotName, "ipv6", newIPv6, "error", err)
		}
	}

	// Update slot in repo
	if slot, ok := c.SlotRepo.Get(slotName); ok {
		slot.PublicIPv4s = []string{newIPv4}
		slot.IPv6Address = newIPv6
		slot.Status = entity.SlotStatusHealthy
		slot.LastCheckedAt = time.Now().UnixMilli()
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
		return nil, entity.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		return nil, entity.ErrSlotBusy
	}

	slot.Status = entity.SlotStatusDiscovering
	c.Log.Info("recycling slot", "slot", request.SlotName, "index", slotIndex)

	newIPv4, _, err := c.rerollSlotNamespace(request.SlotName, slotIndex, slot.Interface, slot.Nameserver)
	if err != nil {
		slot.Status = entity.SlotStatusUnhealthy
		c.Log.Warn("recycle failed", "slot", request.SlotName, "error", err)
		return nil, err
	}

	// Restart monitor in FAST_CHECK mode
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
	// Use auto-detected nameserver for resolv.conf inside namespaces
	dns64 := nameserver

	// Enable NDP proxy on the interface
	if err := c.Provisioner.EnableNDPProxy(iface); err != nil {
		return nil, fmt.Errorf("enable NDP proxy: %w", err)
	}

	// count = total desired slots (declarative)
	existingCount := c.SlotRepo.CountByDevice(deviceAlias)

	if existingCount >= count {
		c.Log.Info("slots sufficient, skipping", "device", deviceAlias, "existing", existingCount, "requested", count)
		return &model.ProvisionResponse{
			Created: 0,
			Failed:  0,
			Total:   existingCount,
		}, nil
	}

	// Check max slots limit
	target := count
	if c.MaxSlots > 0 && target > c.MaxSlots {
		c.Log.Warn("slot count capped", "max", c.MaxSlots)
		target = c.MaxSlots
	}

	created := 0
	failed := 0
	toCreate := target - existingCount
	if toCreate < 0 {
		toCreate = 0
	}

	// Use globally unique slot indices
	var createdNames []string
	for i := 0; i < toCreate; i++ {
		idx := c.SlotRepo.NextSlotIndex()
		slotName := fmt.Sprintf("slot%d", idx)
		c.Log.Info("provisioning slot", "slot", slotName, "progress", fmt.Sprintf("%d/%d", i+1, toCreate))
		if err := c.Provisioner.CreateSlot(idx, iface, dns64); err != nil {
			c.Log.Error("provision failed", "slot", slotName, "error", err)
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

	// Wait for SLAAC
	time.Sleep(slaacWaitDuration)

	// Start per-slot monitors (handles IP discovery + NDP proxy)
	for _, name := range createdNames {
		if c.Monitor != nil {
			c.Monitor.StartSlot(name)
		}
	}

	// Count unique IP pairs (exit points)
	pairSet := make(map[string]bool)
	for _, s := range c.SlotRepo.ListAll() {
		if s.Status == entity.SlotStatusHealthy {
			sorted := make([]string, 0, len(s.PublicIPv4s))
			for _, ip := range s.PublicIPv4s {
				if ip != "" {
					sorted = append(sorted, ip)
				}
			}
			if len(sorted) > 0 {
				sort.Strings(sorted)
				pairSet[strings.Join(sorted, ",")] = true
			}
		}
	}

	return &model.ProvisionResponse{
		Created:   created,
		Failed:    failed,
		Total:     existingCount + created,
		UniqueIPs: len(pairSet),
	}, nil
}


func (c *SlotUseCase) DestroySlot(slotName string) error {
	// Stop monitor goroutine first
	if c.Monitor != nil {
		c.Monitor.StopSlot(slotName)
	}

	slot, ok := c.SlotRepo.Get(slotName)
	if !ok {
		return entity.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		return entity.ErrSlotBusy
	}

	// Capture before deleting from repo
	ipv6 := slot.IPv6Address
	iface := slot.Interface
	c.SlotRepo.Delete(slotName)
	c.publishSlotRemoved(slotName)

	if ipv6 != "" && iface != "" {
		if err := c.Provisioner.RemoveNDPProxyEntry(ipv6, iface); err != nil {
			c.Log.Warn("ndp proxy removal failed", "slot", slotName, "error", err)
		}
	}

	if err := c.Provisioner.DestroySlot(slotName); err != nil {
		return fmt.Errorf("destroy %s: %w", slotName, err)
	}

	c.Log.Info("slot destroyed", "slot", slotName)
	return nil
}

// CleanupOrphans removes network namespaces that exist on disk but are not
// tracked in the in-memory SlotRepository.
func (uc *SlotUseCase) CleanupOrphans() (int, error) {
	tracked := uc.SlotRepo.ListAllNames()
	cleaned, err := uc.Provisioner.CleanupNamespaces(tracked)
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
		if err := c.Provisioner.DestroySlot(name); err != nil {
			c.Log.Warn("slot destroy failed", "device", deviceAlias, "slot", name, "error", err)
		}
	}
	removed := c.SlotRepo.DeleteByDevice(deviceAlias)
	c.Log.Info("slots teardown complete", "device", deviceAlias, "slots_removed", removed)
	return removed
}

func (c *SlotUseCase) ReattachByDevice(deviceAlias string, iface string) int {
	slotNames := c.SlotRepo.ListNamesForDevice(deviceAlias)
	reattached := 0
	for _, name := range slotNames {
		if err := c.Provisioner.ReattachSlot(name, iface); err != nil {
			c.Log.Warn("slot re-attach failed", "slot", name, "error", err)
			c.SlotRepo.SetStatus(name, entity.SlotStatusUnhealthy)
			continue
		}
		reattached++
	}

	// Resume suspended → healthy
	c.ResumeByDevice(deviceAlias)

	// Restart monitors for healthy slots
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

// naturalSlotLess compares slot names by their numeric suffix (e.g., slot2 < slot10).
func naturalSlotLess(a, b string) bool {
	aNum, aErr := strconv.Atoi(strings.TrimPrefix(a, "slot"))
	bNum, bErr := strconv.Atoi(strings.TrimPrefix(b, "slot"))
	if aErr == nil && bErr == nil {
		return aNum < bNum
	}
	return a < b
}

