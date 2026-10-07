package config

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"commonkit/util/httputil"
	"commonkit/util/jsonutil"
	"doorservice/db"

	"go.uber.org/zap"
)

const (
	defaultLocalBoxConfigURL  = "http://127.0.0.1:80/api/internal/m2m/config"
	localConfigRetry          = 5 * time.Second
	localConfigPollInterval   = 2 * time.Second
	localServiceIDHeader      = "X-AIBOX-Service-ID"
	provisioningStatePaired   = "paired"
	provisioningStateReset    = "reset"
	provisioningStateUnpaired = "unpaired"
)

var (
	boxConfigInstance *BoxConfig
	boxConfigOnce     sync.Once
)

type BoxConfig struct {
	*BaseConfig
	configData       string
	localConfigURL   string
	localResetAckURL string
	pairedReady      chan struct{}
	pairedOnce       sync.Once
	pollOnce         sync.Once
}

type loopbackConfig struct {
	State               string `json:"state"`
	ResetID             string `json:"reset_id"`
	ApiURL              string `json:"api_url"`
	PartnerID           string `json:"partner_id"`
	BoxToken            string `json:"box_token"`
	AIBoxID             string `json:"ai_box_id"`
	LegacyCameraGroupID string `json:"camera_group_id"`
}

func newBoxConfigWithBase(base *BaseConfig) *BoxConfig {
	boxConfigOnce.Do(func() {
		boxConfigInstance = &BoxConfig{
			BaseConfig:       base,
			localConfigURL:   defaultLocalBoxConfigURL,
			localResetAckURL: strings.Replace(defaultLocalBoxConfigURL, "/config", "/reset/ack", 1),
			pairedReady:      make(chan struct{}),
		}
		boxConfigInstance.ConfigInterface = boxConfigInstance
		boxConfigInstance.startLocalConfigPoll()
	})
	return boxConfigInstance
}

func (c *BoxConfig) update() {
	data := c.readLocalConfig()
	dataValue := data

	if data.State == provisioningStateReset || data.State == provisioningStateUnpaired {
		c.handleReset(strings.TrimSpace(data.ResetID))
		return
	}

	if data.State == "" || data.State == provisioningStatePaired {
		if c.applyLoopbackConfig(&dataValue) {
			c.pairedOnce.Do(func() {
				close(c.pairedReady)
			})
		}
	}
}

func (c *BoxConfig) applyLoopbackConfig(data *loopbackConfig) bool {
	if data == nil {
		return false
	}

	dataBytes, _ := jsonutil.Marshal(data)
	dataStr := string(dataBytes)

	if c.configData == dataStr {
		return true
	}

	if strings.TrimSpace(data.ApiURL) == "" {
		c.logger.Warn("Skip local config update because api_url is missing")
		return false
	}

	if strings.TrimSpace(data.BoxToken) == "" {
		c.logger.Warn("Skip local config update because box_token is missing")
		return false
	}

	aiBoxID := strings.TrimSpace(data.AIBoxID)
	if aiBoxID == "" {
		aiBoxID = strings.TrimSpace(data.LegacyCameraGroupID)
	}

	c.BoxToken = strings.TrimSpace(data.BoxToken)
	c.AIBoxID = aiBoxID
	c.PartnerID = strings.TrimSpace(data.PartnerID)
	c.setApiUrl(strings.TrimSpace(data.ApiURL))
	c.API.SetUrl(c.APIURL)
	c.configData = dataStr

	return true
}

func (c *BoxConfig) setApiUrl(apiUrl string) {
	c.APIURL = strings.TrimRight(apiUrl, "/")
	c.WSURL = strings.Replace(c.APIURL, "http", "ws", 1)
	c.VmsAPIURL = c.APIURL
	c.VmsWSURL = c.WSURL
}

func (c *BoxConfig) Start(ctx context.Context) error {
	return c.BaseConfig.Start(ctx)
}

func (c *BoxConfig) configureRegistrationAuth() {
	c.API.SetUrl(c.APIURL)
	boxToken := strings.TrimSpace(c.BoxToken)
	if boxToken == "" {
		c.API.ConfigureRegistrationAuth(nil, nil)
		return
	}
	tokenKey := "X-Box-Token"
	c.API.ConfigureRegistrationAuth(&boxToken, &tokenKey)
}

