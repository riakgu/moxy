package usecase

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotProvisioner interface {
	CreateSlot(deviceAlias string, slotIndex int, iface string, dns64 string) error
	DestroySlot(name string) error
	EnableNDPProxy(iface string) error
	AddNDPProxyEntry(ipv6 string, iface string) error
	RemoveNDPProxyEntry(ipv6 string, iface string) error
	ListSlotNamespaces() ([]string, error)
	ListSlotNamespacesForDevice(deviceAlias string) ([]string, error)
}

type SlotDiscovery interface {
	DiscoverAll(slotNames []string) []*model.DiscoveredSlot
	ResolveSlotIP(slotName string) (string, error)
	ResolveSlotIPv6(slotName string) (string, error)
}

const slaacWaitDuration = 5 * time.Second

const (
	StrategyRandom           = "random"
	StrategyRoundRobin       = "round-robin"
	StrategyLeastConnections = "least-connections"
	StrategyStickyIP         = "sticky-ip"
)

type SlotUseCase struct {
	Log         *logrus.Logger
	Validate    *validator.Validate
	SlotRepo    *repository.SlotRepository
	Discovery   SlotDiscovery
	Provisioner SlotProvisioner
	MaxSlots    int
	Strategy    string
	rrIndex     uint64
}

func NewSlotUseCase(
	log *logrus.Logger,
	validate *validator.Validate,
	slotRepo *repository.SlotRepository,
	discovery SlotDiscovery,
	provisioner SlotProvisioner,
	maxSlots int,
	strategy string,
) *SlotUseCase {
	if strategy == "" {
		strategy = StrategyRandom
	}
	return &SlotUseCase{
		Log:         log,
		Validate:    validate,
		SlotRepo:    slotRepo,
		Discovery:   discovery,
		Provisioner: provisioner,
		MaxSlots:    maxSlots,
		Strategy:    strategy,
	}
}

func (c *SlotUseCase) UpdateSlots(discovered []*model.DiscoveredSlot) {
	now := time.Now().UnixMilli()

	seen := make(map[string]bool)
	for _, d := range discovered {
		seen[d.Name] = true

		status := entity.SlotStatusUnhealthy
		if d.Healthy {
			status = entity.SlotStatusHealthy
		}

		s := &entity.Slot{
			Name:          d.Name,
			IPv6Address:   d.IPv6Address,
			PublicIPv4:    d.IPv4Address,
			Status:        status,
			LastCheckedAt: now,
		}

		if da, _, err := parseSlotName(d.Name); err == nil {
			s.DeviceAlias = da
		}

		if existing, ok := c.SlotRepo.Get(d.Name); ok {
			s.ActiveConnections = atomic.LoadInt64(&existing.ActiveConnections)
			if existing.Interface != "" {
				s.Interface = existing.Interface
			}
			if existing.Nameserver != "" {
				s.Nameserver = existing.Nameserver
			}
			if existing.NAT64Prefix != "" {
				s.NAT64Prefix = existing.NAT64Prefix
			}
			// Log IP changes
			if existing.PublicIPv4 != "" && existing.PublicIPv4 != d.IPv4Address && d.IPv4Address != "" {
				if c.Log != nil {
					c.Log.Infof("slot %s: IPv4 changed %s → %s", d.Name, existing.PublicIPv4, d.IPv4Address)
				}
			}
		}
		c.SlotRepo.Put(s)
	}

	// Mark unseen slots as unhealthy
	for _, slot := range c.SlotRepo.ListAll() {
		if !seen[slot.Name] {
			slot.Status = entity.SlotStatusUnhealthy
			slot.LastCheckedAt = now
		}
	}
}

// refreshNDPProxy adds NDP proxy entries for all discovered slots that have
// an IPv6 address and an interface. Uses per-slot interface from SlotRepo.
func (c *SlotUseCase) refreshNDPProxy(discovered []*model.DiscoveredSlot) {
	for _, d := range discovered {
		if d.IPv6Address == "" {
			continue
		}
		if s, ok := c.SlotRepo.Get(d.Name); ok && s.Interface != "" {
			if err := c.Provisioner.AddNDPProxyEntry(d.IPv6Address, s.Interface); err != nil {
				if c.Log != nil {
					c.Log.Warnf("NDP proxy entry for %s failed: %v", d.Name, err)
				}
			}
		}
	}
}

