//go:build linux

package usecase

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
	"log/slog"

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
	SuspendByDevice(deviceAlias string)
	ResumeByDevice(deviceAlias string)
	TeardownByDevice(deviceAlias string, drainTimeout time.Duration) int
	ReattachByDevice(deviceAlias string, iface string) int
}

// DeviceWatcher monitors USB device connections in real-time.
type DeviceWatcher interface {
	Watch(ctx context.Context) <-chan model.DeviceEvent
}

type DeviceUseCase struct {
	Log           *slog.Logger
	DeviceRepo    *repository.DeviceRepository
	ADB           *adb.ADBGateway
	Provisioner   SlotProvisioner
	SlotRepo      *repository.SlotRepository
	SlotProvision SlotProvisionService
	ISPProber     ISPProber
	Watcher       DeviceWatcher
	GracePeriod   time.Duration
	DrainTimeout  time.Duration
	TrafficRepo   *repository.TrafficRepository
	EventPub      EventPublisher
	OnTeardown    func()
	graceTimers   map[string]*time.Timer
	mu            sync.Mutex
}

func NewDeviceUseCase(
	log *slog.Logger,
	deviceRepo *repository.DeviceRepository,
	adbGW *adb.ADBGateway,
	provisioner SlotProvisioner,
	slotRepo *repository.SlotRepository,
	slotProvision SlotProvisionService,
	ispProber ISPProber,
	watcher DeviceWatcher,
	gracePeriod time.Duration,
	drainTimeout time.Duration,
	trafficRepo *repository.TrafficRepository,
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
		DrainTimeout:  drainTimeout,
		TrafficRepo:   trafficRepo,
		graceTimers:   make(map[string]*time.Timer),
	}
}



// publishDevice publishes the current state of a device as a device_updated event.
func (c *DeviceUseCase) publishDevice(alias string) {
	if c.EventPub == nil {
		return
	}
	resp, err := c.GetByAlias(alias)
	if err != nil {
		return
	}
	c.EventPub.Publish("device_updated", resp)
}

// publishDeviceRemoved publishes a device_removed event.
func (c *DeviceUseCase) publishDeviceRemoved(alias string) {
	if c.EventPub == nil {
		return
	}
	c.EventPub.Publish("device_removed", map[string]string{"alias": alias})
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
		c.publishDevice(device.Alias)
	}

	// Teardown disconnected phones (guarded by c.mu to prevent race with grace timer)
	for _, device := range c.DeviceRepo.ListAll() {
		if device.Status != entity.DeviceStatusOffline && !serialSet[device.Serial] {
			c.mu.Lock()
			// Cancel grace timer if one exists (prevents double teardown)
			if timer, ok := c.graceTimers[device.Serial]; ok {
				timer.Stop()
				delete(c.graceTimers, device.Serial)
			}
			c.Log.Warn("device disconnected", "device", device.Alias, "serial", device.Serial)
			c.teardownDevice(device)
			c.mu.Unlock()
		}
	}

	// Build response
	for _, device := range c.DeviceRepo.ListAll() {
		slotCount := c.SlotRepo.CountByDevice(device.Alias)
		uniqueIPs := c.SlotRepo.UniqueIPsByDevice(device.Alias)
		rx, tx := c.TrafficRepo.TotalByDevice(device.Alias)
		resp.Devices = append(resp.Devices, *converter.DeviceToResponse(device, slotCount, uniqueIPs, rx, tx))
	}
	if resp.Devices == nil {
		resp.Devices = []model.DeviceResponse{}
	}

	return resp, nil
}

