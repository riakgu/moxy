package repository

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
)

// DeviceRepository is a thread-safe in-memory store for devices, keyed by serial.
type DeviceRepository struct {
	mu       sync.RWMutex
	devices  map[string]*entity.Device // keyed by serial
	log      *logrus.Logger
	aliasSeq uint64
}

func NewDeviceRepository(log *logrus.Logger) *DeviceRepository {
	return &DeviceRepository{
		devices: make(map[string]*entity.Device),
		log:     log,
	}
}

// Put stores or updates a device.
func (r *DeviceRepository) Put(device *entity.Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[device.Serial] = device
}

// GetBySerial returns a device by its ADB serial.
func (r *DeviceRepository) GetBySerial(serial string) (*entity.Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[serial]
	return d, ok
}

// GetByAlias returns a device by its alias (e.g., "dev1").
func (r *DeviceRepository) GetByAlias(alias string) (*entity.Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.devices {
		if d.Alias == alias {
			return d, true
		}
	}
	return nil, false
}

// Delete removes a device by serial.
func (r *DeviceRepository) Delete(serial string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, serial)
}

// ListAll returns all devices.
func (r *DeviceRepository) ListAll() []*entity.Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*entity.Device, 0, len(r.devices))
	for _, d := range r.devices {
		result = append(result, d)
	}
	return result
}

// NextAlias generates the next device alias (dev1, dev2, ...).
func (r *DeviceRepository) NextAlias() string {
	n := atomic.AddUint64(&r.aliasSeq, 1)
	return fmt.Sprintf("dev%d", n)
}
