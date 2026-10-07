package zktecomanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"doorservice/dto"
	serviceManager "doorservice/manager"
)

// ZKBio CVSecurity 3rd Party API
// Base URL: http://serverIP:serverPort/api
// Auth: access_token in query params (NOT Basic Auth)
const (
	zktecoBasePath     = "/api"
	defaultServerPort  = 8088
	tokenCacheTTL      = 30 * time.Minute
)

type Manager struct {
	client    *http.Client
	tokens    map[string]*tokenInfo // key: serverIP:serverPort
}

type tokenInfo struct {
	token     string
	createdAt time.Time
}

func New(httpClient *http.Client) *Manager {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Manager{
		client: httpClient,
		tokens: make(map[string]*tokenInfo),
	}
}

// Connect - Get device info from CVSecurity server
// Endpoint: GET /api/device/getAcc/{sn}
func (m *Manager) Connect(ctx context.Context, controller dto.AccessControllerDTO) (dto.AccessControllerDTO, error) {
	if controller.Port == 0 {
		controller.Port = defaultServerPort
	}

	// Get access_token (apiToken)
	token, err := m.getAccessToken(ctx, controller)
	if err != nil {
		return dto.AccessControllerDTO{}, err
	}

	// Get device info by serial number
	if controller.SerialNumber == "" {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInvalidRequest, "ZKTeco device serial number is required", nil)
	}

	baseURL := fmt.Sprintf("http://%s:%d%s", controller.IPAddress, controller.Port, zktecoBasePath)
	deviceURL := fmt.Sprintf("%s/device/getAcc/%s?access_token=%s",
		baseURL, controller.SerialNumber, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deviceURL, nil)
	if err != nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to create request", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return dto.AccessControllerDTO{}, mapClientError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to read response body", err)
	}

	var apiResp ZKApiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse device info", err)
	}

	if apiResp.Code != 0 {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorCommandRejected, apiResp.Message, nil)
	}

	deviceData, _ := json.Marshal(apiResp.Data)
	var deviceInfo ZKDeviceInfo
	if err := json.Unmarshal(deviceData, &deviceInfo); err != nil {
		return dto.AccessControllerDTO{}, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse device info data", err)
	}

	controller.Type = "ZKTECO"
	controller.Manufacturer = "ZKTeco"
	controller.SerialNumber = strings.TrimSpace(deviceInfo.SN)
	controller.DeviceModel = strings.TrimSpace(deviceInfo.Type)
	controller.DeviceType = strings.TrimSpace(deviceInfo.Module)
	controller.State = "CONNECTED"
	controller.Activate = true

	return controller, nil
}

// DiscoverDoors - Get door list from CVSecurity server
// Endpoint: GET /api/door/list?pageNo=1&pageSize=20
func (m *Manager) DiscoverDoors(ctx context.Context, controller dto.AccessControllerDTO) ([]dto.DoorDTO, error) {
	if controller.Port == 0 {
		controller.Port = defaultServerPort
	}

	token, err := m.getAccessToken(ctx, controller)
	if err != nil {
		return nil, err
	}

	baseURL := fmt.Sprintf("http://%s:%d%s", controller.IPAddress, controller.Port, zktecoBasePath)
	listURL := fmt.Sprintf("%s/door/list?pageNo=1&pageSize=100&access_token=%s",
		baseURL, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to create request", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, mapClientError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to read response body", err)
	}

	var apiResp ZKApiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse door list", err)
	}

	if apiResp.Code != 0 {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorCommandRejected, apiResp.Message, nil)
	}

	dataBytes, _ := json.Marshal(apiResp.Data)
	var doorResponse ZKDoorListResponse
	if err := json.Unmarshal(dataBytes, &doorResponse); err != nil {
		return nil, serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse door list data", err)
	}

	doors := make([]dto.DoorDTO, 0, len(doorResponse.Data))
	for _, zkDoor := range doorResponse.Data {
		doors = append(doors, dto.DoorDTO{
			ID:                 zkDoor.ID,
			DoorType:           "ZKTECO",
			Name:               zkDoor.Name,
			BrandName:          "ZKTeco",
			Username:           controller.Username,
			Password:           controller.Password,
			IPAddress:          controller.IPAddress,
			Port:               controller.Port,
			Activate:           true,
			AccessControllerID: zkDoor.DeviceID,
			DoorIndex:          extractDoorIndex(zkDoor.Name),
			AIBoxID:            controller.AIBoxID,
			PartnerID:          controller.PartnerID,
		})
	}

	return doors, nil
}

// OpenDoor - Remote open door by door ID
// Endpoint: POST /api/door/remoteOpenById?doorId={doorId}&interval=5
func (m *Manager) OpenDoor(ctx context.Context, door dto.DoorDTO) error {
	return m.remoteControl(ctx, door, "remoteOpenById")
}

