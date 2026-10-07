package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"commonkit/const/mime"
	"commonkit/util/httputil"
	"commonkit/util/jsonutil"
	"doorservice/auth"
	"doorservice/dto"
)

type Api struct {
	client    *http.Client
	apiUrl    string
	transport *auth.AuthTransport
}

func New(baseURL string, tokenMiddleware *auth.AuthTransport) *Api {
	if tokenMiddleware == nil {
		tokenMiddleware = auth.GetAuthTransport()
	}
	api := &Api{
		apiUrl:    strings.TrimRight(baseURL, "/"),
		transport: tokenMiddleware,
	}
	api.client = &http.Client{
		Transport: api.transport,
	}
	return api
}

func (api *Api) SetUrl(url string) {
	api.apiUrl = strings.TrimRight(url, "/")
}

func (api *Api) ConfigureRegistrationAuth(token, tokenKey *string) {
	api.transport.ConfigureRegistrationAuth(token, tokenKey)
}

func (api *Api) ConfigureRuntimeAuth(sessionURL, serviceID, signingPrivateKey *string) {
	api.transport.ConfigureRuntimeAuth(sessionURL, serviceID, signingPrivateKey)
}

type SyncAiBoxGroupRequest struct {
	ServiceID string `json:"serviceId"`
	AIBoxId   string `json:"aiBoxId"`
}

func (api *Api) SyncAiBoxGroup(serviceID, aiBoxId string) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/api/m2m/aibox-group/sync", api.apiUrl)
	payload := SyncAiBoxGroupRequest{
		ServiceID: serviceID,
		AIBoxId:   aiBoxId,
	}
	jsonData, _ := jsonutil.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mime.ContentTypeJSON)
	return api.client.Do(req)
}

func (api *Api) ReadService(id string) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/api/m2m/service", api.apiUrl)
	params := url.Values{}
	params.Set("ids", id)
	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	return api.client.Do(req)
}

func (api *Api) CreateService(service map[string]interface{}) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/api/m2m/service", api.apiUrl)
	jsonData, _ := jsonutil.Marshal(service)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mime.ContentTypeJSON)
	return api.client.Do(req)
}

func (api *Api) UpdateFieldService(service map[string]interface{}) (*http.Response, error) {
	endpoint := fmt.Sprintf("%s/api/m2m/service", api.apiUrl)
	jsonData, _ := jsonutil.Marshal(service)
	req, err := http.NewRequest(http.MethodPatch, endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mime.ContentTypeJSON)
	return api.client.Do(req)
}

func (api *Api) ReadServiceData(id string) (map[string]interface{}, error) {
	resp, err := api.ReadService(id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := httputil.ReadResponseBody(resp)
	if err != nil {
		return nil, err
	}

	result, err := jsonutil.Unmarshal[struct {
		Data []map[string]interface{} `json:"data"`
	}](body)
	if err != nil {
		return nil, err
	}

	if result != nil && len(result.Data) > 0 {
		return result.Data[0], nil
	}
	return nil, nil
}

// DoorServiceData represents the service data from VMS
type DoorServiceData struct {
	AccessControllers []dto.AccessControllerDTO `json:"accessControllers"`
	Doors             []dto.DoorDTO             `json:"doors"`
}

// ReadDoorServiceData fetches door service data from VMS-BE
func (api *Api) ReadDoorServiceData(id string) (*DoorServiceData, error) {
	endpoint := fmt.Sprintf("%s/api/m2m/service/data", api.apiUrl)
	params := url.Values{}
	params.Set("id", id)
	params.Set("type", "DOOR")
	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to read door service data: %d", resp.StatusCode)
	}

	body, err := httputil.ReadResponseBody(resp)
	if err != nil {
		return nil, err
	}

	var data DoorServiceData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	return &data, nil
}
