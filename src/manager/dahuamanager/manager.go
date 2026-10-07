package dahuamanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"commonkit/util/httputil"
	"doorservice/dto"
	serviceManager "doorservice/manager"
)

type Manager struct {
	client   *http.Client
	sessions sync.Map // controllerIP -> *session
}

type session struct {
	cookie    *http.Cookie
	createdAt time.Time
}

func New(httpClient *http.Client) *Manager {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15e9}
	}
	// Create a client with cookie jar for session management
	jar, _ := cookiejar.New(nil)
	httpClient.Jar = jar

	return &Manager{client: httpClient}
}

func (m *Manager) Connect(ctx context.Context, controller dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	baseURL := fmt.Sprintf("http://%s:%d", controller.IPAddress, controller.Port)

	// Step 1: Login to get session
	loginURL := baseURL + "/cgi-bin/accessControl.cgi"
	resp, err := m.client.PostForm(loginURL+"?action=login",
		url.Values{
			"userName": {controller.Username},
			"password": {controller.Password},
		})
	if err != nil {
		return dto.AccessControllerDTO{}, mapClientError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorAuthFailed, "Dahua login failed", nil)
	}

	// Get session cookie
	var sessionCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SESSION" || cookie.Name == "session" || cookie.Name == "DahuaSession" {
			sessionCookie = cookie
			break
		}
	}

	if sessionCookie == nil && len(resp.Cookies()) > 0 {
		sessionCookie = resp.Cookies()[0]
	}

	if sessionCookie == nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorAuthFailed, "Dahua no session cookie received", nil)
	}

	// Store session
	m.sessions.Store(controller.IPAddress, &session{
		cookie:    sessionCookie,
		createdAt: time.Now(),
	})

	// Step 2: Get device info
	infoURL := baseURL + "/cgi-bin/devVideo.cgi?action=getDeviceType"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to create request", err)
	}
	m.addCookie(req, sessionCookie)

	resp, err = m.client.Do(req)
	if err != nil {
		return dto.AccessControllerDTO{}, mapClientError(err)
	}
	defer resp.Body.Close()

	// Parse device info using httputil
	body, err := httputil.ReadResponseBody(resp)
	if err != nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to read device info response", err)
	}

	deviceType := parseDahuaValue(body, "DeviceType")

	controller.Type = "DAHUA"
	controller.Manufacturer = "Dahua"
	controller.DeviceModel = strings.TrimSpace(deviceType)
	controller.State = "CONNECTED"
	controller.Activate = true

	return controller, nil
}

func (m *Manager) DiscoverDoors(ctx context.Context, controller dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	baseURL := fmt.Sprintf("http://%s:%d", controller.IPAddress, controller.Port)

	sess, ok := m.sessions.Load(controller.IPAddress)
	if !ok {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorControllerNotFound, "Dahua no session found, please connect first", nil)
	}
	session := sess.(*session)

	// Get door list
	listURL := baseURL + "/cgi-bin/accessControl.cgi?action=getDoorList"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	m.addCookie(req, session.cookie)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, mapClientError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorCommandRejected,
			fmt.Sprintf("Dahua door discovery failed: %d", resp.StatusCode), nil)
	}

	body, err := httputil.ReadResponseBody(resp)
	if err != nil {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to read door list response", err)
	}

	doors, _ := parseDoorList(body, controller)
	return doors, nil
}

func (m *Manager) OpenDoor(ctx context.Context, door dto.DoorDTO) error {
	return m.controlDoor(ctx, door, "openDoor")
}

func (m *Manager) CloseDoor(ctx context.Context, door dto.DoorDTO) error {
	return m.controlDoor(ctx, door, "closeDoor")
}

func (m *Manager) controlDoor(ctx context.Context, door dto.DoorDTO, action string) error {
	if door.DoorIndex == nil || *door.DoorIndex <= 0 {
		return serviceManager.NewServiceError(
			serviceManager.ErrorInvalidRequest, "Dahua door index is required", nil)
	}

	sess, ok := m.sessions.Load(door.IPAddress)
	if !ok {
		return serviceManager.NewServiceError(
			serviceManager.ErrorControllerNotFound, "Dahua no session found", nil)
	}
	session := sess.(*session)

	baseURL := fmt.Sprintf("http://%s:%d", door.IPAddress, door.Port)
	ctrlURL := fmt.Sprintf("%s/cgi-bin/accessControl.cgi?action=%s&doorID=%d",
		baseURL, action, *door.DoorIndex)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ctrlURL, nil)
	if err != nil {
		return serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to create request", err)
	}
	m.addCookie(req, session.cookie)

	resp, err := m.client.Do(req)
	if err != nil {
		return mapClientError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return serviceManager.NewServiceError(
			serviceManager.ErrorCommandRejected,
			fmt.Sprintf("Dahua control failed: %d", resp.StatusCode), nil)
	}

	return nil
}

func (m *Manager) addCookie(req *http.Request, cookie *http.Cookie) {
	req.AddCookie(cookie)
}

func parseDahuaValue(body []byte, key string) string {
	content := string(body)
	// Dahua returns: key=value\r\n
	prefix := key + "="
	if idx := strings.Index(content, prefix); idx >= 0 {
		start := idx + len(prefix)
		end := start
		for end < len(content) && content[end] != '\r' && content[end] != '\n' {
			end++
		}
		return strings.TrimSpace(content[start:end])
	}
	return ""
}

type dahuaDoorInfo struct {
	doorID   int
	doorName string
}

func parseDoorList(body []byte, controller dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	content := string(body)
	doors := make([]dto.DoorDTO, 0)

	// Dahua returns format like:
	// door[0].doorID=1&door[0].name=Main Door&door[1].doorID=2&...
	// or: total=2&door[0].doorID=1&door[0].name=Front Door...

	// Simple parsing - look for door[N].doorID patterns
	lines := strings.Split(content, "&")
	for _, line := range lines {
		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Look for door[N].doorID
		if strings.Contains(key, ".doorID") {
			doorID, err := strconv.Atoi(value)
			if err != nil {
				continue
			}

			// Look for corresponding door name
			nameKey := strings.Replace(key, ".doorID", ".name", 1)
			doorName := getValueForKey(lines, nameKey)

			idPrefix := controller.ID
			if idPrefix == "" {
				idPrefix = controller.IPAddress
			}

			doors = append(doors, dto.DoorDTO{
				ID:                 fmt.Sprintf("%s:%d", idPrefix, doorID),
				DoorType:          "DAHUA",
				Name:              doorName,
				BrandName:         "Dahua",
				Username:          controller.Username,
				Password:          controller.Password,
				IPAddress:         controller.IPAddress,
				Port:              controller.Port,
				Activate:          true,
				AccessControllerID: controller.ID,
				DoorIndex:         &doorID,
				AIBoxID:           controller.AIBoxID,
				PartnerID:         controller.PartnerID,
			})
		}
	}

	return doors, nil
}

func getValueForKey(lines []string, targetKey string) string {
	for _, line := range lines {
		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == targetKey {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func mapClientError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return serviceManager.NewServiceError(
			serviceManager.ErrorCommandTimeout, "Dahua request timed out", err)
	}
	return serviceManager.NewServiceError(
		serviceManager.ErrorDeviceUnreachable, "Dahua device is unreachable", err)
}
