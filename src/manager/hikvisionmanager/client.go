package hikvisionmanager

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/icholy/digest"
)

type client struct {
	baseClient *http.Client
	timeout    time.Duration
}

func newClient(baseClient *http.Client) *client {
	if baseClient == nil {
		baseClient = http.DefaultClient
	}
	return &client{baseClient: baseClient, timeout: 10 * time.Second}
}

func (c *client) request(
	ctx context.Context,
	method, endpoint, username, password string,
	verifyTLS bool,
	body []byte,
) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	baseTransport := c.baseClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, 0, err
	}
	if parsed.Scheme == "https" {
		if transport, ok := baseTransport.(*http.Transport); ok {
			clone := transport.Clone()
			if clone.TLSClientConfig == nil {
				clone.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			} else {
				clone.TLSClientConfig = clone.TLSClientConfig.Clone()
			}
			clone.TLSClientConfig.InsecureSkipVerify = !verifyTLS // #nosec G402 -- controlled by per-controller configuration.
			baseTransport = clone
		}
	}

	digestTransport := &digest.Transport{
		Username:  username,
		Password:  password,
		Transport: baseTransport,
	}
	httpClient := &http.Client{Transport: digestTransport, Timeout: c.timeout}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/xml")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return payload, resp.StatusCode, nil
}

func baseURL(tlsEnabled bool, host string, port int) string {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
		if port == 0 {
			port = 443
		}
	} else if port == 0 {
		port = 80
	}
	return scheme + "://" + host + ":" + strconv.Itoa(port)
}

func remoteControlPath(index int) string {
	return fmt.Sprintf("/ISAPI/AccessControl/RemoteControl/door/%d", index)
}
