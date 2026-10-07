package manager

import (
	"context"
	"fmt"

	"doorservice/dto"
)

const (
	ErrorInvalidRequest      = "INVALID_REQUEST"
	ErrorRegistryNotReady    = "REGISTRY_NOT_READY"
	ErrorDoorNotFound        = "DOOR_NOT_FOUND"
	ErrorControllerNotFound  = "CONTROLLER_NOT_FOUND"
	ErrorUnsupportedProtocol = "UNSUPPORTED_PROTOCOL"
	ErrorAuthFailed          = "AUTH_FAILED"
	ErrorDeviceUnreachable   = "DEVICE_UNREACHABLE"
	ErrorCommandRejected     = "COMMAND_REJECTED"
	ErrorCommandTimeout      = "COMMAND_TIMEOUT"
	ErrorDiscoveryCancelled  = "DISCOVERY_CANCELLED"
	ErrorInternal            = "INTERNAL_ERROR"
)

type ServiceError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewServiceError(code, message string, cause error) *ServiceError {
	return &ServiceError{Code: code, Message: message, Cause: cause}
}

type DoorAdapter interface {
	Connect(context.Context, dto.AccessControllerDTO) (dto.AccessControllerDTO, error)
	DiscoverDoors(context.Context, dto.AccessControllerDTO) ([]dto.DoorDTO, error)
	OpenDoor(context.Context, dto.DoorDTO) error
	CloseDoor(context.Context, dto.DoorDTO) error
}

type AdapterFactory interface {
	ForDoor(dto.DoorDTO) (DoorAdapter, error)
	ForController(dto.AccessControllerDTO) (DoorAdapter, error)
}