// CloseDoor - Remote close door by door ID
// Endpoint: POST /api/door/remoteCloseById?doorId={doorId}
func (m *Manager) CloseDoor(ctx context.Context, door dto.DoorDTO) error {
	return m.remoteControl(ctx, door, "remoteCloseById")
}

func (m *Manager) remoteControl(ctx context.Context, door dto.DoorDTO, action string) error {
	if door.ID == "" {
		return serviceManager.NewServiceError(
			serviceManager.ErrorInvalidRequest, "ZKTeco door ID is required", nil)
	}

	controller := dto.AccessControllerDTO{
		IPAddress: door.IPAddress,
		Port:      door.Port,
		Username:  door.Username,
		Password:  door.Password,
	}
	token, err := m.getAccessToken(ctx, controller)
	if err != nil {
		return err
	}

	baseURL := fmt.Sprintf("http://%s:%d%s", controller.IPAddress, controller.Port, zktecoBasePath)
	var ctrlURL string
	if action == "remoteOpenById" {
		ctrlURL = fmt.Sprintf("%s/door/%s?doorId=%s&interval=5&access_token=%s",
			baseURL, action, url.QueryEscape(door.ID), token)
	} else {
		ctrlURL = fmt.Sprintf("%s/door/%s?doorId=%s&access_token=%s",
			baseURL, action, url.QueryEscape(door.ID), token)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ctrlURL, nil)
	if err != nil {
		return serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to create request", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return mapClientError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to read response body", err)
	}

	var apiResp ZKApiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse response", err)
	}

	if apiResp.Code != 0 {
		return serviceManager.NewServiceError(
			serviceManager.ErrorCommandRejected, apiResp.Message, nil)
	}

	return nil
}

// getAccessToken - Get or cache the API access token (apiToken)
// According to docs: "access_token: API access token is to check whether the requested permission is allowed or denied"
// Token is created in: System > Authority Management > API Authorization > Client Secret
// We pass ClientId + ClientSecret to get the token
func (m *Manager) getAccessToken(ctx context.Context, controller dto.AccessControllerDTO) (string, error) {
	key := fmt.Sprintf("%s:%d", controller.IPAddress, controller.Port)

	// Check cache
	if info, ok := m.tokens[key]; ok {
		if time.Since(info.createdAt) < tokenCacheTTL {
			return info.token, nil
		}
	}

	// Token endpoint: GET /api/auth/token?clientId={clientId}&clientSecret={clientSecret}
	// OR we use the username/password as client credentials
	baseURL := fmt.Sprintf("http://%s:%d%s", controller.IPAddress, controller.Port, zktecoBasePath)
	tokenURL := fmt.Sprintf("%s/auth/token?clientId=%s&clientSecret=%s",
		baseURL, url.QueryEscape(controller.Username), url.QueryEscape(controller.Password))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to create token request", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", mapClientError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to read token response", err)
	}

	var apiResp ZKApiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse token response", err)
	}

	if apiResp.Code != 0 {
		return "", serviceManager.NewServiceError(
			serviceManager.ErrorAuthFailed, "ZKTeco authentication failed: "+apiResp.Message, nil)
	}

	tokenData, _ := json.Marshal(apiResp.Data)
	var tokenResp ZKTokenResponse
	if err := json.Unmarshal(tokenData, &tokenResp); err != nil {
		return "", serviceManager.NewServiceError(
			serviceManager.ErrorInternal, "failed to parse token data", err)
	}

	// Cache token
	m.tokens[key] = &tokenInfo{
		token:     tokenResp.AccessToken,
		createdAt: time.Now(),
	}

	return tokenResp.AccessToken, nil
}

func extractDoorIndex(name string) *int {
	// Door name format: "192.168.218.11-1" → index 1
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return nil
	}
	idx, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return nil
	}
	return &idx
}

func mapClientError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return serviceManager.NewServiceError(
			serviceManager.ErrorCommandTimeout, "ZKTeco request timed out", err)
	}
	return serviceManager.NewServiceError(
		serviceManager.ErrorDeviceUnreachable, "ZKTeco server is unreachable", err)
}

// --- ZKBio CVSecurity API Models ---

// ZKApiResponse is the public response wrapper
type ZKApiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type ZKTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type ZKDeviceInfo struct {
	ID     string `json:"id"`
	SN     string `json:"sn"`
	Name   string `json:"name"`
	Type   string `json:"type"`   // device model name
	State  string `json:"state"`  // 1 enabled, 0 disabled
	Module string `json:"module"` // module: access, attendance, elevator
}

type ZKDoorListResponse struct {
	Data []ZKDoorInfo `json:"data"`
}

type ZKDoorInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DeviceID string `json:"deviceId"`
}

// request helper for body-based requests
func newJSONRequest(ctx context.Context, method, url string, body interface{}) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
