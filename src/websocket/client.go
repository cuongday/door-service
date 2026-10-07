package websocket

import (
	"sync"

	"doorservice/auth"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type Client struct {
	sync.RWMutex
	url  string
	conn *websocket.Conn
	log  *zap.Logger
}

var (
	client     *Client
	clientOnce sync.Once
)

func New() *Client {
	clientOnce.Do(func() {
		client = &Client{
			log: zap.NewNop(),
		}
	})
	return client
}

func (c *Client) SetURL(url string) {
	c.Lock()
	defer c.Unlock()
	c.url = url
}

func (c *Client) URL() string {
	c.RLock()
	defer c.RUnlock()
	return c.url
}

func (c *Client) Connect() error {
	c.Lock()
	defer c.Unlock()

	if c.url == "" {
		return nil
	}

	headers, err := auth.GetAuthTransport().SignedHeadersForWebsocket()
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.Dial(c.url, headers)
	if err != nil {
		c.log.Error("Failed to connect to websocket", zap.String("url", c.url), zap.Error(err))
		return err
	}

	c.conn = conn
	c.log.Info("Connected to websocket", zap.String("url", c.url))
	return nil
}

func (c *Client) Close() error {
	c.Lock()
	defer c.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *Client) Send(message []byte) error {
	c.RLock()
	defer c.RUnlock()

	if c.conn == nil {
		return nil
	}
	return c.conn.WriteMessage(websocket.TextMessage, message)
}
