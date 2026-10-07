package rbmqhandler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"doorservice/dto"
	"doorservice/manager"
	"doorservice/store"

	"github.com/rabbitmq/amqp091-go"
)

type AccessControllerRabbitMQHandler struct {
	base        *BaseHandler
	factory     manager.AdapterFactory
	clients     *store.ClientStore
	controllers *store.ControllerStore
	doors       *store.DoorStore
}

func NewAccessControllerRabbitMQHandler(
	base *BaseHandler,
	factory manager.AdapterFactory,
	clients *store.ClientStore,
	controllers *store.ControllerStore,
	doors *store.DoorStore,
) *AccessControllerRabbitMQHandler {
	if clients == nil {
		clients = store.NewClientStore()
	}
	if controllers == nil {
		controllers = store.NewControllerStore()
	}
	if doors == nil {
		doors = store.NewDoorStore()
	}
	return &AccessControllerRabbitMQHandler{
		base: base, factory: factory, clients: clients, controllers: controllers, doors: doors,
	}
}

func (h *AccessControllerRabbitMQHandler) Handle(delivery amqp091.Delivery) bool {
	var message RBMQMessage
	if err := json.Unmarshal(delivery.Body, &message); err != nil {
		return false
	}
	if err := h.base.ValidateRequest(message); err != nil {
		return false
	}
	if message.Event != EventConnectController && message.Event != EventDiscoverDoors {
		return h.base.PublishError(message, manager.ErrorInvalidRequest, "unsupported access-controller event") == nil
	}
	var controller dto.AccessControllerDTO
	if err := json.Unmarshal(message.Data, &controller); err != nil {
		return h.base.PublishError(message, manager.ErrorInvalidRequest, "invalid access-controller payload") == nil
	}
	// A controller connected for the first time has no VMS id yet; VMS assigns it
	// when it stores the CONNECTED reply. Discovery always targets a stored one.
	if controller.ID == "" && (message.Event == EventDiscoverDoors || controller.IPAddress == "") {
		return h.base.PublishError(message, manager.ErrorInvalidRequest, "invalid access-controller payload") == nil
	}
	if controller.ID == "" {
		if known, ok := h.controllers.FindByAddress(controller.Type, controller.IPAddress, controller.Port); ok {
			controller.ID = known.ID
		}
	}
	if h.factory == nil {
		return h.base.PublishError(message, manager.ErrorInternal, "adapter factory is unavailable") == nil
	}

	err := h.clients.WithController(
		context.Background(),
		controllerKey(controller),
		func() (manager.DoorAdapter, error) { return h.factory.ForController(controller) },
		func(adapter manager.DoorAdapter) error {
			if message.Event == EventConnectController {
				connected, err := adapter.Connect(context.Background(), controller)
				if err != nil {
					return err
				}
				// Refresh credentials/TLS used by door commands of a known controller.
				if connected.ID != "" {
					if err := h.controllers.Upsert(connected); err != nil {
						return err
					}
				}
				return h.base.PublishReply(message, EventConnected, connected)
			}

			discovered, err := adapter.DiscoverDoors(context.Background(), controller)
			if err != nil {
				return err
			}
			_ = h.controllers.Upsert(controller)
			// Discovered doors are only candidates: VMS assigns their ids and registers
			// the ones the user adds through DOOR_CREATE.
			for _, door := range discovered {
				if err := h.base.PublishReply(message, EventDiscovery, door); err != nil {
					return err
				}
			}
			return h.base.PublishReply(message, EventDone, "Done")
		},
	)
	if err != nil {
		return publishHandlerError(h.base, message, err)
	}
	return true
}

func controllerKey(controller dto.AccessControllerDTO) string {
	if controller.ID != "" {
		return controller.ID
	}
	return fmt.Sprintf("%s://%s:%d", strings.ToLower(controller.Type), controller.IPAddress, controller.Port)
}

func publishHandlerError(base *BaseHandler, message RBMQMessage, err error) bool {
	var serviceErr *manager.ServiceError
	if errors.As(err, &serviceErr) {
		return base.PublishError(message, serviceErr.Code, serviceErr.Message) == nil
	}
	return base.PublishError(message, manager.ErrorInternal, "door service request failed") == nil
}