func (c *BoxConfig) onServiceRegisteredCallback(service *db.ServiceModel) {
	c.PartnerID = strings.TrimSpace(service.PartnerID)
	c.RbmqHost = service.RbmqPublicHost
	c.RbmqPort = service.RbmqPublicPort
	c.syncAiBoxGroup()
}

func (c *BoxConfig) readLocalConfig() loopbackConfig {
	for {
		req, err := http.NewRequest(http.MethodGet, c.localConfigURL, nil)
		if err != nil {
			c.logger.Error("Failed to create local provisioning config request", zap.Error(err))
			time.Sleep(localConfigRetry)
			continue
		}

		if strings.TrimSpace(c.ServiceID) != "" {
			req.Header.Set(localServiceIDHeader, strings.TrimSpace(c.ServiceID))
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.logger.Error("Failed to read local provisioning config", zap.Error(err))
			time.Sleep(localConfigRetry)
			continue
		}

		cfg, err := httputil.UnmarshalResponseBody[loopbackConfig](resp)
		if err != nil {
			c.logger.Error("Failed to parse local provisioning config", zap.Error(err))
			time.Sleep(localConfigRetry)
			continue
		}

		if cfg != nil {
			return *cfg
		}
		time.Sleep(localConfigRetry)
	}
}

func (c *BoxConfig) cleanup() {
	_ = os.Remove(c.DatabasePath)
	_ = os.Remove(c.Signing.PublicKeyPath())
}

func (c *BoxConfig) ackReset(resetID string) error {
	if strings.TrimSpace(resetID) == "" {
		return nil
	}
	if strings.TrimSpace(c.ServiceID) == "" {
		return fmt.Errorf("reset ack failed because service id is missing")
	}

	body := []byte(fmt.Sprintf(`{"reset_id":%q}`, resetID))
	req, err := http.NewRequest(http.MethodPost, c.localResetAckURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(localServiceIDHeader, strings.TrimSpace(c.ServiceID))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := httputil.ReadResponseBody(resp)
		return fmt.Errorf("reset ack failed with status code %d %s", resp.StatusCode, string(payload))
	}
	return nil
}

func (c *BoxConfig) handleReset(resetID string) {
	if strings.TrimSpace(c.ServiceID) == "" {
		return
	}
	c.cleanup()
	if strings.TrimSpace(resetID) != "" {
		if err := c.ackReset(resetID); err != nil {
			c.logger.Error("Failed to acknowledge reset", zap.Error(err))
			time.Sleep(localConfigRetry)
			return
		}
	}
	os.Exit(0)
}

func (c *BoxConfig) waitForConfig() {
	for {
		select {
		case <-c.pairedReady:
			return
		case <-time.After(localConfigRetry):
			c.logger.Info("Waiting for local provisioning config to reach paired state")
		}
	}
}

func (c *BoxConfig) startLocalConfigPoll() {
	c.pollOnce.Do(func() {
		go func() {
			for {
				time.Sleep(localConfigPollInterval)
				data := c.readLocalConfig()
				state := data.State

				if state == provisioningStateReset || state == provisioningStateUnpaired {
					c.handleReset(strings.TrimSpace(data.ResetID))
					continue
				}

				if (state == "" || state == provisioningStatePaired) && c.applyLoopbackConfig(&data) {
					c.pairedOnce.Do(func() {
						close(c.pairedReady)
					})
				}
			}
		}()
	})
}

func (c *BoxConfig) syncAiBoxGroup() {
	if c.ServiceID == "" || c.AIBoxID == "" {
		return
	}

	for {
		resp, err := c.API.SyncAiBoxGroup(c.ServiceID, c.AIBoxID)
		if err != nil {
			c.logger.Error("Failed to sync AI box group", zap.Error(err))
			time.Sleep(localConfigRetry)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return
		}
		c.logger.Error("Sync AI box group failed", zap.Int("status_code", resp.StatusCode))
		time.Sleep(localConfigRetry)
	}
}
