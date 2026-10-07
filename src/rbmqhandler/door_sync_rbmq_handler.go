package rbmqhandler

import (
	"encoding/json"

	"doorservice/dto"
	"doorservice/store"

	"github.com/rabbitmq/amqp091-go"
)

type DoorSyncRabbitMQHandler struct {
	base  *BaseHandler
	doors *store.DoorStore
}

func NewDoorSyncRabbitMQHandler(base *BaseHandler, doors *store.DoorStore) *DoorSyncRabbitMQHandler {
	return &DoorSyncRabbitMQHandler{base: base, doors: doors}
}

func (h *DoorSyncRabbitMQHandler) Handle(delivery amqp091.Delivery) bool {
	var message RBMQMessage
	if err := json.Unmarshal(delivery.Body, &message); err != nil {
		return false
	}
	if err := h.base.ValidateRequest(message); err != nil || h.doors == nil {
		return false
	}

	var incoming dto.DoorDTO
	if err := json.Unmarshal(message.Data, &incoming); err != nil || incoming.ID == "" {
		return false
	}

	switch message.Event {
	case EventDoorCreate:
		if current, ok := h.doors.Get(incoming.ID); ok {
			incoming = mergeDoorSnapshot(current, incoming)
		}
		// One physical door per controller index: drop an entry left under another id.
		if incoming.AccessControllerID != "" && incoming.DoorIndex != nil {
			if stale, ok := h.doors.GetByControllerDoorIndex(incoming.AccessControllerID, *incoming.DoorIndex); ok && stale.ID != incoming.ID {
				h.doors.Delete(stale.ID)
			}
		}
		return h.doors.Upsert(incoming) == nil
	case EventDoorUpdate:
		if current, ok := h.doors.Get(incoming.ID); ok {
			current.Name = incoming.Name
			return h.doors.Upsert(current) == nil
		}
		return h.doors.Upsert(incoming) == nil
	case EventDoorDelete:
		h.doors.Delete(incoming.ID)
		return true
	default:
		return false
	}
}

func mergeDoorSnapshot(current, incoming dto.DoorDTO) dto.DoorDTO {
	if incoming.DoorType == "" {
		incoming.DoorType = current.DoorType
	}
	if incoming.Name == "" {
		incoming.Name = current.Name
	}
	if incoming.BrandName == "" {
		incoming.BrandName = current.BrandName
	}
	if incoming.Username == "" {
		incoming.Username = current.Username
	}
	if incoming.Password == "" {
		incoming.Password = current.Password
	}
	if incoming.IPAddress == "" {
		incoming.IPAddress = current.IPAddress
	}
	if incoming.Port == 0 {
		incoming.Port = current.Port
	}
	if incoming.AccessControllerID == "" {
		incoming.AccessControllerID = current.AccessControllerID
	}
	if incoming.DoorIndex == nil {
		incoming.DoorIndex = current.DoorIndex
	}
	if incoming.ONVIFEndpoint == "" {
		incoming.ONVIFEndpoint = current.ONVIFEndpoint
	}
	if incoming.ONVIFDoorToken == "" {
		incoming.ONVIFDoorToken = current.ONVIFDoorToken
	}
	if incoming.ONVIFLockToken == "" {
		incoming.ONVIFLockToken = current.ONVIFLockToken
	}
	if incoming.ProtocolMetadata == nil {
		incoming.ProtocolMetadata = current.ProtocolMetadata
	}
	if incoming.AIBoxID == "" {
		incoming.AIBoxID = current.AIBoxID
	}
	if incoming.PartnerID == "" {
		incoming.PartnerID = current.PartnerID
	}
	return incoming
}
