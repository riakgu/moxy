//go:build linux

package usecase

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/gateway/adb"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type ISPProber interface {
	Probe(hintDNS []string) (*model.ISPProbeResult, error)
}

type SlotProvisionService interface {
	ProvisionSlots(deviceAlias, iface string, count int, nameserver, nat64Prefix string) (*model.ProvisionResponse, error)
}

// DeviceWatcher monitors USB device connections in real-time.
type DeviceWatcher interface {
	Watch(ctx context.Context) <-chan model.DeviceEvent
}

type DeviceUseCase struct {
	Log           *logrus.Logger
	DeviceRepo    *repository.DeviceRepository
	ADB           *adb.ADBGateway
	Provisioner   SlotProvisioner
	SlotRepo      *repository.SlotRepository
	SlotProvision SlotProvisionService
	ISPProber     ISPProber
	Watcher       DeviceWatcher
	GracePeriod   time.Duration
	Monitor       *SlotMonitorUseCase
	graceTimers   map[string]*time.Timer
	mu            sync.Mutex
}

func NewDeviceUseCase(
	log *logrus.Logger,
	deviceRepo *repository.DeviceRepository,
	adbGW *adb.ADBGateway,
	provisioner SlotProvisioner,
	slotRepo *repository.SlotRepository,
	slotProvision SlotProvisionService,
	ispProber ISPProber,
	watcher DeviceWatcher,
	gracePeriod time.Duration,
) *DeviceUseCase {
	return &DeviceUseCase{
		Log:           log,
		DeviceRepo:    deviceRepo,
		ADB:           adbGW,
		Provisioner:   provisioner,
		SlotRepo:      slotRepo,
		SlotProvision: slotProvision,
		ISPProber:     ispProber,
		Watcher:       watcher,
		GracePeriod:   gracePeriod,
		graceTimers:   make(map[string]*time.Timer),
	}
}

// Scan discovers ADB devices and registers new ones as "detected".
// No setup or provisioning — use Setup(alias) for that.
func (c *DeviceUseCase) Scan() (*model.ScanResponse, error) {
	serials, err := c.ADB.ListDevices()
	if err != nil {
		return nil, fmt.Errorf("ADB scan failed: %w", err)
	}

	resp := &model.ScanResponse{}
	serialSet := make(map[string]bool, len(serials))

	// Register new phones as "detected" — no setup, no provisioning
	for _, serial := range serials {
		serialSet[serial] = true

		if _, exists := c.DeviceRepo.GetBySerial(serial); exists {
			continue // already known
		}

		resp.Discovered++
		device := &entity.Device{
			Alias:  c.DeviceRepo.NextAlias(),
			Serial: serial,
			Status: entity.DeviceStatusDetected,
		}
		c.DeviceRepo.Put(device)
	}

	// Teardown disconnected phones
	for _, device := range c.DeviceRepo.ListAll() {
		if device.Status != entity.DeviceStatusOffline && !serialSet[device.Serial] {
			c.Log.Warnf("scan: device %s (%s) disconnected — tearing down", device.Alias, device.Serial)
			c.teardownDevice(device)
		}
	}

	// Build response
	for _, device := range c.DeviceRepo.ListAll() {
		slotCount := c.SlotRepo.CountByDevice(device.Alias)
		resp.Devices = append(resp.Devices, *converter.DeviceToResponse(device, slotCount))
	}
	if resp.Devices == nil {
		resp.Devices = []model.DeviceResponse{}
	}

	return resp, nil
}

// Setup runs the full setup pipeline for a single detected device.
// Configures tethering, network interface, DNS64, and auto-provisions 1 slot.
func (c *DeviceUseCase) Setup(alias string) (*model.SetupResponse, error) {
	device, ok := c.DeviceRepo.GetByAlias(alias)
	if !ok {
		return nil, fmt.Errorf("device %s not found", alias)
	}
	if device.Status != entity.DeviceStatusDetected {
		return nil, model.ErrDeviceNotDetected
	}

	device.Status = entity.DeviceStatusSetup
	c.DeviceRepo.Put(device)

	if err := c.setup(device); err != nil {
		c.Log.WithError(err).Warnf("setup: failed for %s (%s)", device.Alias, device.Serial)
		device.Status = entity.DeviceStatusError
		c.DeviceRepo.Put(device)
		return nil, err
	}

	// Auto-provision 1 slot
	var provResp *model.ProvisionResponse
	prov, provErr := c.SlotProvision.ProvisionSlots(
		device.Alias, device.Interface, 1,
		device.Nameserver, device.NAT64Prefix,
	)
	if provErr != nil {
		c.Log.WithError(provErr).Warnf("setup: provision failed for %s", device.Alias)
	} else {
		provResp = prov
	}

	slotCount := c.SlotRepo.CountByDevice(device.Alias)
	return &model.SetupResponse{
		Device:    *converter.DeviceToResponse(device, slotCount),
		Provision: provResp,
	}, nil
}

