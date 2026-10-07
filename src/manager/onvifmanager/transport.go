package onvifmanager

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DeviceNamespace        = "http://www.onvif.org/ver10/device/wsdl"
	AccessControlNamespace = "http://www.onvif.org/ver10/accesscontrol/wsdl"
	DoorControlNamespace   = "http://www.onvif.org/ver10/doorcontrol/wsdl"
	soapNamespace          = "http://www.w3.org/2003/05/soap-envelope"
	wsseNamespace          = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	wsuNamespace           = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
)

type Credential struct {
	Username string
	Password string
}

type SOAPFault struct {
	HTTPStatus int
	Code       string
	Reason     string
}

func (f *SOAPFault) Error() string {
	if f == nil {
		return ""
	}
	if f.Reason != "" {
		return fmt.Sprintf("SOAP fault %s: %s", f.Code, f.Reason)
	}
	return fmt.Sprintf("SOAP fault %s", f.Code)
}

type Transport struct {
	client  *http.Client
	timeout time.Duration
	now     func() time.Time
	nonce   func([]byte) error
}

func NewTransport(client *http.Client, timeout time.Duration) *Transport {
	if client == nil {
		client = http.DefaultClient
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Transport{
		client:  client,
		timeout: timeout,
		now:     time.Now,
		nonce: func(value []byte) error {
			_, err := rand.Read(value)
			return err
		},
	}
}

func (t *Transport) Call(
	ctx context.Context,
	endpoint, action, body string,
	credential Credential,
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	nonce := make([]byte, 16)
	if err := t.nonce(nonce); err != nil {
		return nil, fmt.Errorf("generate WS-Security nonce: %w", err)
	}
	security := ""
	if credential.Username != "" || credential.Password != "" {
		security = BuildUsernameToken(credential.Username, credential.Password, t.now(), nonce)
	}
	envelope := buildEnvelope(security, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(envelope))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+action+`"`)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	if fault := parseSOAPFault(response); fault != nil {
		fault.HTTPStatus = resp.StatusCode
		return nil, fault
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("ONVIF SOAP status %d", resp.StatusCode)
	}
	return response, nil
}

func buildEnvelope(security, body string) string {
	header := ""
	if security != "" {
		header = "<s:Header>" + security + "</s:Header>"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="` + soapNamespace + `"` +
		` xmlns:tds="` + DeviceNamespace + `"` +
		` xmlns:tac="` + AccessControlNamespace + `"` +
		` xmlns:tdc="` + DoorControlNamespace + `"` +
		` xmlns:wsse="` + wsseNamespace + `"` +
		` xmlns:wsu="` + wsuNamespace + `">` +
		header + `<s:Body>` + body + `</s:Body></s:Envelope>`
}

func parseSOAPFault(document []byte) *SOAPFault {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	inFault := false
	fault := &SOAPFault{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local == "Fault" {
				inFault = true
				continue
			}
			if !inFault {
				continue
			}
			if typed.Name.Local == "Value" && fault.Code == "" {
				_ = decoder.DecodeElement(&fault.Code, &typed)
			}
			if typed.Name.Local == "Text" && fault.Reason == "" {
				_ = decoder.DecodeElement(&fault.Reason, &typed)
			}
		case xml.EndElement:
			if typed.Name.Local == "Fault" && inFault {
				fault.Code = strings.TrimSpace(fault.Code)
				fault.Reason = strings.TrimSpace(fault.Reason)
				return fault
			}
		}
	}
}
