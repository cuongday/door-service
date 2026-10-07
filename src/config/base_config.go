package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"commonkit/util/httputil"
	"commonkit/util/jsonutil"
	"commonkit/util/netutil"
	"doorservice/api"
	"doorservice/auth"
	"doorservice/db"
	"doorservice/websocket"
	"go.uber.org/zap"
)

const (
	ServiceName = "DOOR_SERVICE"
	ServiceType = "DOOR"
)

type ConfigInterface interface {
	Start(context.Context) error
	GetConfig() *BaseConfig
	update()
	waitForConfig()
	configureRegistrationAuth()
	onServiceRegisteredCallback(*db.ServiceModel)
}

type BaseConfig struct {
	ConfigInterface     ConfigInterface
	ServiceID           string
	MacAddress          string
	IPAddress           string
	IsBox               bool
	APIURL              string
	WSURL               string
	VmsAPIURL           string
	VmsWSURL            string
	RbmqUsername        string
	RbmqPassword        string
	RbmqHost            string
	RbmqPort            int
	RbmqVirtualHost     string
	RbmqQueuePrefix     string
	RbmqQueuePostfix    string
	DatabasePath        string
	StateDir            string
	Signing             auth.M2MSigning
	SigningPrivateKey   string
	SigningPublicKeyPEM string
	RetryDelay          time.Duration
	API                 *api.Api
	Websocket           *websocket.Client
	ConfigJSON          ConfigJSON
	ServiceRepo         *db.ServiceRepo
	logger              *zap.Logger
	// Box-specific fields
	PartnerID string
	BoxToken  string
	AIBoxID   string
}

type ConfigJSON struct {
	IsBox           bool   `json:"isBox"`
	APIHost         string `json:"apiHost"`
	APIPort         int    `json:"apiPort"`
	APISecure       bool   `json:"apiSecure"`
	BootstrapSecret string `json:"bootstrapSecret"`
}

func (c *BaseConfig) update() {
	// Default implementation - overridden by BoxConfig/ServerConfig
}

func (c *BaseConfig) waitForConfig() {
	// Default implementation - overridden by BoxConfig
}

func (c *BaseConfig) configureRegistrationAuth() {
	// Default implementation - overridden by BoxConfig/ServerConfig
}

func (c *BaseConfig) Start(ctx context.Context) error {
	c.ConfigInterface.update()
	c.waitForConfig()
	c.registerService(ctx)
	return nil
}

func (c *BaseConfig) registerService(ctx context.Context) {
	c.ConfigInterface.configureRegistrationAuth()
	for {
		service, _ := c.ServiceRepo.FindFirst()
		if service != nil {
			if err := c.ensureServicePublicKey(service); err != nil {
				c.logger.Error("Failed to bind service public key", zap.Error(err))
			} else if err := c.applyService(service); err == nil {
				c.logger.Info("Reusing registered service", zap.String("serviceId", service.ID))
				c.onServiceRegisteredCallback(service)
				return
			}
		} else {
			if c.createAndSaveService() {
				svc, _ := c.ServiceRepo.FindFirst()
				if svc != nil {
					c.onServiceRegisteredCallback(svc)
				}
				return
			}
		}

		c.logger.Info("Retry registering service")
		time.Sleep(c.RetryDelay)
	}
}

func (c *BaseConfig) readService(id string) (*db.ServiceModel, error) {
	resp, err := c.API.ReadService(id)
	if err != nil {
		c.logger.Error("Failed to read service", zap.Error(err))
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		c.logger.Info("Service not found on VMS, will create new")
		return nil, nil
	}

	if resp.StatusCode != 200 {
		c.logger.Error("Failed to read service", zap.Int("status", resp.StatusCode))
		return nil, fmt.Errorf("read service failed: %d", resp.StatusCode)
	}

	body, err := httputil.ReadResponseBody(resp)
	if err != nil {
		c.logger.Error("Failed to read response body", zap.Error(err))
		return nil, err
	}

	result, err := jsonutil.Unmarshal[struct {
		Data []db.ServiceModel `json:"data"`
	}](body)
	if err != nil {
		c.logger.Error("Failed to parse service response", zap.Error(err))
		return nil, err
	}

	if result != nil && len(result.Data) > 0 {
		return &result.Data[0], nil
	}
	return nil, nil
}