// ListADBDevices returns raw ADB serial numbers.
func (c *DeviceUseCase) ListADBDevices() ([]string, error) {
	return c.ADB.ListDevices()
}

// List returns all registered devices with slot counts.
func (c *DeviceUseCase) List() ([]model.DeviceResponse, error) {
	devices := c.DeviceRepo.ListAll()
	result := make([]model.DeviceResponse, 0, len(devices))
	for _, d := range devices {
		slotCount := c.SlotRepo.CountByDevice(d.Alias)
		result = append(result, *converter.DeviceToResponse(d, slotCount))
	}
	return result, nil
}

// GetByAlias returns a single device by alias.
func (c *DeviceUseCase) GetByAlias(alias string) (*model.DeviceResponse, error) {
	device, ok := c.DeviceRepo.GetByAlias(alias)
	if !ok {
		return nil, fmt.Errorf("device %s not found", alias)
	}
	slotCount := c.SlotRepo.CountByDevice(device.Alias)
	return converter.DeviceToResponse(device, slotCount), nil
}

// Delete tears down a device and removes it from memory.
func (c *DeviceUseCase) Delete(alias string) error {
	device, ok := c.DeviceRepo.GetByAlias(alias)
	if !ok {
		return fmt.Errorf("device %s not found", alias)
	}
	c.teardownDevice(device)
	c.DeviceRepo.Delete(device.Serial)
	return nil
}

// Provision adds more slots to a device.
func (c *DeviceUseCase) Provision(req *model.ProvisionDeviceRequest) (*model.ProvisionResponse, error) {
	device, ok := c.DeviceRepo.GetByAlias(req.Alias)
	if !ok {
		return nil, fmt.Errorf("device %s not found", req.Alias)
	}

	slots := req.Slots
	if slots <= 0 {
		slots = 5
	}

	return c.SlotProvision.ProvisionSlots(
		device.Alias, device.Interface, slots,
		device.Nameserver, device.NAT64Prefix,
	)
}

// StartWatching consumes events from the DeviceWatcher and handles
// connect/disconnect with grace period and smart reconnect.
// This replaces the old CheckHealth() polling approach.
func (c *DeviceUseCase) StartWatching(ctx context.Context) {
	events := c.Watcher.Watch(ctx)
	c.Log.Info("device watcher: started")
	for {
		select {
		case <-ctx.Done():
			c.cancelAllGraceTimers()
			c.Log.Info("device watcher: stopped")
			return
		case event, ok := <-events:
			if !ok {
				c.Log.Warn("device watcher: event channel closed")
				return
			}
			c.mu.Lock()
			switch event.Status {
			case "connected":
				c.handleConnect(event.Serial)
			case "disconnected":
				c.handleDisconnect(event.Serial)
			}
			c.mu.Unlock()
		}
	}
}

// handleConnect handles a device appearing on USB.
func (c *DeviceUseCase) handleConnect(serial string) {
	// Case 1: Reconnect within grace period
	if timer, ok := c.graceTimers[serial]; ok {
		timer.Stop()
		delete(c.graceTimers, serial)
		c.Log.Infof("device watcher: %s reconnected within grace period", serial)
		c.smartReconnect(serial)
		return
	}

	// Case 2: Already known device
	if device, exists := c.DeviceRepo.GetBySerial(serial); exists {
		// If device was offline/error (e.g. grace expired), reset to detected
		if device.Status == entity.DeviceStatusOffline || device.Status == entity.DeviceStatusError {
			device.Status = entity.DeviceStatusDetected
			c.DeviceRepo.Put(device)
			c.Log.Infof("device watcher: %s (%s) re-appeared — reset to detected",
				device.Alias, serial)
		}
		return
	}

	// Case 3: Brand new device — register as detected
	device := &entity.Device{
		Alias:  c.DeviceRepo.NextAlias(),
		Serial: serial,
		Status: entity.DeviceStatusDetected,
	}
	c.DeviceRepo.Put(device)
	c.Log.Infof("device watcher: new device %s (%s) detected", device.Alias, serial)
}

