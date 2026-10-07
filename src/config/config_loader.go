package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"commonkit/util/logutil"
	"commonkit/util/netutil"
	"doorservice/api"
	"doorservice/auth"
	"doorservice/db"
	"doorservice/websocket"
	"go.uber.org/zap"
)

var (
	configInstance ConfigInterface
	configOnce     sync.Once
)

func GetConfig() ConfigInterface {
	return configInstance
}

func Load(path string) error {
	var err error
	configOnce.Do(func() {
		configInstance, err = doLoad(path)
	})
	return err
}

func doLoad(path string) (ConfigInterface, error) {
	var value ConfigJSON

	// Try to load from path first
	if data, readErr := os.ReadFile(path); readErr == nil {
		if unmarshalErr := json.Unmarshal(data, &value); unmarshalErr == nil {
			return createConfig(value), nil
		}
	}

	configPaths := []string{
		"config.json",
		"data/config.json",
		filepath.Join("data", "config", "config.json"),
	}

	for _, configPath := range configPaths {
		if data, err := os.ReadFile(configPath); err == nil {
			if err := json.Unmarshal(data, &value); err == nil {
				return createConfig(value), nil
			}
		}
	}

	return createConfig(ConfigJSON{
		IsBox:   false,
		APIHost: "localhost",
		APIPort: 8080,
	}), nil
}

func createConfig(value ConfigJSON) ConfigInterface {
	logFilePath := "logs/config.log"
	logger := logutil.CreateLogger(&logFilePath, true)

	// Create state directory
	stateDir := "data"
	_ = os.MkdirAll(stateDir, 0755)
	_ = os.MkdirAll(filepath.Join(stateDir, "keys"), 0700)

	// Create websocket client
	ws := websocket.New()

	// Initialize signing
	signing := auth.M2MSigning{
		PrivateKeyPath: filepath.Join(stateDir, "keys", "private.pem"),
	}
	signingPrivateKey, signingPublicKeyPEM, err := signing.EnsureKeyPair()
	if err != nil {
		logger.Fatal("Failed to ensure signing key", zap.Error(err))
	}

	// Initialize service repo
	serviceRepo := db.New(nil)
	service, _ := serviceRepo.FindFirst()
	var serviceID string
	if service != nil {
		serviceID = service.ID
	}

	// Create base config
	base := &BaseConfig{
		ServiceID:           serviceID,
		MacAddress:         netutil.GetMacAddress(),
		IPAddress:          netutil.GetIpAddress(),
		IsBox:              value.IsBox,
		DatabasePath:       filepath.Join(stateDir, "database.db"),
		StateDir:           stateDir,
		RetryDelay:         5 * time.Second,
		ConfigJSON:          value,
		Websocket:          ws,
		Signing:            signing,
		SigningPrivateKey:   signingPrivateKey,
		SigningPublicKeyPEM: signingPublicKeyPEM,
		ServiceRepo:        serviceRepo,
		logger:             logger,
	}

	// Initialize API
	base.API = api.New("", nil)

	if value.IsBox {
		return newBoxConfigWithBase(base)
	}
	return newServerConfigWithBase(base)
}
