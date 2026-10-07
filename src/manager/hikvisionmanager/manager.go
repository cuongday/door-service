package hikvisionmanager

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"doorservice/dto"
	serviceManager "doorservice/manager"
)

type Manager struct {
	client *client
}

func New(httpClient *http.Client) *Manager {
	return &Manager{client: newClient(httpClient)}
}

func (m *Manager) Connect(ctx context.Context, controller dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	endpoint := baseURL(controller.TLS, controller.IPAddress, controller.Port) + "/ISAPI/System/deviceInfo"
	payload, status, err := m.client.request(
		ctx, http.MethodGet, endpoint, controller.Username, controller.Password, controller.VerifyTLS, nil,
	)
	if err != nil {
		return dto.AccessControllerDTO{}, mapClientError(err)
	}
	if err := statusError(status); err != nil {
		return dto.AccessControllerDTO{}, err
	}

	// Parse device info using xmlutil
	var deviceInfo ISAPIDeviceInfo
	if err := xml.Unmarshal(payload, &deviceInfo); err != nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse ISAPI device info", err)
	}

	controller.Type = "HIKVISION"
	controller.Manufacturer = "Hikvision" // Hardcoded - ISAPI doesn't have manufacturer tag
	controller.DeviceModel = strings.TrimSpace(deviceInfo.Model)
	controller.SerialNumber = strings.TrimSpace(deviceInfo.SerialNumber)
	controller.FirmwareVersion = strings.TrimSpace(deviceInfo.FirmwareVersion)
	controller.MacAddress = strings.TrimSpace(deviceInfo.MacAddress)
	controller.DeviceType = strings.TrimSpace(deviceInfo.DeviceType)
	controller.DeviceInfoRaw = string(payload)
	controller.State = "CONNECTED"
	controller.Activate = true

	return controller, nil
}

// maxProbedDoors bounds the Door/param fallback probe; Hikvision panels have at most 8 doors.
const maxProbedDoors = 8

// DiscoverDoors lists the controller's doors. ISAPI has no door list endpoint, so the
// count comes from AcsWorkStatus (one status entry per door) and each name from
// Door/param/<n>. n is the same doorNo used by RemoteControl/door/<n>.
func (m *Manager) DiscoverDoors(ctx context.Context, controller dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	connected, err := m.Connect(ctx, controller)
	if err != nil {
		return nil, err
	}
	base := baseURL(controller.TLS, controller.IPAddress, controller.Port)

	count, err := m.doorCount(ctx, controller, base)
	if err != nil {
		return nil, err
	}
	names := make(map[int]string, count)
	if count == 0 {
		// AcsWorkStatus is unsupported or empty: probe Door/param until a door is missing.
		for index := 1; index <= maxProbedDoors; index++ {
			name, found, err := m.doorName(ctx, controller, base, index)
			if err != nil {
				return nil, err
			}
			if !found {
				break
			}
			names[index] = name
			count = index
		}
	} else {
		for index := 1; index <= count; index++ {
			name, _, err := m.doorName(ctx, controller, base, index)
			if err != nil {
				return nil, err
			}
			names[index] = name
		}
	}
	if count == 0 {
		return nil, serviceManager.NewServiceError(serviceManager.ErrorCommandRejected,
			"Hikvision controller reported no doors", nil)
	}

	doors := make([]dto.DoorDTO, 0, count)
	for index := 1; index <= count; index++ {
		name := names[index]
		if name == "" {
			name = fmt.Sprintf("Door %d", index)
		}
		doorIndex := index
		doors = append(doors, dto.DoorDTO{
			DoorType:           "HIKVISION",
			Name:               name,
			BrandName:          connected.Manufacturer,
			Username:           controller.Username,
			Password:           controller.Password,
			IPAddress:          controller.IPAddress,
			Port:               controller.Port,
			Activate:           true,
			AccessControllerID: controller.ID,
			DoorIndex:          &doorIndex,
			AIBoxID:            controller.AIBoxID,
			PartnerID:          controller.PartnerID,
			ProtocolMetadata: map[string]any{
				"tls":       controller.TLS,
				"verifyTls": controller.VerifyTLS,
			},
		})
	}
	return doors, nil
}

