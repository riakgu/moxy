package repository

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"log/slog"

	"github.com/riakgu/moxy/internal/entity"
)

type DeviceRepository struct {
	mu         sync.RWMutex
	devices    map[string]*entity.Device // keyed by serial
	log        *slog.Logger
	maxDevices int
	freeList   []int
	usedSet    map[int]bool
}

func NewDeviceRepository(log *slog.Logger, maxDevices int) *DeviceRepository {
	if maxDevices <= 0 {
		maxDevices = 2
	}
	freeList := make([]int, maxDevices)
	for i := 0; i < maxDevices; i++ {
		freeList[i] = maxDevices - i // [maxDevices, ..., 2, 1] — pop gives 1 first
	}
	return &DeviceRepository{
		devices:    make(map[string]*entity.Device),
		log:        log,
		maxDevices: maxDevices,
		freeList:   freeList,
		usedSet:    make(map[int]bool),
	}
}

// AllocateAlias assigns the next available device alias (dev1, dev2, etc.)
// Returns the alias and true if successful, empty string and false if pool is full.
func (r *DeviceRepository) AllocateAlias() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.freeList) == 0 {
		return "", false
	}
	idx := r.freeList[len(r.freeList)-1]
	r.freeList = r.freeList[:len(r.freeList)-1]
	r.usedSet[idx] = true
	return fmt.Sprintf("dev%d", idx), true
}

// ReleaseAlias returns a device alias index to the free pool.
func (r *DeviceRepository) ReleaseAlias(alias string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := parseDeviceIdx(alias)
	if idx <= 0 || !r.usedSet[idx] {
		return
	}
	delete(r.usedSet, idx)
	r.freeList = append(r.freeList, idx)
}

func parseDeviceIdx(alias string) int {
	if !strings.HasPrefix(alias, "dev") {
		return 0
	}
	n, err := strconv.Atoi(alias[3:])
	if err != nil {
		return 0
	}
	return n
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
	if d, ok := r.devices[serial]; ok {
		delete(r.devices, serial)
		if d.Alias != "" {
			idx := parseDeviceIdx(d.Alias)
			if idx > 0 && r.usedSet[idx] {
				delete(r.usedSet, idx)
				r.freeList = append(r.freeList, idx)
			}
		}
	}
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
