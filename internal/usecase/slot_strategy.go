package usecase

import (
	"math/rand"
	"sync/atomic"

	"github.com/riakgu/moxy/internal/entity"
)

// SlotStrategy selects a slot from a non-empty list of candidates.
type SlotStrategy interface {
	Select(slots []*entity.Slot) *entity.Slot
}

// RandomStrategy picks a random slot.
type RandomStrategy struct{}

func (s *RandomStrategy) Select(slots []*entity.Slot) *entity.Slot {
	return slots[rand.Intn(len(slots))]
}

// RoundRobinStrategy cycles through slots in order.
type RoundRobinStrategy struct {
	index uint64
}

func (s *RoundRobinStrategy) Select(slots []*entity.Slot) *entity.Slot {
	idx := atomic.AddUint64(&s.index, 1)
	return slots[idx%uint64(len(slots))]
}

// LeastConnectionsStrategy picks the slot with the fewest active connections.
type LeastConnectionsStrategy struct{}

func (s *LeastConnectionsStrategy) Select(slots []*entity.Slot) *entity.Slot {
	best := slots[0]
	bestConns := atomic.LoadInt64(&best.ActiveConnections)
	for _, slot := range slots[1:] {
		conns := atomic.LoadInt64(&slot.ActiveConnections)
		if conns < bestConns {
			best = slot
			bestConns = conns
		}
	}
	return best
}

// NewSlotStrategy creates a strategy by name. Defaults to random.
func NewSlotStrategy(name string) SlotStrategy {
	switch name {
	case "round-robin":
		return &RoundRobinStrategy{}
	case "least-connections":
		return &LeastConnectionsStrategy{}
	default:
		return &RandomStrategy{}
	}
}