// doorCount returns the number of doors reported by AcsWorkStatus, or 0 when the
// device does not support it so the caller falls back to probing Door/param.
func (m *Manager) doorCount(ctx context.Context, controller dto.AccessControllerDTO, base string) (int, error) {
	payload, status, err := m.client.request(
		ctx, http.MethodGet, base+"/ISAPI/AccessControl/AcsWorkStatus?format=json",
		controller.Username, controller.Password, controller.VerifyTLS, nil,
	)
	if err != nil {
		return 0, mapClientError(err)
	}
	if status == http.StatusUnauthorized {
		return 0, statusError(status)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return 0, nil
	}
	var workStatus struct {
		AcsWorkStatus struct {
			DoorLockStatus []int `json:"doorLockStatus"`
			DoorStatus     []int `json:"doorStatus"`
		} `json:"AcsWorkStatus"`
	}
	if json.Unmarshal(payload, &workStatus) != nil {
		return 0, nil
	}
	count := len(workStatus.AcsWorkStatus.DoorLockStatus)
	if count == 0 {
		count = len(workStatus.AcsWorkStatus.DoorStatus)
	}
	if count > maxProbedDoors*4 {
		count = maxProbedDoors * 4
	}
	return count, nil
}

// doorName reads Door/param/<index>. found is false when the device reports the door
// does not exist; a door that exists but has unreadable parameters keeps a default name.
// Only doorName is read: the payload also carries door passwords and is never stored.
func (m *Manager) doorName(ctx context.Context, controller dto.AccessControllerDTO, base string, index int) (string, bool, error) {
	payload, status, err := m.client.request(
		ctx, http.MethodGet, base+fmt.Sprintf("/ISAPI/AccessControl/Door/param/%d", index),
		controller.Username, controller.Password, controller.VerifyTLS, nil,
	)
	if err != nil {
		return "", false, mapClientError(err)
	}
	if status == http.StatusUnauthorized {
		// Stop at once: continuing with bad credentials can lock the device account.
		return "", false, statusError(status)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return "", false, nil
	}
	var param ISAPIDoorParam
	if xml.Unmarshal(payload, &param) != nil {
		return "", true, nil
	}
	return strings.TrimSpace(param.DoorName), true, nil
}

func (m *Manager) OpenDoor(ctx context.Context, door dto.DoorDTO) error {
	return m.control(ctx, door, "open")
}

func (m *Manager) CloseDoor(ctx context.Context, door dto.DoorDTO) error {
	return m.control(ctx, door, "close")
}

func (m *Manager) control(ctx context.Context, door dto.DoorDTO, command string) error {
	if door.DoorIndex == nil || *door.DoorIndex <= 0 {
		return serviceManager.NewServiceError(serviceManager.ErrorInvalidRequest, "Hikvision door index is required", nil)
	}
	tlsEnabled, _ := metadataBool(door.ProtocolMetadata, "tls")
	verifyTLS, _ := metadataBool(door.ProtocolMetadata, "verifyTls")
	endpoint := baseURL(tlsEnabled, door.IPAddress, door.Port) + remoteControlPath(*door.DoorIndex)
	body := []byte(`<RemoteControlDoor xmlns="http://www.hikvision.com/ver20/XMLSchema"><cmd>` + command + `</cmd></RemoteControlDoor>`)
	_, status, err := m.client.request(
		ctx, http.MethodPut, endpoint, door.Username, door.Password, verifyTLS, body,
	)
	if err != nil {
		return mapClientError(err)
	}
	return statusError(status)
}

func metadataBool(metadata map[string]any, key string) (bool, bool) {
	if metadata == nil {
		return false, false
	}
	value, ok := metadata[key].(bool)
	return value, ok
}

func statusError(status int) error {
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		return nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return serviceManager.NewServiceError(serviceManager.ErrorAuthFailed, "Hikvision authentication failed", nil)
	default:
		return serviceManager.NewServiceError(serviceManager.ErrorCommandRejected, fmt.Sprintf("Hikvision status %d", status), nil)
	}
}

func mapClientError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return serviceManager.NewServiceError(serviceManager.ErrorCommandTimeout, "Hikvision request timed out", err)
	}
	return serviceManager.NewServiceError(serviceManager.ErrorDeviceUnreachable, "Hikvision device is unreachable", err)
}

// --- ISAPI XML Response Structures ---

type ISAPIDeviceInfo struct {
	XMLName xml.Name `xml:"DeviceInfo"`
	Model  string   `xml:"model"`
	SerialNumber string `xml:"serialNumber"`
	FirmwareVersion string `xml:"firmwareVersion"`
	MacAddress string `xml:"macAddress"`
	DeviceType string `xml:"deviceType"`
}

type ISAPIDoorParam struct {
	XMLName  xml.Name `xml:"DoorParam"`
	DoorName string   `xml:"doorName"`
}
