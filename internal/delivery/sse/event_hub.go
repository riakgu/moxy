package sse

import (
	"sync"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/entity"
)

// Event represents a published state change.
type Event struct {
	Topic string
	Data  interface{}
}

// EventHub is an in-process pub/sub hub that fans out events to SSE clients.
type EventHub struct {
	log        *logrus.Logger
	publishCh  chan Event
	clients    map[string]chan Event
	mu         sync.RWMutex
	bufSize    int
	maxClients int
	done       chan struct{}
}

// Ensure EventHub implements entity.EventPublisher.
var _ entity.EventPublisher = (*EventHub)(nil)

func NewEventHub(log *logrus.Logger, maxClients int) *EventHub {
	if maxClients <= 0 {
		maxClients = 10
	}
	return &EventHub{
		log:        log,
		publishCh:  make(chan Event, 256),
		clients:    make(map[string]chan Event),
		bufSize:    64,
		maxClients: maxClients,
		done:       make(chan struct{}),
	}
}

// Run starts the fan-out goroutine. Blocks until Shutdown is called.
func (h *EventHub) Run() {
	for {
		select {
		case <-h.done:
			return
		case evt, ok := <-h.publishCh:
			if !ok {
				return
			}
			h.mu.RLock()
			for id, ch := range h.clients {
				select {
				case ch <- evt:
				default:
					h.log.Warnf("sse: dropping event for slow client %s", id)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Publish sends an event to all connected clients (non-blocking).
func (h *EventHub) Publish(topic string, data interface{}) {
	select {
	case h.publishCh <- Event{Topic: topic, Data: data}:
	default:
		h.log.Warn("sse: publish channel full, dropping event")
	}
}

// Subscribe registers a new SSE client. Returns client ID and event channel.
// Returns "", nil if max clients exceeded.
func (h *EventHub) Subscribe() (string, chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.clients) >= h.maxClients {
		return "", nil
	}

	id := uuid.NewString()
	ch := make(chan Event, h.bufSize)
	h.clients[id] = ch
	h.log.Infof("sse: client %s connected (%d total)", id, len(h.clients))
	return id, ch
}

// Unsubscribe removes a client and closes its channel.
func (h *EventHub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ch, ok := h.clients[id]; ok {
		close(ch)
		delete(h.clients, id)
		h.log.Infof("sse: client %s disconnected (%d remaining)", id, len(h.clients))
	}
}

// Shutdown closes all client channels and stops the fan-out goroutine.
func (h *EventHub) Shutdown() {
	close(h.done)

	h.mu.Lock()
	defer h.mu.Unlock()

	for id, ch := range h.clients {
		close(ch)
		delete(h.clients, id)
	}
	h.log.Info("sse: hub shut down")
}

// ClientCount returns the current number of connected clients.
func (h *EventHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
