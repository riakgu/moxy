package repository

import (
	"log/slog"
	"sync"

	"github.com/riakgu/moxy/internal/entity"
)

type LogRepository struct {
	mu      sync.RWMutex
	entries []entity.LogEntry
	size    int
	pos     int
	count   int
	log     *slog.Logger
}

func NewLogRepository(log *slog.Logger, size int) *LogRepository {
	if size <= 0 {
		size = 1000
	}
	return &LogRepository{
		entries: make([]entity.LogEntry, size),
		size:    size,
		log:     log,
	}
}

func (r *LogRepository) Append(entry entity.LogEntry) {
	r.mu.Lock()
	r.entries[r.pos] = entry
	r.pos = (r.pos + 1) % r.size
	if r.count < r.size {
		r.count++
	}
	r.mu.Unlock()
}

func (r *LogRepository) GetRecent() []entity.LogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.count == 0 {
		return nil
	}

	result := make([]entity.LogEntry, 0, r.count)
	start := 0
	if r.count >= r.size {
		start = r.pos
	}

	for i := 0; i < r.count; i++ {
		idx := (start + i) % r.size
		result = append(result, r.entries[idx])
	}
	return result
}
