//go:build linux

package usecase

import (
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/gateway/adb"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type ISPProber interface {
	Probe(ifaceName string) (*model.ISPProbeResult, error)
}

type SlotProvisionService interface {
	ProvisionSlots(deviceAlias, iface string, count int, nameserver, nat64Prefix string) (*model.ProvisionResponse, error)
}

type DeviceUseCase struct {
	Log           *logrus.Logger
	DeviceRepo    *repository.DeviceRepository
	ADB           *adb.ADBGateway
	Provisioner   SlotProvisioner
	SlotRepo      *repository.SlotRepository
	SlotProvision SlotProvisionService
	ISPProber     ISPProber
}

func NewDeviceUseCase(
	log *logrus.Logger,
	deviceRepo *repository.DeviceRepository,
	adbGW *adb.ADBGateway,
	provisioner SlotProvisioner,
	slotRepo *repository.SlotRepository,
	slotProvision SlotProvisionService,
	ispProber ISPProber,
) *DeviceUseCase {
	return &DeviceUseCase{
		Log:           log,
		DeviceRepo:    deviceRepo,
		ADB:           adbGW,
		Provisioner:   provisioner,
		SlotRepo:      slotRepo,
		SlotProvision: slotProvision,
		ISPProber:     ispProber,
	}
}

// Scan discovers ADB devices, sets up new ones, and tears down missing ones.
// New devices get 1 slot auto-provisioned.
func (c *DeviceUseCase) Scan() (*model.ScanResponse, error) {
	serials, err := c.ADB.ListDevices()
	if err != nil {
		return nil, fmt.Errorf("ADB scan failed: %w", err)
	}

	resp := &model.ScanResponse{}
	serialSet := make(map[string]bool, len(serials))

	// Setup new phones
	for _, serial := range serials {
		serialSet[serial] = true

		if _, exists := c.DeviceRepo.GetBySerial(serial); exists {
			continue // already registered
		}

		resp.Discovered++
		device := &entity.Device{
			Alias:  c.DeviceRepo.NextAlias(),
			Serial: serial,
			Status: entity.DeviceStatusSetup,
		}
		c.DeviceRepo.Put(device)

		if err := c.setup(device); err != nil {
			c.Log.WithError(err).Warnf("scan: setup failed for %s (%s)", device.Alias, serial)
			device.Status = entity.DeviceStatusError
			c.DeviceRepo.Put(device)
			resp.Failed++
			continue
		}

		// Auto-provision 1 slot
		_, provErr := c.SlotProvision.ProvisionSlots(
			device.Alias, device.Interface, 1,
			device.Nameserver, device.NAT64Prefix,
		)
		if provErr != nil {
			c.Log.WithError(provErr).Warnf("scan: provision failed for %s", device.Alias)
		}

		resp.SetupOk++
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

// CheckHealth checks ADB connectivity and marks disconnected devices offline.
func (c *DeviceUseCase) CheckHealth() {
	serials, err := c.ADB.ListDevices()
	if err != nil {
		c.Log.WithError(err).Warn("health check: ADB list failed")
		return
	}

	connectedSet := make(map[string]bool, len(serials))
	for _, s := range serials {
		connectedSet[s] = true
	}

	for _, device := range c.DeviceRepo.ListAll() {
		if device.Status == entity.DeviceStatusOnline && !connectedSet[device.Serial] {
			c.Log.Warnf("health check: device %s (%s) disconnected — marking offline", device.Alias, device.Serial)
			device.Status = entity.DeviceStatusOffline
			c.DeviceRepo.Put(device)
		}
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
			ifaces, err := c.ADB.DetectTetheringInterface()
			if err != nil {
				return err
			}
			if len(ifaces) == 0 {
				return fmt.Errorf("no tethering interface detected")
			}
			device.Interface = ifaces[0]
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
			result, err := c.ISPProber.Probe(device.Interface)
			if err != nil {
				c.Log.Warnf("device %s: ISP probe failed: %v — using defaults", device.Alias, err)
				return nil
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