// handleDisconnect handles a device being removed from USB.
func (c *DeviceUseCase) handleDisconnect(serial string) {
	device, ok := c.DeviceRepo.GetBySerial(serial)
	if !ok || device.Status != entity.DeviceStatusOnline {
		return
	}

	// Immediately suspend all slots (exclude from proxy routing)
	c.suspendDeviceSlots(device.Alias)
	device.Status = entity.DeviceStatusDisconnected
	c.DeviceRepo.Put(device)

	// Start grace timer
	alias := device.Alias
	timer := time.AfterFunc(c.GracePeriod, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.graceTimers, serial)
		c.Log.Warnf("device watcher: grace expired for %s (%s) — tearing down",
			alias, serial)
		c.teardownDevice(device)
	})
	c.graceTimers[serial] = timer
	c.Log.Infof("device watcher: %s (%s) disconnected — %s grace started",
		device.Alias, serial, c.GracePeriod)
}

// smartReconnect attempts lightweight recovery after a transient USB disconnect.
func (c *DeviceUseCase) smartReconnect(serial string) {
	device, ok := c.DeviceRepo.GetBySerial(serial)
	if !ok {
		return
	}

	// 1. Verify interface came back
	iface, err := c.ADB.DetectInterfaceForSerial(serial)
	if err != nil {
		c.Log.Warnf("device watcher: %s reconnected but interface not found: %v — full teardown",
			device.Alias, err)
		c.teardownDevice(device)
		return
	}
	device.Interface = iface

	// 2. Re-attach IPVLAN in existing namespaces
	slotNames := c.SlotRepo.ListNamesForDevice(device.Alias)
	reattached := 0
	for _, name := range slotNames {
		if err := c.Provisioner.ReattachSlot(name, iface); err != nil {
			c.Log.Warnf("device watcher: re-attach %s failed: %v", name, err)
			if slot, ok := c.SlotRepo.Get(name); ok {
				slot.Status = entity.SlotStatusUnhealthy
			}
			continue
		}
		reattached++
	}

	// 3. Resume slots that were successfully re-attached
	c.resumeDeviceSlots(device.Alias)

	// 4. Restart monitors for re-attached slots
	if c.Monitor != nil {
		for _, name := range slotNames {
			if slot, ok := c.SlotRepo.Get(name); ok && slot.Status == entity.SlotStatusHealthy {
				c.Monitor.StopSlot(name)
				c.Monitor.StartSlot(name)
			}
		}
	}

	device.Status = entity.DeviceStatusOnline
	c.DeviceRepo.Put(device)
	c.Log.Infof("device watcher: %s smart reconnect complete (%d/%d slots re-attached)",
		device.Alias, reattached, len(slotNames))
}

// suspendDeviceSlots marks all slots for a device as suspended.
func (c *DeviceUseCase) suspendDeviceSlots(deviceAlias string) {
	slotNames := c.SlotRepo.ListNamesForDevice(deviceAlias)
	for _, name := range slotNames {
		if slot, ok := c.SlotRepo.Get(name); ok {
			slot.Status = entity.SlotStatusSuspended
		}
		if c.Monitor != nil {
			c.Monitor.StopSlot(name)
		}
	}
}

// resumeDeviceSlots marks suspended slots for a device as healthy.
func (c *DeviceUseCase) resumeDeviceSlots(deviceAlias string) {
	slotNames := c.SlotRepo.ListNamesForDevice(deviceAlias)
	for _, name := range slotNames {
		if slot, ok := c.SlotRepo.Get(name); ok && slot.Status == entity.SlotStatusSuspended {
			slot.Status = entity.SlotStatusHealthy
		}
	}
}

// cancelAllGraceTimers stops all pending grace timers (used during shutdown).
func (c *DeviceUseCase) cancelAllGraceTimers() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for serial, timer := range c.graceTimers {
		timer.Stop()
		delete(c.graceTimers, serial)
	}
}

