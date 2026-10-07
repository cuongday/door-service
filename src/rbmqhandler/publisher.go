package rbmqhandler

import (
	"encoding/json"

	commonrbmq "commonkit/rbmq"
)

type Publisher interface {
	Publish(queue string, message RBMQMessage) error
}

type MessageQueueProducer interface {
	PublishMessageToQueue(routingKey string, message string)
}

type JSONPublisher struct {
	producer MessageQueueProducer
}

func NewPublisher(producer MessageQueueProducer) *JSONPublisher {
	return &JSONPublisher{producer: producer}
}

func NewCommonkitPublisher(producer *commonrbmq.ProducerRabbitMQ) *JSONPublisher {
	return NewPublisher(producer)
}

func (p *JSONPublisher) Publish(queue string, message RBMQMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	p.producer.PublishMessageToQueue(queue, string(payload))
	return nil
}
