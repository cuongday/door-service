package rbmqhandler

import (
	"context"
	"encoding/json"

	"doorservice/dto"
	"doorservice/manager"

	"github.com/rabbitmq/amqp091-go"
)

type DiscoveryService interface {
	Start(
		context.Context,
		string,
		dto.DiscoveryDoorInfoDTO,
		func(dto.DiscoveryResult),
		func(),
	) error
	Stop(string) bool
}

type DiscoveryRabbitMQHandler struct {
	base      *BaseHandler
	discovery DiscoveryService
	ctx       context.Context
}

func NewDiscoveryRabbitMQHandler(
	base *BaseHandler,
	discovery DiscoveryService,
	contexts ...context.Context,
) *DiscoveryRabbitMQHandler {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	return &DiscoveryRabbitMQHandler{base: base, discovery: discovery, ctx: ctx}
}

func (h *DiscoveryRabbitMQHandler) Handle(delivery amqp091.Delivery) bool {
	var message RBMQMessage
	if err := json.Unmarshal(delivery.Body, &message); err != nil {
		return false
	}
	if err := h.base.ValidateRequest(message); err != nil {
		return false
	}
	if h.discovery == nil {
		return publishHandlerError(h.base, message, manager.NewServiceError(manager.ErrorInternal, "discovery manager is unavailable", nil))
	}
	if message.Event == EventDiscoveryDoorStop {
		h.discovery.Stop(message.ID)
		return true
	}
	if message.Event != EventDiscoveryDoor {
		return h.base.PublishError(message, manager.ErrorInvalidRequest, "unsupported discovery event") == nil
	}

	var input dto.DiscoveryDoorInfoDTO
	if err := json.Unmarshal(message.Data, &input); err != nil {
		return h.base.PublishError(message, manager.ErrorInvalidRequest, "invalid discovery payload") == nil
	}
	err := h.discovery.Start(
		h.ctx,
		message.ID,
		input,
		func(result dto.DiscoveryResult) {
			_ = h.base.PublishReply(message, EventDiscoveryDoor, result)
		},
		func() {
			_ = h.base.PublishReply(message, EventDiscoveryDoor, "Done")
		},
	)
	if err != nil {
		return publishHandlerError(h.base, message, err)
	}
	return true
}
