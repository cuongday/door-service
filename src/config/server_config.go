package config

import (
	"context"
	"fmt"
	"strings"

	"doorservice/db"
)

type ServerConfig struct {
	*BaseConfig
}

func newServerConfigWithBase(base *BaseConfig) *ServerConfig {
	config := &ServerConfig{BaseConfig: base}
	config.ConfigInterface = config
	return config
}

func (c *ServerConfig) update() {
	scheme := "http"
	if c.ConfigJSON.APISecure {
		scheme = "https"
	}
	c.APIURL = fmt.Sprintf("%s://%s:%d", scheme, c.ConfigJSON.APIHost, c.ConfigJSON.APIPort)
	c.WSURL = strings.Replace(c.APIURL, "http", "ws", 1)
	c.VmsAPIURL = c.APIURL
	c.VmsWSURL = c.WSURL
	c.API.SetUrl(c.VmsAPIURL)
}

func (c *ServerConfig) Start(ctx context.Context) error {
	return c.BaseConfig.Start(ctx)
}

func (c *ServerConfig) configureRegistrationAuth() {
	bootstrapSecret := strings.TrimSpace(c.ConfigJSON.BootstrapSecret)
	if bootstrapSecret == "" {
		c.API.ConfigureRegistrationAuth(nil, nil)
		return
	}
	tokenKey := "X-M2M-Bootstrap-Secret"
	c.API.ConfigureRegistrationAuth(&bootstrapSecret, &tokenKey)
}

func (c *ServerConfig) onServiceRegisteredCallback(service *db.ServiceModel) {
	if c.ServiceID != "" && c.VmsAPIURL != "" {
		sessionURL := fmt.Sprintf("%s/api/m2m/session/open", c.VmsAPIURL)
		c.API.ConfigureRuntimeAuth(&sessionURL, &c.ServiceID, &c.SigningPrivateKey)
	}

	if c.Websocket != nil {
		wsURL := fmt.Sprintf("%s/api/m2m/service/%s", c.VmsWSURL, c.ServiceID)
		c.Websocket.SetURL(wsURL)
	}
}
