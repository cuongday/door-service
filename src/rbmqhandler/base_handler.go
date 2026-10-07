package rbmqhandler

import (
	"encoding/json"
	"fmt"
	"time"

	"doorservice/manager"
)

type BaseHandler struct {
	publisher Publisher
}

func NewBaseHandler(publisher Publisher) *BaseHandler {
	return &BaseHandler{publisher: publisher}
}

func (h *BaseHandler) ValidateRequest(message RBMQMessage) error {
	if message.ID == "" {
		return manager.NewServiceError(manager.ErrorInvalidRequest, "message id is required", nil)
	}
	if message.Event == "" {
		return manager.NewServiceError(manager.ErrorInvalidRequest, "message event is required", nil)
	}
	if requiresReplyQueue(message.Event) && message.ReplyQueueName == "" {
		return manager.NewServiceError(manager.ErrorInvalidRequest, "reply queue is required", nil)
	}
	return nil
}

func (h *BaseHandler) PublishError(request RBMQMessage, code, message string) error {
	if request.ReplyQueueName == "" {
		return fmt.Errorf("reply queue is required")
	}
	data, err := json.Marshal(manager.NewServiceError(code, message, nil))
	if err != nil {
		return err
	}
	return h.publisher.Publish(request.ReplyQueueName, RBMQMessage{
		ID:        request.ID,
		CreatedAt: time.Now().UTC(),
		Dst:       request.ReplyQueueName,
		Event:     EventError,
		Data:      data,
		Username:  request.Username,
	})
}

func (h *BaseHandler) PublishReply(request RBMQMessage, event string, value any) error {
	if request.ReplyQueueName == "" {
		return fmt.Errorf("reply queue is required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return h.publisher.Publish(request.ReplyQueueName, RBMQMessage{
		ID:        request.ID,
		CreatedAt: time.Now().UTC(),
		Dst:       request.ReplyQueueName,
		Event:     event,
		Data:      data,
		Username:  request.Username,
	})
}

func requiresReplyQueue(event string) bool {
	switch event {
	case EventDiscoveryDoorStop, EventDoorCreate, EventDoorUpdate, EventDoorDelete:
		return false
	default:
		return true
	}
}