// setup runs the device setup steps (private — called by Scan).
func (c *DeviceUseCase) setup(device *entity.Device) error {
	type setupStep struct {
		Name string
		Fn   func() error
	}

	steps := []setupStep{
		{"screen_unlocked", func() error {
			ok, err := c.ADB.IsScreenUnlocked(device.Serial)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("phone screen is locked — please unlock it")
			}
			return nil
		}},
		{"enabled_tethering", func() error { return c.ADB.EnableTethering(device.Serial) }},
		{"interface_detected", func() error {
			iface, err := c.ADB.DetectInterfaceForSerial(device.Serial)
			if err != nil {
				return err
			}
			device.Interface = iface
			return nil
		}},
		{"enabled_data", func() error { return c.ADB.EnableData(device.Serial) }},
		{"dismissed_dialog", func() error { return c.ADB.DismissDataDialog(device.Serial) }},
		{"disabled_wifi", func() error { return c.ADB.DisableWifi(device.Serial) }},
		{"dhcp_configured", func() error {
			return c.Provisioner.ConfigureDHCP(device.Interface)
		}},
		{"ipv6_configured", func() error {
			if err := c.Provisioner.ConfigureIPv6SLAAC(device.Interface); err != nil {
				return err
			}
			time.Sleep(5 * time.Second) // Wait for SLAAC
			return nil
		}},
		{"ipv6_verified", func() error {
			iface, err := net.InterfaceByName(device.Interface)
			if err != nil {
				return fmt.Errorf("interface %s not found: %w", device.Interface, err)
			}
			addrs, err := iface.Addrs()
			if err != nil {
				return fmt.Errorf("list addrs on %s: %w", device.Interface, err)
			}
			for _, addr := range addrs {
				ipNet, ok := addr.(*net.IPNet)
				if !ok {
					continue
				}
				ip := ipNet.IP
				if ip.To4() == nil && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() {
					c.Log.Infof("device %s: global IPv6 found: %s", device.Alias, ip)
					return nil
				}
			}
			return fmt.Errorf("no global IPv6 address on %s", device.Interface)
		}},
		{"isp_probed", func() error {
			if c.ISPProber == nil {
				c.Log.Warnf("device %s: ISP prober not available", device.Alias)
				return nil
			}

			// Read carrier-assigned DNS from the phone (most reliable source)
			adbDNS, err := c.ADB.GetDNSServers(device.Serial)
			if err != nil {
				c.Log.Warnf("device %s: ADB DNS discovery failed: %v", device.Alias, err)
			} else if len(adbDNS) > 0 {
				c.Log.Infof("device %s: ADB DNS servers: %v", device.Alias, adbDNS)
			}

			result, err := c.ISPProber.Probe(adbDNS)
			if err != nil {
				return fmt.Errorf("ISP probe: %w", err)
			}
			device.Nameserver = result.Nameserver
			device.NAT64Prefix = result.NAT64Prefix
			c.Log.Infof("device %s: ISP probe ns=%s prefix=%s",
				device.Alias, result.Nameserver, result.NAT64Prefix)
			return nil
		}},
		{"carrier_detected", func() error {
			carrier, err := c.ADB.GetCarrier(device.Serial)
			if err != nil {
				return err
			}
			device.Carrier = carrier
			return nil
		}},
	}

	for _, step := range steps {
		c.Log.Infof("device %s: running step %s", device.Alias, step.Name)
		if err := step.Fn(); err != nil {
			return fmt.Errorf("step %s: %w", step.Name, err)
		}
	}

	// Enable NDP proxy on the interface
	c.Provisioner.EnableNDPProxy(device.Interface)

	device.Status = entity.DeviceStatusOnline
	c.DeviceRepo.Put(device)
	return nil
}

// teardownDevice destroys namespaces and removes slots for a device.
func (c *DeviceUseCase) teardownDevice(device *entity.Device) {
	// Get slot names from in-memory repo (not filesystem)
	slotNames := c.SlotRepo.ListNamesForDevice(device.Alias)
	for _, name := range slotNames {
		if err := c.Provisioner.DestroySlot(name); err != nil {
			c.Log.WithError(err).Warnf("teardown %s: failed to destroy %s", device.Alias, name)
		}
	}
	removed := c.SlotRepo.DeleteByDevice(device.Alias)
	c.Log.Infof("device %s: teardown complete — removed %d slots", device.Alias, removed)
	device.Status = entity.DeviceStatusOffline
	c.DeviceRepo.Put(device)
}
