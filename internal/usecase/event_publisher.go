package usecase

type EventPublisher interface {
	Publish(topic string, data interface{})
}

type NoopPublisher struct{}

func (n *NoopPublisher) Publish(topic string, data interface{}) {}
