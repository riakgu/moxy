package repository

import (
	"fmt"
	"sync"
	"sync/atomic"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
)

type DeviceRepository struct {
	mu       sync.RWMutex
	devices  map[string]*entity.Device // keyed by serial
	log      *slog.Logger
	aliasSeq uint64
}

func NewDeviceRepository(log *slog.Logger) *DeviceRepository {
	return &DeviceRepository{
		devices: make(map[string]*entity.Device),
		log:     log,
	}
}

func (r *DeviceRepository) Put(device *entity.Device) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[device.Serial] = device
}

func (r *DeviceRepository) GetBySerial(serial string) (*entity.Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[serial]
	return d, ok
}

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

func (r *DeviceRepository) Delete(serial string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, serial)
}

func (r *DeviceRepository) ListAll() []*entity.Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*entity.Device, 0, len(r.devices))
	for _, d := range r.devices {
		result = append(result, d)
	}
	return result
}

func (r *DeviceRepository) NextAlias() string {
	n := atomic.AddUint64(&r.aliasSeq, 1)
	return fmt.Sprintf("dev%d", n)
}