func (c *SlotUseCase) DiscoverSlots() (int, error) {
	names, err := c.Provisioner.ListSlotNamespaces()
	if err != nil {
		return 0, fmt.Errorf("list namespaces: %w", err)
	}

	discovered := c.Discovery.DiscoverAll(names)
	c.UpdateSlots(discovered)
	c.refreshNDPProxy(discovered)
	return len(discovered), nil
}

func (c *SlotUseCase) RemoveSlotsForDevice(deviceAlias string) int {
	return c.SlotRepo.DeleteByDevice(deviceAlias)
}

func (c *SlotUseCase) GetSlotNames() []string {
	return c.SlotRepo.ListNames()
}

func (c *SlotUseCase) SelectSlot(clientIP string) (*entity.Slot, error) {
	healthy := c.SlotRepo.ListHealthy()
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy slots available")
	}

	switch c.Strategy {
	case StrategyRoundRobin:
		idx := atomic.AddUint64(&c.rrIndex, 1)
		return healthy[idx%uint64(len(healthy))], nil

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
		return best, nil

	case StrategyStickyIP:
		if clientIP == "" {
			return healthy[rand.Intn(len(healthy))], nil
		}
		hash := fnvHash(clientIP)
		return healthy[hash%uint64(len(healthy))], nil

	default: // random
		return healthy[rand.Intn(len(healthy))], nil
	}
}

