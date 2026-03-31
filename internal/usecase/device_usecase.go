//go:build linux

package usecase

import (
	"database/sql"
	"fmt"
	"os/exec"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/gateway/adb"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type DeviceUseCase struct {
	Log         *logrus.Logger
	Validate    *validator.Validate
	DB          *sql.DB
	DeviceRepo  *repository.DeviceRepository
	ADB         *adb.ADBGateway
	Provisioner SlotProvisioner
	DNS64Server string
}

func NewDeviceUseCase(log *logrus.Logger, validate *validator.Validate, db *sql.DB,
	deviceRepo *repository.DeviceRepository, adbGW *adb.ADBGateway,
	provisioner SlotProvisioner, dns64 string) *DeviceUseCase {
	return &DeviceUseCase{
		Log: log, Validate: validate, DB: db,
		DeviceRepo: deviceRepo, ADB: adbGW,
		Provisioner: provisioner, DNS64Server: dns64,
	}
}

func (c *DeviceUseCase) ListADBDevices() ([]string, error) {
	return c.ADB.ListDevices()
}

func (c *DeviceUseCase) Register(req *model.RegisterDeviceRequest) (*model.DeviceResponse, error) {
	if err := c.Validate.Struct(req); err != nil {
		return nil, err
	}
	device := &entity.Device{
		ID:       uuid.NewString(),
		Serial:   req.Serial,
		Alias:    req.Alias,
		Status:   entity.DeviceStatusOffline,
		MaxSlots: req.MaxSlots,
	}
	if err := c.DeviceRepo.Create(c.DB, device); err != nil {
		return nil, err
	}
	return converter.DeviceToResponse(device), nil
}

func (c *DeviceUseCase) List() ([]model.DeviceResponse, error) {
	devices, err := c.DeviceRepo.FindAll(c.DB)
	if err != nil {
		return nil, err
	}
	var result []model.DeviceResponse
	for _, d := range devices {
		result = append(result, *converter.DeviceToResponse(d))
	}
	return result, nil
}

func (c *DeviceUseCase) GetByID(id string) (*model.DeviceResponse, error) {
	device, err := c.DeviceRepo.FindByID(c.DB, id)
	if err != nil {
		return nil, err
	}
	return converter.DeviceToResponse(device), nil
}

func (c *DeviceUseCase) Setup(req *model.SetupDeviceRequest) (*model.SetupProgressResponse, error) {
	device, err := c.DeviceRepo.FindByID(c.DB, req.DeviceId)
	if err != nil {
		return nil, err
	}

	device.Status = entity.DeviceStatusSetup
	c.DeviceRepo.Update(c.DB, device)

	progress := &model.SetupProgressResponse{
		DeviceId: device.ID,
		Status:   "running",
	}

	type setupStep struct {
		Name string
		Fn   func() error
	}

	dns64 := c.DNS64Server
	if device.Nameserver != "" {
		dns64 = device.Nameserver
	}
	_ = dns64 // used in future phases

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
			// Use first available if not already assigned
			if device.Interface == "" {
				device.Interface = ifaces[0]
			}
			return nil
		}},
		{"enabled_data", func() error { return c.ADB.EnableData(device.Serial) }},
		{"dismissed_dialog", func() error { return c.ADB.DismissDataDialog(device.Serial) }},
		{"disabled_wifi", func() error { return c.ADB.DisableWifi(device.Serial) }},
		{"dhcp_configured", func() error {
			return exec.Command("dhcpcd", device.Interface).Run()
		}},
		{"ipv6_configured", func() error {
			cmds := [][]string{
				{"sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.accept_ra=2", device.Interface)},
				{"sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.autoconf=1", device.Interface)},
			}
			for _, cmd := range cmds {
				if err := exec.Command(cmd[0], cmd[1:]...).Run(); err != nil {
					return fmt.Errorf("%s: %w", cmd[0], err)
				}
			}
			time.Sleep(3 * time.Second) // Wait for SLAAC
			return nil
		}},
		{"isp_detected", func() error {
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
			progress.FailedAt = step.Name
			progress.Error = err.Error()
			progress.Status = "failed"
			device.Status = entity.DeviceStatusError
			c.DeviceRepo.Update(c.DB, device)
			return progress, err
		}
		progress.CompletedSteps = append(progress.CompletedSteps, step.Name)
	}

	// Enable NDP proxy on the interface
	c.Provisioner.EnableNDPProxy(device.Interface)

	device.Status = entity.DeviceStatusOnline
	c.DeviceRepo.Update(c.DB, device)

	progress.Status = "completed"
	return progress, nil
}

func (c *DeviceUseCase) Teardown(deviceId string) error {
	device, err := c.DeviceRepo.FindByID(c.DB, deviceId)
	if err != nil {
		return err
	}

	// Destroy all namespaces for this device
	namespaces, err := c.Provisioner.ListSlotNamespacesForDevice(device.Alias)
	if err != nil {
		return err
	}
	for _, ns := range namespaces {
		if err := c.Provisioner.DestroySlot(ns); err != nil {
			c.Log.WithError(err).Warnf("failed to destroy slot %s", ns)
		}
	}

	device.Status = entity.DeviceStatusOffline
	return c.DeviceRepo.Update(c.DB, device)
}

func (c *DeviceUseCase) Delete(deviceId string) error {
	if err := c.Teardown(deviceId); err != nil {
		c.Log.WithError(err).Warn("teardown failed during delete")
	}
	return c.DeviceRepo.Delete(c.DB, deviceId)
}

func (c *DeviceUseCase) UpdateISPOverride(req *model.UpdateISPOverrideRequest) (*model.DeviceResponse, error) {
	device, err := c.DeviceRepo.FindByID(c.DB, req.DeviceId)
	if err != nil {
		return nil, err
	}
	device.Nameserver = req.Nameserver
	device.NAT64Prefix = req.NAT64Prefix
	if err := c.DeviceRepo.Update(c.DB, device); err != nil {
		return nil, err
	}
	return converter.DeviceToResponse(device), nil
}
