package onvifmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"doorservice/dto"
	"doorservice/manager"
)

type SOAPTransport interface {
	Call(context.Context, string, string, string, Credential) ([]byte, error)
}

type Manager struct {
	transport SOAPTransport
}

func New(client *http.Client) *Manager {
	return NewWithTransport(NewTransport(client, 10*time.Second))
}

func NewWithTransport(transport SOAPTransport) *Manager {
	return &Manager{transport: transport}
}

func (m *Manager) Connect(ctx context.Context, controller dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	connected, _, err := m.inspect(ctx, controller)
	return connected, err
}

func (m *Manager) DiscoverDoors(ctx context.Context, controller dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	connected, endpoints, err := m.inspect(ctx, controller)
	if err != nil {
		return nil, err
	}
	doorEndpoint := findServiceEndpoint(endpoints, DoorControlNamespace)
	if doorEndpoint == "" {
		return nil, manager.NewServiceError(manager.ErrorUnsupportedProtocol, "ONVIF Door Control service is unavailable", nil)
	}

	credential := Credential{Username: controller.Username, Password: controller.Password}
	result := make([]dto.DoorDTO, 0)
	nextReference := ""
	for page := 0; page < 100; page++ {
		body := `<tdc:GetDoorInfoList/>`
		if nextReference != "" {
			body = `<tdc:GetDoorInfoList><tdc:StartReference>` + xmlEscape(nextReference) + `</tdc:StartReference></tdc:GetDoorInfoList>`
		}
		document, callErr := m.transport.Call(
			ctx,
			doorEndpoint,
			DoorControlNamespace+"/GetDoorInfoList",
			body,
			credential,
		)
		if callErr != nil {
			return nil, mapONVIFError(callErr)
		}
		infos, following := parseDoorInfoList(document)
		for _, info := range infos {
			idPrefix := connected.ID
			if idPrefix == "" {
				idPrefix = doorEndpoint
			}
			result = append(result, dto.DoorDTO{
				ID:                 idPrefix + ":" + info.Token,
				DoorType:           "ONVIF",
				Name:               info.Name,
				BrandName:          connected.Manufacturer,
				Username:           controller.Username,
				Password:           controller.Password,
				IPAddress:          controller.IPAddress,
				Port:               controller.Port,
				Activate:           true,
				AccessControllerID: controller.ID,
				ONVIFEndpoint:      doorEndpoint,
				ONVIFDoorToken:     info.Token,
				ONVIFLockToken:     info.LockToken,
				AIBoxID:            controller.AIBoxID,
				PartnerID:          controller.PartnerID,
				ProtocolMetadata: map[string]any{
					"supportsAccessDoor": true,
					"supportsLockDoor":   true,
				},
			})
		}
		if following == "" || following == nextReference {
			break
		}
		nextReference = following
	}
	return result, nil
}

func (m *Manager) OpenDoor(ctx context.Context, door dto.DoorDTO) error {
	command := "AccessDoor"
	if supported, ok := metadataBool(door.ProtocolMetadata, "supportsAccessDoor"); ok && !supported {
		command = "UnlockDoor"
	}
	return m.controlDoor(ctx, door, command)
}

func (m *Manager) CloseDoor(ctx context.Context, door dto.DoorDTO) error {
	if supported, ok := metadataBool(door.ProtocolMetadata, "supportsLockDoor"); ok && !supported {
		return manager.NewServiceError(manager.ErrorUnsupportedProtocol, "ONVIF LockDoor is unavailable", nil)
	}
	return m.controlDoor(ctx, door, "LockDoor")
}

func (m *Manager) inspect(
	ctx context.Context,
	controller dto.AccessControllerDTO,
) (dto.AccessControllerDTO, []serviceEndpoint, error) {
	endpoint := deviceEndpoint(controller)
	credential := Credential{Username: controller.Username, Password: controller.Password}
	deviceInfo, err := m.transport.Call(
		ctx,
		endpoint,
		DeviceNamespace+"/GetDeviceInformation",
		`<tds:GetDeviceInformation/>`,
		credential,
	)
	if err != nil {
		return dto.AccessControllerDTO{}, nil, mapONVIFError(err)
	}
	services, err := m.transport.Call(
		ctx,
		endpoint,
		DeviceNamespace+"/GetServices",
		`<tds:GetServices><tds:IncludeCapability>true</tds:IncludeCapability></tds:GetServices>`,
		credential,
	)
	if err != nil {
		return dto.AccessControllerDTO{}, nil, mapONVIFError(err)
	}
	endpoints := parseServiceEndpoints(services)
	if findServiceEndpoint(endpoints, DoorControlNamespace) == "" && !supportsAccessControl(endpoints) {
		return dto.AccessControllerDTO{}, nil, manager.NewServiceError(
			manager.ErrorUnsupportedProtocol,
			"device does not expose ONVIF access-control services",
			nil,
		)
	}
	return applyDeviceInformation(controller, deviceInfo), endpoints, nil
}

func (m *Manager) controlDoor(ctx context.Context, door dto.DoorDTO, command string) error {
	if door.ONVIFEndpoint == "" || door.ONVIFDoorToken == "" {
		return manager.NewServiceError(manager.ErrorInvalidRequest, "ONVIF endpoint and door token are required", nil)
	}
	_, err := m.transport.Call(
		ctx,
		door.ONVIFEndpoint,
		DoorControlNamespace+"/"+command,
		doorCommandBody(command, door.ONVIFDoorToken),
		Credential{Username: door.Username, Password: door.Password},
	)
	if err != nil {
		return mapONVIFError(err)
	}
	return nil
}

func metadataBool(metadata map[string]any, key string) (bool, bool) {
	if metadata == nil {
		return false, false
	}
	value, ok := metadata[key].(bool)
	return value, ok
}

func mapONVIFError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return manager.NewServiceError(manager.ErrorCommandTimeout, "ONVIF request timed out", err)
	}
	var fault *SOAPFault
	if errors.As(err, &fault) {
		reason := strings.ToLower(fault.Reason)
		if strings.Contains(reason, "auth") || strings.Contains(reason, "authorized") {
			return manager.NewServiceError(manager.ErrorAuthFailed, "ONVIF authentication failed", err)
		}
		return manager.NewServiceError(manager.ErrorCommandRejected, "ONVIF command was rejected", err)
	}
	return manager.NewServiceError(manager.ErrorDeviceUnreachable, fmt.Sprintf("ONVIF request failed: %v", err), err)
}
