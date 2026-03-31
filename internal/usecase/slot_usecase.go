package usecase

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
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
	Discovery   SlotDiscovery
	Provisioner SlotProvisioner
	DNS64Server string
	MaxSlots    int
	Strategy    string
	slots       map[string]*entity.Slot
	mu          sync.RWMutex
	rrIndex     uint64
}

func NewSlotUseCase(log *logrus.Logger, validate *validator.Validate, discovery SlotDiscovery, provisioner SlotProvisioner, dns64 string, maxSlots int, strategy string) *SlotUseCase {
	if strategy == "" {
		strategy = StrategyRandom
	}
	return &SlotUseCase{
		Log:         log,
		Validate:    validate,
		Discovery:   discovery,
		Provisioner: provisioner,
		DNS64Server: dns64,
		MaxSlots:    maxSlots,
		Strategy:    strategy,
		slots:       make(map[string]*entity.Slot),
	}
}

func (c *SlotUseCase) UpdateSlots(discovered []*model.DiscoveredSlot) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

		if existing, ok := c.slots[d.Name]; ok {
			s.ActiveConnections = atomic.LoadInt64(&existing.ActiveConnections)
			s.DeviceAlias = existing.DeviceAlias
			s.Interface = existing.Interface
		}
		c.slots[d.Name] = s
	}

	for name, slot := range c.slots {
		if !seen[name] {
			slot.Status = entity.SlotStatusUnhealthy
			slot.LastCheckedAt = now
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
	return len(discovered), nil
}

// RemoveSlotsForDevice removes all slots belonging to a device alias from the in-memory map.
func (c *SlotUseCase) RemoveSlotsForDevice(deviceAlias string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefix := deviceAlias + "_slot"
	removed := 0
	for name := range c.slots {
		if strings.HasPrefix(name, prefix) {
			delete(c.slots, name)
			removed++
		}
	}
	return removed
}

func (c *SlotUseCase) GetSlotNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.slots))
	for name := range c.slots {
		names = append(names, name)
	}
	return names
}

// SelectSlot picks a healthy slot based on the configured strategy.
// clientIP is used only for sticky-ip strategy (can be empty for others).
func (c *SlotUseCase) SelectSlot(clientIP string) (*entity.Slot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	healthy := make([]*entity.Slot, 0)
	for _, s := range c.slots {
		if s.Status == entity.SlotStatusHealthy {
			healthy = append(healthy, s)
		}
	}

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
	c.mu.RLock()
	defer c.mu.RUnlock()

	slot, ok := c.slots[name]
	if !ok {
		return nil, fmt.Errorf("slot %s not found", name)
	}

	if slot.Status != entity.SlotStatusHealthy {
		return nil, fmt.Errorf("slot %s is %s", name, slot.Status)
	}

	return slot, nil
}

func (c *SlotUseCase) ListAll() []model.SlotResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]model.SlotResponse, 0, len(c.slots))
	for _, s := range c.slots {
		result = append(result, *converter.SlotToResponse(s))
	}
	return result
}

func (c *SlotUseCase) GetByName(request *model.GetSlotRequest) (*model.SlotResponse, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	slot, ok := c.slots[request.SlotName]
	if !ok {
		return nil, fmt.Errorf("slot %s not found", request.SlotName)
	}

	return converter.SlotToResponse(slot), nil
}

func (c *SlotUseCase) GetStats() *model.StatsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := &model.StatsResponse{
		SlotStats: make([]model.SlotResponse, 0, len(c.slots)),
	}

	for _, s := range c.slots {
		stats.TotalSlots++
		if s.Status == entity.SlotStatusHealthy {
			stats.HealthySlots++
		} else {
			stats.UnhealthySlots++
		}
		stats.ActiveConnections += atomic.LoadInt64(&s.ActiveConnections)
		stats.SlotStats = append(stats.SlotStats, *converter.SlotToResponse(s))
	}

	return stats
}

func (c *SlotUseCase) GetHealth() *model.HealthResponse {
	stats := c.GetStats()
	status := "healthy"
	if stats.HealthySlots == 0 {
		status = "unhealthy"
	}
	return &model.HealthResponse{
		Status:       status,
		HealthySlots: stats.HealthySlots,
		TotalSlots:   stats.TotalSlots,
	}
}

