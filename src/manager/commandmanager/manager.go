package commandmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"doorservice/dto"
	serviceManager "doorservice/manager"
	"doorservice/manager/registrymanager"
	"doorservice/store"
)

const defaultCommandTimeout = 10 * time.Second

type Option func(*Manager)

type RegistryState interface {
	State() registrymanager.ServiceState
}

func WithClientStore(clients *store.ClientStore) Option {
	return func(m *Manager) {
		if clients != nil {
			m.clients = clients
		}
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(m *Manager) {
		if timeout > 0 {
			m.timeout = timeout
		}
	}
}

func WithRegistry(registry RegistryState) Option {
	return func(m *Manager) {
		m.registry = registry
	}
}

type Manager struct {
	doors       *store.DoorStore
	controllers *store.ControllerStore
	factory     serviceManager.AdapterFactory
	clients     *store.ClientStore
	timeout     time.Duration
	registry    RegistryState
}

func New(
	doors *store.DoorStore,
	controllers *store.ControllerStore,
	factory serviceManager.AdapterFactory,
	options ...Option,
) *Manager {
	if doors == nil {
		doors = store.NewDoorStore()
	}
	if controllers == nil {
		controllers = store.NewControllerStore()
	}
	m := &Manager{
		doors:       doors,
		controllers: controllers,
		factory:     factory,
		clients:     store.NewClientStore(),
		timeout:     defaultCommandTimeout,
	}
	for _, option := range options {
		option(m)
	}
	return m
}

func (m *Manager) Execute(ctx context.Context, command dto.DoorCommand) dto.CommandResult {
	return m.ExecuteRequest(ctx, "", command)
}

func (m *Manager) ExecuteRequest(
	ctx context.Context,
	requestID string,
	command dto.DoorCommand,
) dto.CommandResult {
	result := dto.CommandResult{
		RequestID:   requestID,
		DoorID:      command.DoorID,
		Command:     command.Command,
		CompletedAt: time.Now().UTC(),
	}
	if strings.TrimSpace(command.DoorID) == "" {
		return failResult(result, serviceManager.NewServiceError(serviceManager.ErrorInvalidRequest, "door id is required", nil))
	}
	if command.Command != "OPEN_DOOR" && command.Command != "CLOSE_DOOR" {
		return failResult(result, serviceManager.NewServiceError(serviceManager.ErrorInvalidRequest, "unsupported door command", nil))
	}

	door, ok := m.doors.Get(command.DoorID)
	if !ok {
		if m.registry != nil && m.registry.State() != registrymanager.StateReady {
			return failResult(result, serviceManager.NewServiceError(serviceManager.ErrorRegistryNotReady, "door registry is not ready", nil))
		}
		return failResult(result, serviceManager.NewServiceError(serviceManager.ErrorDoorNotFound, "door was not found", nil))
	}
	if door.AccessControllerID != "" {
		controller, exists := m.controllers.Get(door.AccessControllerID)
		if !exists {
			return failResult(result, serviceManager.NewServiceError(serviceManager.ErrorControllerNotFound, "access controller was not found", nil))
		}
		door = withControllerConnection(door, controller)
	}
	if m.factory == nil {
		return failResult(result, serviceManager.NewServiceError(serviceManager.ErrorInternal, "adapter factory is unavailable", nil))
	}

	operationCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		operationCtx, cancel = context.WithTimeout(ctx, m.timeout)
		defer cancel()
	}
	key := controllerKey(door)
	err := m.clients.WithController(
		operationCtx,
		key,
		func() (serviceManager.DoorAdapter, error) { return m.factory.ForDoor(door) },
		func(adapter serviceManager.DoorAdapter) error {
			if command.Command == "OPEN_DOOR" {
				return adapter.OpenDoor(operationCtx, door)
			}
			return adapter.CloseDoor(operationCtx, door)
		},
	)
	if err != nil {
		return failResult(result, normalizeCommandError(err))
	}
	result.Success = true
	result.CompletedAt = time.Now().UTC()
	return result
}

// withControllerConnection uses the controller's current address, credentials and
// TLS settings. A door's own copy goes stale when the controller is reconnected
// with a new password or IP, and VMS does not persist the door's TLS metadata.
func withControllerConnection(door dto.DoorDTO, controller dto.AccessControllerDTO) dto.DoorDTO {
	if controller.IPAddress != "" {
		door.IPAddress = controller.IPAddress
	}
	if controller.Port != 0 {
		door.Port = controller.Port
	}
	if controller.Username != "" {
		door.Username = controller.Username
	}
	if controller.Password != "" {
		door.Password = controller.Password
	}
	metadata := make(map[string]any, len(door.ProtocolMetadata)+2)
	for key, value := range door.ProtocolMetadata {
		metadata[key] = value
	}
	metadata["tls"] = controller.TLS
	metadata["verifyTls"] = controller.VerifyTLS
	door.ProtocolMetadata = metadata
	return door
}

func controllerKey(door dto.DoorDTO) string {
	if door.AccessControllerID != "" {
		return door.AccessControllerID
	}
	if door.ONVIFEndpoint != "" {
		return door.ONVIFEndpoint
	}
	return fmt.Sprintf("%s://%s:%d", strings.ToLower(door.DoorType), door.IPAddress, door.Port)
}

func normalizeCommandError(err error) error {
	var serviceErr *serviceManager.ServiceError
	if errors.As(err, &serviceErr) {
		return serviceErr
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return serviceManager.NewServiceError(serviceManager.ErrorCommandTimeout, "door command timed out", err)
	}
	return serviceManager.NewServiceError(serviceManager.ErrorInternal, "door command failed", err)
}

func failResult(result dto.CommandResult, err error) dto.CommandResult {
	result.Success = false
	result.CompletedAt = time.Now().UTC()
	var serviceErr *serviceManager.ServiceError
	if errors.As(err, &serviceErr) {
		result.ErrorCode = serviceErr.Code
		result.ErrorMessage = serviceErr.Message
		return result
	}
	result.ErrorCode = serviceManager.ErrorInternal
	result.ErrorMessage = "door command failed"
	return result
}
