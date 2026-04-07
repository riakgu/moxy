package entity

// EventPublisher allows usecases to publish state changes
// without knowing about the SSE transport layer.
type EventPublisher interface {
	Publish(topic string, data interface{})
}

// NoopPublisher is a no-op implementation for when SSE is not wired.
type NoopPublisher struct{}

func (n *NoopPublisher) Publish(topic string, data interface{}) {}
