package config

import (
	"doorservice/websocket"
	"go.uber.org/zap"
)

func newWebsocketClient(logger *zap.Logger) *websocket.Client {
	return websocket.New()
}