// Setup runs the full setup pipeline for a single detected device.
// Configures tethering, network interface, DNS64, and auto-provisions 1 slot.
func (c *DeviceUseCase) Setup(ctx context.Context, alias string) (*model.SetupResponse, error) {
	device, ok := c.DeviceRepo.GetByAlias(alias)
	if !ok {
		return nil, fmt.Errorf("device %s not found", alias)
	}
	if device.Status != entity.DeviceStatusDetected {
		return nil, entity.ErrDeviceNotDetected
	}

	device.Status = entity.DeviceStatusSetup
	c.DeviceRepo.Put(device)
	c.publishDevice(device.Alias)

	if err := c.setup(ctx, device); err != nil {
		c.Log.Warn("setup failed", "device", device.Alias, "serial", device.Serial, "error", err)
		device.SetupStep = ""
		device.Status = entity.DeviceStatusError
		c.DeviceRepo.Put(device)
		c.publishDevice(device.Alias)
		return nil, err
	}

	// Auto-provision 1 slot
	var provResp *model.ProvisionResponse
	prov, provErr := c.SlotProvision.ProvisionSlots(
		device.Alias, device.Interface, 1,
		device.Nameserver, device.NAT64Prefix,
	)
	if provErr != nil {
		c.Log.Warn("auto-provision failed", "device", device.Alias, "error", provErr)
	} else {
		provResp = prov
	}

	slotCount := c.SlotRepo.CountByDevice(device.Alias)
	uniqueIPs := c.SlotRepo.UniqueIPsByDevice(device.Alias)
	rx, tx := c.TrafficRepo.TotalByDevice(device.Alias)
	return &model.SetupResponse{
		Device:    *converter.DeviceToResponse(device, slotCount, uniqueIPs, rx, tx),
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
		uniqueIPs := c.SlotRepo.UniqueIPsByDevice(d.Alias)
		rx, tx := c.TrafficRepo.TotalByDevice(d.Alias)
		result = append(result, *converter.DeviceToResponse(d, slotCount, uniqueIPs, rx, tx))
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
	uniqueIPs := c.SlotRepo.UniqueIPsByDevice(device.Alias)
	rx, tx := c.TrafficRepo.TotalByDevice(device.Alias)
	return converter.DeviceToResponse(device, slotCount, uniqueIPs, rx, tx), nil
}

// Delete tears down a device and removes it from memory.
func (c *DeviceUseCase) Delete(alias string) error {
	device, ok := c.DeviceRepo.GetByAlias(alias)
	if !ok {
		return fmt.Errorf("device %s not found", alias)
	}
	c.teardownDevice(device)
	c.DeviceRepo.Delete(device.Serial)
	c.publishDeviceRemoved(alias)
	return nil
}

// Provision adds more slots to a device.
func (c *DeviceUseCase) Provision(req *model.ProvisionRequest) (*model.ProvisionResponse, error) {
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
	c.Log.Info("watcher started")
	for {
		select {
		case <-ctx.Done():
			c.cancelAllGraceTimers()
			c.Log.Info("watcher stopped")
			return
		case event, ok := <-events:
			if !ok {
				c.Log.Warn("event channel closed")
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
		c.Log.Info("reconnected within grace", "serial", serial)
		c.smartReconnect(serial)
		return
	}

	// Case 2: Already known device
	if device, exists := c.DeviceRepo.GetBySerial(serial); exists {
		// If device was offline/error (e.g. grace expired), reset to detected
		if device.Status == entity.DeviceStatusOffline || device.Status == entity.DeviceStatusError {
			device.Status = entity.DeviceStatusDetected
			c.DeviceRepo.Put(device)
			c.publishDevice(device.Alias)
			c.Log.Info("device re-appeared", "device", device.Alias, "serial", serial)
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
	c.publishDevice(device.Alias)
	c.Log.Info("device detected", "device", device.Alias, "serial", serial)
}

// handleDisconnect handles a device being removed from USB.
func (c *DeviceUseCase) handleDisconnect(serial string) {
	device, ok := c.DeviceRepo.GetBySerial(serial)
	if !ok || device.Status != entity.DeviceStatusOnline {
		return
	}

	c.SlotProvision.SuspendByDevice(device.Alias)
	device.Status = entity.DeviceStatusDisconnected
	c.DeviceRepo.Put(device)
	c.publishDevice(device.Alias)

	// Start grace timer
	alias := device.Alias
	timer := time.AfterFunc(c.GracePeriod, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.graceTimers, serial)
		c.Log.Warn("grace period expired", "device", alias, "serial", serial)
		c.teardownDevice(device)
	})
	c.graceTimers[serial] = timer
	c.Log.Info("grace period started", "device", device.Alias, "serial", serial, "duration", c.GracePeriod.String())
}

func (c *DeviceUseCase) smartReconnect(serial string) {
	device, ok := c.DeviceRepo.GetBySerial(serial)
	if !ok {
		return
	}

	iface, err := c.ADB.DetectInterfaceForSerial(serial)
	if err != nil {
		c.Log.Warn("reconnect failed, interface not found", "device", device.Alias, "error", err)
		c.teardownDevice(device)
		return
	}
	device.Interface = iface

	reattached := c.SlotProvision.ReattachByDevice(device.Alias, iface)

	device.Status = entity.DeviceStatusOnline
	c.DeviceRepo.Put(device)
	c.publishDevice(device.Alias)
	c.Log.Info("smart reconnect complete", "device", device.Alias, "reattached", reattached)
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

// setup runs the device setup steps (private — called by Setup).
func (c *DeviceUseCase) setup(ctx context.Context, device *entity.Device) error {
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
			// Wait for SLAAC (cancellable)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
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
					c.Log.Info("global ipv6 found", "device", device.Alias, "ip", ip.String())
					return nil
				}
			}
			return fmt.Errorf("no global IPv6 address on %s", device.Interface)
		}},
		{"isp_probed", func() error {
			if c.ISPProber == nil {
				c.Log.Warn("isp prober not available", "device", device.Alias)
				return nil
			}

			// Read carrier-assigned DNS from the phone (most reliable source)
			adbDNS, err := c.ADB.GetDNSServers(device.Serial)
			if err != nil {
				c.Log.Warn("adb dns discovery failed", "device", device.Alias, "error", err)
			} else if len(adbDNS) > 0 {
				c.Log.Info("adb dns servers found", "device", device.Alias, "servers", adbDNS)
			}

			result, err := c.ISPProber.Probe(adbDNS)
			if err != nil {
				return fmt.Errorf("ISP probe: %w", err)
			}
			device.Nameserver = result.Nameserver
			device.NAT64Prefix = result.NAT64Prefix
			c.Log.Info("isp probe complete", "device", device.Alias, "nameserver", result.Nameserver, "nat64_prefix", result.NAT64Prefix)
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
		{"device_info", func() error {
			model, brand, version := c.ADB.GetDeviceInfo(device.Serial)
			device.Model = model
			device.Brand = brand
			device.AndroidVersion = version
			return nil
		}},
	}

	for _, step := range steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		device.SetupStep = step.Name
		c.DeviceRepo.Put(device)
		c.publishDevice(device.Alias)
		c.Log.Info("setup step started", "device", device.Alias, "step", step.Name)
		if err := step.Fn(); err != nil {
			return fmt.Errorf("step %s: %w", step.Name, err)
		}
	}

	// Enable NDP proxy on the interface
	if err := c.Provisioner.EnableNDPProxy(device.Interface); err != nil {
		c.Log.Warn("enable ndp proxy failed", "device", device.Alias, "interface", device.Interface, "error", err)
	}

	device.SetupStep = ""
	device.Status = entity.DeviceStatusOnline
	c.DeviceRepo.Put(device)
	c.publishDevice(device.Alias)
	return nil
}

func (c *DeviceUseCase) teardownDevice(device *entity.Device) {
	c.SlotProvision.TeardownByDevice(device.Alias, c.DrainTimeout)
	device.Status = entity.DeviceStatusOffline
	c.DeviceRepo.Put(device)
	c.publishDevice(device.Alias)

	if c.OnTeardown != nil {
		c.OnTeardown()
	}
}

// ListOnlineAliases returns aliases of all devices with status "online".
// Used by controllers to sync per-device proxy ports.
func (c *DeviceUseCase) ListOnlineAliases() []string {
	devices := c.DeviceRepo.ListAll()
	var aliases []string
	for _, d := range devices {
		if d.Status == entity.DeviceStatusOnline {
			aliases = append(aliases, d.Alias)
		}
	}
	return aliases
}