func (c *BaseConfig) createAndSaveService() bool {
	c.ConfigInterface.configureRegistrationAuth()

	payload := map[string]interface{}{
		"name":         ServiceName,
		"type":         ServiceType,
		"hostName":     netutil.GetHostName(),
		"ipAddress":    c.IPAddress,
		"macAddress":   c.MacAddress,
		"heartbeat":    time.Now(),
		"publicKeyPem": c.SigningPublicKeyPEM,
	}

	resp, err := c.API.CreateService(payload)
	if err != nil {
		c.logger.Error("Failed to create service", zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		c.logger.Error("Create service failed", zap.Int("status", resp.StatusCode))
		return false
	}

	body, err := httputil.ReadResponseBody(resp)
	if err != nil {
		c.logger.Error("Failed to read response body", zap.Error(err))
		return false
	}

	created, err := jsonutil.Unmarshal[db.ServiceModel](body)
	if err != nil {
		c.logger.Error("Failed to parse create service response", zap.Error(err))
		return false
	}

	if created != nil {
		if err := c.applyService(created); err != nil {
			c.logger.Error("Failed to save service", zap.Error(err))
			return false
		}
	}

	c.logger.Info("Service registered successfully")
	return true
}

func (c *BaseConfig) ensureServicePublicKey(service *db.ServiceModel) error {
	if service == nil || strings.TrimSpace(service.ID) == "" {
		return fmt.Errorf("service id is required")
	}
	publicKeyPEM := strings.TrimSpace(c.SigningPublicKeyPEM)
	if publicKeyPEM == "" {
		return fmt.Errorf("signing public key is required")
	}
	if strings.TrimSpace(service.PublicKeyPem) == publicKeyPEM {
		return nil
	}

	c.ConfigInterface.configureRegistrationAuth()
	resp, err := c.API.UpdateFieldService(map[string]interface{}{
		"id":           service.ID,
		"type":         ServiceType,
		"publicKeyPem": publicKeyPEM,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := httputil.ReadResponseBody(resp)
		return fmt.Errorf("update service public key failed: %d %s", resp.StatusCode, string(body))
	}
	service.PublicKeyPem = publicKeyPEM
	return nil
}

func (c *BaseConfig) applyService(service *db.ServiceModel) error {
	c.ServiceID = service.ID
	c.RbmqUsername = service.RbmqUsername
	c.RbmqPassword = service.RbmqPassword
	c.RbmqVirtualHost = service.RbmqVirtualHost
	c.RbmqQueuePrefix = fmt.Sprintf("%s_%s", service.Type, service.ID)
	c.RbmqQueuePostfix = service.RbmqQueuePostfix

	if c.IsBox {
		c.RbmqHost = service.RbmqPublicHost
		c.RbmqPort = service.RbmqPublicPort
	} else {
		c.RbmqHost = service.RbmqPrivateHost
		c.RbmqPort = service.RbmqPrivatePort
	}

	_, err := c.ServiceRepo.Save(*service)
	return err
}

func (c *BaseConfig) onServiceRegisteredCallback(service *db.ServiceModel) {
	if c.ServiceID != "" && c.VmsAPIURL != "" {
		sessionURL := fmt.Sprintf("%s/api/m2m/session/open", c.VmsAPIURL)
		c.API.ConfigureRuntimeAuth(&sessionURL, &c.ServiceID, &c.SigningPrivateKey)
	}

	if c.Websocket != nil {
		wsURL := fmt.Sprintf("%s/api/m2m/service/%s", c.VmsWSURL, c.ServiceID)
		c.Websocket.SetURL(wsURL)
	}
}

func (c *BaseConfig) GetConfig() *BaseConfig {
	return c
}