func (c *SlotUseCase) IncrementConnections(slotName string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if slot, ok := c.slots[slotName]; ok {
		atomic.AddInt64(&slot.ActiveConnections, 1)
	}
}

func (c *SlotUseCase) DecrementConnections(slotName string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if slot, ok := c.slots[slotName]; ok {
		atomic.AddInt64(&slot.ActiveConnections, -1)
	}
}

func (c *SlotUseCase) AddTraffic(slotName string, bytesSent, bytesReceived int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if slot, ok := c.slots[slotName]; ok {
		atomic.AddInt64(&slot.BytesSent, bytesSent)
		atomic.AddInt64(&slot.BytesReceived, bytesReceived)
	}
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

	// Lock: check slot exists, no active connections, mark as discovering
	c.mu.Lock()
	slot, ok := c.slots[request.SlotName]
	if !ok {
		c.mu.Unlock()
		return nil, model.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		c.mu.Unlock()
		return nil, model.ErrSlotBusy
	}
	iface := slot.Interface
	oldIPv6 := slot.IPv6Address
	slot.Status = entity.SlotStatusDiscovering
	slot.PublicIPv4 = ""
	slot.IPv6Address = ""
	c.mu.Unlock()

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
		c.mu.Lock()
		if s, ok := c.slots[request.SlotName]; ok {
			s.Status = entity.SlotStatusUnhealthy
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("destroy slot %s: %w", request.SlotName, err)
	}

	dns64 := c.DNS64Server

	// Recreate namespace with same index
	if err := c.Provisioner.CreateSlot(deviceAlias, slotIndex, iface, dns64); err != nil {
		c.mu.Lock()
		if s, ok := c.slots[request.SlotName]; ok {
			s.Status = entity.SlotStatusUnhealthy
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("recreate slot %s: %w", request.SlotName, err)
	}

	// Wait for SLAAC address assignment
	time.Sleep(slaacWaitDuration)

	// Resolve new IPs
	ipv4, ipv4Err := c.Discovery.ResolveSlotIP(request.SlotName)

	// Lock: update slot with new IP data
	c.mu.Lock()
	slot = c.slots[request.SlotName]
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
	c.mu.Unlock()

	if c.Log != nil {
		c.Log.Infof("slot %s recycled: IPv4=%s status=%s", request.SlotName, response.PublicIPv4, response.Status)
	}

	return response, nil
}

func (c *SlotUseCase) ProvisionSlots(deviceAlias string, iface string, count int, dns64 string) (*model.ProvisionResponse, error) {
	if dns64 == "" {
		dns64 = c.DNS64Server
	}

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

	// Discover all slots for this device — update map first, then set device fields
	allNames, _ := c.Provisioner.ListSlotNamespacesForDevice(deviceAlias)
	discovered := c.Discovery.DiscoverAll(allNames)
	c.UpdateSlots(discovered)

	// Set DeviceAlias and Interface on discovered slots (after UpdateSlots created the entries)
	c.mu.Lock()
	for _, d := range discovered {
		if s, ok := c.slots[d.Name]; ok {
			s.DeviceAlias = deviceAlias
			s.Interface = iface
		}
	}
	c.mu.Unlock()
	// Warmup: detect and re-roll duplicate public IPv4 addresses
	dupFound, dupResolved := c.warmupDedup(deviceAlias, iface, dns64)

	// Count unique IPs
	c.mu.RLock()
	ipSet := make(map[string]bool)
	for _, s := range c.slots {
		if s.PublicIPv4 != "" && s.Status == entity.SlotStatusHealthy {
			ipSet[s.PublicIPv4] = true
		}
	}
	c.mu.RUnlock()

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
	c.mu.RLock()
	// Build IP → slot names mapping
	ipToSlots := make(map[string][]string)
	for _, s := range c.slots {
		if s.PublicIPv4 != "" && s.Status == entity.SlotStatusHealthy {
			ipToSlots[s.PublicIPv4] = append(ipToSlots[s.PublicIPv4], s.Name)
		}
	}
	c.mu.RUnlock()

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

			// Parse device alias and index
			da, slotIndex, _ := parseSlotName(slotName)

			// Remove old NDP proxy
			c.mu.RLock()
			s := c.slots[slotName]
			oldIPv6 := ""
			slotIface := iface
			if s != nil {
				oldIPv6 = s.IPv6Address
				if s.Interface != "" {
					slotIface = s.Interface
				}
			}
			c.mu.RUnlock()

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
			c.mu.RLock()
			stillDup := false
			for _, other := range c.slots {
				if other.Name != slotName && other.PublicIPv4 == newIPv4 && other.Status == entity.SlotStatusHealthy {
					stillDup = true
					break
				}
			}
			c.mu.RUnlock()

			// Update slot
			c.mu.Lock()
			if slot, ok := c.slots[slotName]; ok {
				slot.PublicIPv4 = newIPv4
				slot.IPv6Address = newIPv6
				slot.Status = entity.SlotStatusHealthy
				slot.LastCheckedAt = time.Now().UnixMilli()
			}
			c.mu.Unlock()

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

// MonitorIPs re-checks the actual public IPv4 of all healthy slots.
// Detects carrier-side IP changes and updates slot data.
func (c *SlotUseCase) MonitorIPs() {
	c.mu.RLock()
	var names []string
	for _, s := range c.slots {
		if s.Status == entity.SlotStatusHealthy {
			names = append(names, s.Name)
		}
	}
	c.mu.RUnlock()

	if len(names) == 0 {
		return
	}

	if c.Log != nil {
		c.Log.Infof("slot-monitor: checking public IPs for %d slots", len(names))
	}

	type ipResult struct {
		name    string
		newIPv4 string
	}

	results := make(chan ipResult, len(names))
	sem := make(chan struct{}, 10) // limit concurrent curl calls
	var wg sync.WaitGroup

	for _, name := range names {
		wg.Add(1)
		go func(slotName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ipv4, err := c.Discovery.ResolveSlotIP(slotName)
			if err != nil {
				if c.Log != nil {
					c.Log.Warnf("slot-monitor: %s IP check failed: %v", slotName, err)
				}
				return
			}
			results <- ipResult{name: slotName, newIPv4: ipv4}
		}(name)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	changed := 0
	for r := range results {
		c.mu.Lock()
		slot, ok := c.slots[r.name]
		if ok && slot.PublicIPv4 != r.newIPv4 {
			if c.Log != nil {
				c.Log.Infof("slot-monitor: %s IPv4 changed %s → %s", r.name, slot.PublicIPv4, r.newIPv4)
			}
			slot.PublicIPv4 = r.newIPv4
			slot.LastCheckedAt = time.Now().UnixMilli()
			changed++
		} else if ok {
			slot.LastCheckedAt = time.Now().UnixMilli()
		}
		c.mu.Unlock()
	}

	if c.Log != nil {
		c.Log.Infof("slot-monitor: done — %d/%d IPs changed", changed, len(names))
	}
}

func (c *SlotUseCase) DestroySlot(slotName string) error {
	c.mu.Lock()
	slot, ok := c.slots[slotName]
	if !ok {
		c.mu.Unlock()
		return model.ErrSlotNotFound
	}
	if atomic.LoadInt64(&slot.ActiveConnections) > 0 {
		c.mu.Unlock()
		return model.ErrSlotBusy
	}

	// Remove NDP proxy entry before destroying
	ipv6 := slot.IPv6Address
	iface := slot.Interface
	delete(c.slots, slotName)
	c.mu.Unlock()

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
	c.mu.Lock()
	names := make([]string, 0, len(c.slots))
	for name := range c.slots {
		names = append(names, name)
	}
	c.mu.Unlock()

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

// ProvisionOnDemand creates a slot on-the-fly when a proxy request targets
// a slot that doesn't exist yet. Returns the slot entity if successful.
func (c *SlotUseCase) ProvisionOnDemand(slotName string) (*entity.Slot, error) {
	deviceAlias, slotIndex, err := parseSlotName(slotName)
	if err != nil {
		return nil, err
	}

	// Check max slots limit
	c.mu.RLock()
	if c.MaxSlots > 0 && len(c.slots) >= c.MaxSlots {
		c.mu.RUnlock()
		return nil, fmt.Errorf("max slots reached (%d)", c.MaxSlots)
	}
	if slot, ok := c.slots[slotName]; ok {
		c.mu.RUnlock()
		if slot.Status == entity.SlotStatusHealthy {
			return slot, nil
		}
		return nil, fmt.Errorf("slot %s exists but is %s", slotName, slot.Status)
	}

	// Try to determine interface from existing slots of the same device
	var iface string
	for _, s := range c.slots {
		if s.DeviceAlias == deviceAlias && s.Interface != "" {
			iface = s.Interface
			break
		}
	}
	c.mu.RUnlock()

	if iface == "" {
		return nil, fmt.Errorf("cannot determine interface for device %s", deviceAlias)
	}

	if c.Log != nil {
		c.Log.Infof("on-demand: provisioning %s (index %d)", slotName, slotIndex)
	}

	// Create namespace
	if err := c.Provisioner.CreateSlot(deviceAlias, slotIndex, iface, c.DNS64Server); err != nil {
		return nil, fmt.Errorf("create slot %s: %w", slotName, err)
	}

	// Wait for SLAAC
	time.Sleep(slaacWaitDuration)

	// Resolve IPs
	ipv4, err := c.Discovery.ResolveSlotIP(slotName)
	if err != nil {
		if c.Log != nil {
			c.Log.Warnf("on-demand: %s IPv4 resolution failed: %v", slotName, err)
		}
	}

	ipv6, _ := c.Discovery.ResolveSlotIPv6(slotName)

	// NDP proxy for IPv6 reachability
	if ipv6 != "" && c.Provisioner != nil {
		if err := c.Provisioner.AddNDPProxyEntry(ipv6, iface); err != nil {
			if c.Log != nil {
				c.Log.Warnf("on-demand: %s NDP proxy for %s: %v", slotName, ipv6, err)
			}
		}
	}

	now := time.Now().UnixMilli()
	status := entity.SlotStatusHealthy
	if ipv4 == "" && ipv6 == "" {
		status = entity.SlotStatusUnhealthy
	}

	slot := &entity.Slot{
		Name:          slotName,
		DeviceAlias:   deviceAlias,
		Interface:     iface,
		IPv6Address:   ipv6,
		PublicIPv4:    ipv4,
		Status:        status,
		LastCheckedAt: now,
	}

	c.mu.Lock()
	// Double-check: another goroutine may have created it while we were provisioning
	if existing, ok := c.slots[slotName]; ok {
		c.mu.Unlock()
		return existing, nil
	}
	c.slots[slotName] = slot
	c.mu.Unlock()

	if c.Log != nil {
		c.Log.Infof("on-demand: %s ready (IPv4=%s, IPv6=%s, status=%s)", slotName, ipv4, ipv6, status)
	}

	if status != entity.SlotStatusHealthy {
		return nil, fmt.Errorf("slot %s provisioned but unhealthy", slotName)
	}

	return slot, nil
}

// DiscoverSlotsForDevice runs discovery only for slots belonging to a specific device
func (c *SlotUseCase) DiscoverSlotsForDevice(deviceAlias string, iface string) (int, error) {
	names, err := c.Provisioner.ListSlotNamespacesForDevice(deviceAlias)
	if err != nil {
		return 0, fmt.Errorf("list namespaces for %s: %w", deviceAlias, err)
	}

	discovered := c.Discovery.DiscoverAll(names)
	// Set DeviceAlias and Interface on discovered slots
	for _, d := range discovered {
		c.mu.Lock()
		if s, ok := c.slots[d.Name]; ok {
			s.DeviceAlias = deviceAlias
			s.Interface = iface
		}
		c.mu.Unlock()
	}
	c.UpdateSlots(discovered)
	return len(discovered), nil
}

// SelectRandomForDevice picks a random healthy slot belonging to a specific device
func (c *SlotUseCase) SelectRandomForDevice(deviceAlias string) (*entity.Slot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var healthy []*entity.Slot
	for _, s := range c.slots {
		if s.DeviceAlias == deviceAlias && s.Status == entity.SlotStatusHealthy {
			healthy = append(healthy, s)
		}
	}
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy slots for device %s", deviceAlias)
	}
	return healthy[rand.Intn(len(healthy))], nil
}