func fnvHash(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func (c *SlotUseCase) SelectByName(name string) (*entity.Slot, error) {
	slot, ok := c.SlotRepo.Get(name)
	if !ok {
		return nil, fmt.Errorf("slot %s not found", name)
	}
	if slot.Status != entity.SlotStatusHealthy {
		return nil, fmt.Errorf("slot %s is %s", name, slot.Status)
	}
	return slot, nil
}

func (c *SlotUseCase) ListAll() []model.SlotResponse {
	slots := c.SlotRepo.ListAll()
	result := make([]model.SlotResponse, 0, len(slots))
	for _, s := range slots {
		result = append(result, *converter.SlotToResponse(s))
	}
	return result
}

func (c *SlotUseCase) GetByName(request *model.GetSlotRequest) (*model.SlotResponse, error) {
	slot, ok := c.SlotRepo.Get(request.SlotName)
	if !ok {
		return nil, fmt.Errorf("slot %s not found", request.SlotName)
	}
	return converter.SlotToResponse(slot), nil
}

func (c *SlotUseCase) IncrementConnections(slotName string) {
	c.SlotRepo.IncrementConnections(slotName)
}

func (c *SlotUseCase) DecrementConnections(slotName string) {
	c.SlotRepo.DecrementConnections(slotName)
}

// GetSlotConfig returns the ISP config for a slot.
// Returns empty strings if the slot is not found.
func (c *SlotUseCase) GetSlotConfig(name string) (nameserver, nat64Prefix string) {
	if slot, ok := c.SlotRepo.Get(name); ok {
		return slot.Nameserver, slot.NAT64Prefix
	}
	return "", ""
}

// parseSlotName extracts device alias and slot index from names like "dev1_slot3"
func parseSlotName(slotName string) (deviceAlias string, slotIndex int, err error) {
	idx := strings.LastIndex(slotName, "_slot")
	if idx < 0 {
		return "", 0, fmt.Errorf("invalid slot name %s: missing _slot", slotName)
	}
	deviceAlias = slotName[:idx]
	indexStr := slotName[idx+5:] // skip "_slot"
	slotIndex, err = strconv.Atoi(indexStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid slot name %s: cannot parse index", slotName)
	}
	return deviceAlias, slotIndex, nil
}

func (c *SlotUseCase) RecycleSlot(request *model.ChangeIPRequest) (*model.SlotResponse, error) {
	if c.Validate != nil {
		if err := c.Validate.Struct(request); err != nil {
			return nil, err
		}
	}

	deviceAlias, slotIndex, err := parseSlotName(request.SlotName)
	if err != nil {
		return nil, err
	}

	// Check slot exists and has no active connections
	slot, ok := c.SlotRepo.Get(request.SlotName)
	if !ok {
		return nil, model.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		return nil, model.ErrSlotBusy
	}

	iface := slot.Interface
	oldIPv6 := slot.IPv6Address

	// Mark as discovering
	slot.Status = entity.SlotStatusDiscovering
	slot.PublicIPv4 = ""
	slot.IPv6Address = ""

	if c.Log != nil {
		c.Log.Infof("recycling slot %s (index %d)", request.SlotName, slotIndex)
	}

	// Remove old NDP proxy entry before destroying namespace
	if oldIPv6 != "" && c.Provisioner != nil {
		if err := c.Provisioner.RemoveNDPProxyEntry(oldIPv6, iface); err != nil {
			if c.Log != nil {
				c.Log.Warnf("slot %s: remove NDP proxy for %s: %v", request.SlotName, oldIPv6, err)
			}
		}
	}

	// Destroy old namespace
	if err := c.Provisioner.DestroySlot(request.SlotName); err != nil {
		slot.Status = entity.SlotStatusUnhealthy
		return nil, fmt.Errorf("destroy slot %s: %w", request.SlotName, err)
	}

	// Use the slot's own nameserver for resolv.conf
	dns64 := slot.Nameserver

	// Recreate namespace with same index
	if err := c.Provisioner.CreateSlot(deviceAlias, slotIndex, iface, dns64); err != nil {
		slot.Status = entity.SlotStatusUnhealthy
		return nil, fmt.Errorf("recreate slot %s: %w", request.SlotName, err)
	}

	// Wait for SLAAC address assignment
	time.Sleep(slaacWaitDuration)

	// Resolve new IPs
	ipv4, ipv4Err := c.Discovery.ResolveSlotIP(request.SlotName)

	if ipv4Err != nil {
		if c.Log != nil {
			c.Log.Warnf("slot %s: IPv4 resolution failed after recycle: %v", request.SlotName, ipv4Err)
		}
		slot.Status = entity.SlotStatusUnhealthy
	} else {
		ipv6, _ := c.Discovery.ResolveSlotIPv6(request.SlotName)
		slot.PublicIPv4 = ipv4
		slot.IPv6Address = ipv6
		slot.Status = entity.SlotStatusHealthy

		// Add NDP proxy entry for new IPv6
		if ipv6 != "" && c.Provisioner != nil {
			if err := c.Provisioner.AddNDPProxyEntry(ipv6, iface); err != nil {
				if c.Log != nil {
					c.Log.Warnf("slot %s: add NDP proxy for %s: %v", request.SlotName, ipv6, err)
				}
			}
		}
	}
	slot.LastCheckedAt = time.Now().UnixMilli()
	response := converter.SlotToResponse(slot)

	if c.Log != nil {
		c.Log.Infof("slot %s recycled: IPv4=%s status=%s", request.SlotName, response.PublicIPv4, response.Status)
	}

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
	existing, _ := c.Provisioner.ListSlotNamespacesForDevice(deviceAlias)
	existingCount := len(existing)

	if existingCount >= count {
		if c.Log != nil {
			c.Log.Infof("already have %d slots for %s (requested %d), skipping creation", existingCount, deviceAlias, count)
		}
	}

	// Check max slots limit
	target := count
	if c.MaxSlots > 0 && target > c.MaxSlots {
		if c.Log != nil {
			c.Log.Warnf("capping target to %d slots (max %d)", c.MaxSlots, c.MaxSlots)
		}
		target = c.MaxSlots
	}

	// Build set of existing slot indices for gap detection
	prefix := deviceAlias + "_slot"
	existingSet := make(map[int]bool)
	for _, name := range existing {
		indexStr := strings.TrimPrefix(name, prefix)
		if idx, err := strconv.Atoi(indexStr); err == nil {
			existingSet[idx] = true
		}
	}

	created := 0
	failed := 0
	toCreate := target - existingCount
	if toCreate < 0 {
		toCreate = 0
	}

	// Fill gaps first, then append — iterate from 0 upward
	for idx := 0; created+failed < toCreate; idx++ {
		if existingSet[idx] {
			continue // slot already exists, skip
		}
		slotName := fmt.Sprintf("%s_slot%d", deviceAlias, idx)
		if c.Log != nil {
			c.Log.Infof("provisioning %s (%d/%d)", slotName, created+failed+1, toCreate)
		}
		if err := c.Provisioner.CreateSlot(deviceAlias, idx, iface, dns64); err != nil {
			if c.Log != nil {
				c.Log.WithError(err).Errorf("failed to provision %s", slotName)
			}
			failed++
			continue
		}
		created++
	}

	// Wait for SLAAC
	time.Sleep(slaacWaitDuration)

	// Discover all slots for this device
	allNames, _ := c.Provisioner.ListSlotNamespacesForDevice(deviceAlias)
	discovered := c.Discovery.DiscoverAll(allNames)
	c.UpdateSlots(discovered)

	// Set DeviceAlias, Interface, and ISP config on discovered slots
	for _, d := range discovered {
		if s, ok := c.SlotRepo.Get(d.Name); ok {
			s.DeviceAlias = deviceAlias
			s.Interface = iface
			s.Nameserver = nameserver
			s.NAT64Prefix = nat64Prefix
		}
	}

	// Refresh NDP proxy entries with per-slot interface
	c.refreshNDPProxy(discovered)

	// Warmup: detect and re-roll duplicate public IPv4 addresses
	dupFound, dupResolved := c.warmupDedup(deviceAlias, iface, dns64)

	// Count unique IPs
	ipSet := make(map[string]bool)
	for _, s := range c.SlotRepo.ListAll() {
		if s.PublicIPv4 != "" && s.Status == entity.SlotStatusHealthy {
			ipSet[s.PublicIPv4] = true
		}
	}

	return &model.ProvisionResponse{
		Created:            created,
		Failed:             failed,
		Total:              len(allNames),
		DuplicatesFound:    dupFound,
		DuplicatesResolved: dupResolved,
		UniqueIPs:          len(ipSet),
	}, nil
}

const warmupMaxRetries = 3

// warmupDedup finds slots sharing the same public IPv4 and re-rolls them.
func (c *SlotUseCase) warmupDedup(deviceAlias, iface, dns64 string) (found, resolved int) {
	// Build IP → slot names mapping
	ipToSlots := make(map[string][]string)
	for _, s := range c.SlotRepo.ListAll() {
		if s.PublicIPv4 != "" && s.Status == entity.SlotStatusHealthy {
			ipToSlots[s.PublicIPv4] = append(ipToSlots[s.PublicIPv4], s.Name)
		}
	}

	// Find duplicates (keep first, re-roll rest)
	var toReroll []string
	for ip, names := range ipToSlots {
		if len(names) > 1 {
			if c.Log != nil {
				c.Log.Warnf("warmup: duplicate IPv4 %s shared by %v", ip, names)
			}
			toReroll = append(toReroll, names[1:]...)
		}
	}

	if len(toReroll) == 0 {
		return 0, 0
	}

	found = len(toReroll)
	if c.Log != nil {
		c.Log.Infof("warmup: found %d slots with duplicate IPs, re-rolling...", found)
	}

	for _, slotName := range toReroll {
		rerolled := false
		for retry := 0; retry < warmupMaxRetries; retry++ {
			if c.Log != nil {
				c.Log.Infof("warmup: re-rolling %s (attempt %d/%d)", slotName, retry+1, warmupMaxRetries)
			}

			da, slotIndex, _ := parseSlotName(slotName)

			// Get current slot state
			s, ok := c.SlotRepo.Get(slotName)
			oldIPv6 := ""
			slotIface := iface
			if ok {
				oldIPv6 = s.IPv6Address
				if s.Interface != "" {
					slotIface = s.Interface
				}
			}

			if oldIPv6 != "" {
				c.Provisioner.RemoveNDPProxyEntry(oldIPv6, slotIface)
			}

			// Destroy + recreate
			c.Provisioner.DestroySlot(slotName)
			if err := c.Provisioner.CreateSlot(da, slotIndex, slotIface, dns64); err != nil {
				if c.Log != nil {
					c.Log.WithError(err).Warnf("warmup: failed to recreate %s", slotName)
				}
				continue
			}

			time.Sleep(slaacWaitDuration)

			// Resolve new IP
			newIPv4, err := c.Discovery.ResolveSlotIP(slotName)
			if err != nil {
				if c.Log != nil {
					c.Log.Warnf("warmup: %s IPv4 resolution failed after re-roll", slotName)
				}
				continue
			}

			newIPv6, _ := c.Discovery.ResolveSlotIPv6(slotName)
			if newIPv6 != "" {
				c.Provisioner.AddNDPProxyEntry(newIPv6, iface)
			}

			// Check if new IP is still duplicate
			stillDup := false
			for _, other := range c.SlotRepo.ListAll() {
				if other.Name != slotName && other.PublicIPv4 == newIPv4 && other.Status == entity.SlotStatusHealthy {
					stillDup = true
					break
				}
			}

			// Update slot in repository
			if slot, ok := c.SlotRepo.Get(slotName); ok {
				slot.PublicIPv4 = newIPv4
				slot.IPv6Address = newIPv6
				slot.Status = entity.SlotStatusHealthy
				slot.LastCheckedAt = time.Now().UnixMilli()
			}

			if !stillDup {
				rerolled = true
				if c.Log != nil {
					c.Log.Infof("warmup: %s re-rolled → new IPv4 %s (unique)", slotName, newIPv4)
				}
				break
			}

			if c.Log != nil {
				c.Log.Warnf("warmup: %s re-rolled → %s still duplicate, retrying...", slotName, newIPv4)
			}
		}

		if rerolled {
			resolved++
		} else if c.Log != nil {
			c.Log.Warnf("warmup: %s still has duplicate IP after %d retries", slotName, warmupMaxRetries)
		}
	}

	if c.Log != nil {
		c.Log.Infof("warmup: dedup complete — %d found, %d resolved", found, resolved)
	}
	return found, resolved
}



func (c *SlotUseCase) DestroySlot(slotName string) error {
	slot, ok := c.SlotRepo.Get(slotName)
	if !ok {
		return model.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		return model.ErrSlotBusy
	}

	// Capture before deleting from repo
	ipv6 := slot.IPv6Address
	iface := slot.Interface
	c.SlotRepo.Delete(slotName)

	if ipv6 != "" && iface != "" {
		if err := c.Provisioner.RemoveNDPProxyEntry(ipv6, iface); err != nil {
			if c.Log != nil {
				c.Log.Warnf("remove NDP proxy for %s: %v", slotName, err)
			}
		}
	}

	if err := c.Provisioner.DestroySlot(slotName); err != nil {
		return fmt.Errorf("destroy %s: %w", slotName, err)
	}

	if c.Log != nil {
		c.Log.Infof("slot %s destroyed", slotName)
	}
	return nil
}

func (c *SlotUseCase) TeardownAll() (*model.ProvisionResponse, error) {
	names := c.SlotRepo.ListNames()

	destroyed := 0
	failed := 0
	for _, name := range names {
		if err := c.DestroySlot(name); err != nil {
			if c.Log != nil {
				c.Log.WithError(err).Warnf("teardown: failed to destroy %s", name)
			}
			failed++
			continue
		}
		destroyed++
	}

	return &model.ProvisionResponse{
		Created: 0,
		Failed:  failed,
		Total:   destroyed,
	}, nil
}

// DiscoverSlotsForDevice runs discovery only for slots belonging to a specific device
func (c *SlotUseCase) DiscoverSlotsForDevice(deviceAlias string, iface string, nameserver string, nat64Prefix string) (int, error) {
	names, err := c.Provisioner.ListSlotNamespacesForDevice(deviceAlias)
	if err != nil {
		return 0, fmt.Errorf("list namespaces for %s: %w", deviceAlias, err)
	}

	discovered := c.Discovery.DiscoverAll(names)
	// Set DeviceAlias, Interface, and ISP config on discovered slots
	for _, d := range discovered {
		if s, ok := c.SlotRepo.Get(d.Name); ok {
			s.DeviceAlias = deviceAlias
			s.Interface = iface
			s.Nameserver = nameserver
			s.NAT64Prefix = nat64Prefix
		}
	}
	c.UpdateSlots(discovered)
	c.refreshNDPProxy(discovered)
	return len(discovered), nil
}
