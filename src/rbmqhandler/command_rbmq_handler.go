package rbmqhandler

import (
	"context"
	"encoding/json"

	"doorservice/dto"
	"doorservice/manager"
	"doorservice/version"

	"github.com/rabbitmq/amqp091-go"
)

type CommandExecutor interface {
	ExecuteRequest(context.Context, string, dto.DoorCommand) dto.CommandResult
}

type CommandRabbitMQHandler struct {
	base     *BaseHandler
	executor CommandExecutor
}

func NewCommandRabbitMQHandler(base *BaseHandler, executor CommandExecutor) *CommandRabbitMQHandler {
	return &CommandRabbitMQHandler{base: base, executor: executor}
}

func (h *CommandRabbitMQHandler) Handle(delivery amqp091.Delivery) bool {
	var message RBMQMessage
	if err := json.Unmarshal(delivery.Body, &message); err != nil {
		return false
	}
	if err := h.base.ValidateRequest(message); err != nil {
		return false
	}

	switch message.Event {
	case EventGetBuildInfo:
		return h.base.PublishReply(message, EventGetBuildInfo, version.GetBuildInfo()) == nil
	case EventOpenDoor, EventCloseDoor:
		if h.executor == nil {
			return h.base.PublishError(message, manager.ErrorInternal, "command manager is unavailable") == nil
		}
		var command dto.DoorCommand
		if err := json.Unmarshal(message.Data, &command); err != nil {
			return h.base.PublishError(message, manager.ErrorInvalidRequest, "invalid door command payload") == nil
		}
		command.Command = message.Event
		result := h.executor.ExecuteRequest(context.Background(), message.ID, command)
		return h.base.PublishReply(message, message.Event, result) == nil
	default:
		return h.base.PublishError(message, manager.ErrorInvalidRequest, "unsupported command event") == nil
	}
}
