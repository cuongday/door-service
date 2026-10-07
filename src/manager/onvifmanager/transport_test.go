package onvifmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildUsernameTokenContainsDigestNonceAndCreated(t *testing.T) {
	header := BuildUsernameToken(
		"admin",
		"12345",
		time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		[]byte("0123456789abcdef"),
	)
	require.Contains(t, header, "UsernameToken")
	require.Contains(t, header, "PasswordDigest")
	require.Contains(t, header, "admin")
	require.NotContains(t, header, ">12345<")
}

func TestTransportSendsSOAPActionAndParsesBody(t *testing.T) {
	var contentType string
	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		var err error
		requestBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, err = io.WriteString(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tdc:GetDoorInfoListResponse xmlns:tdc="http://www.onvif.org/ver10/doorcontrol/wsdl"/></s:Body></s:Envelope>`)
		require.NoError(t, err)
	}))
	defer server.Close()

	transport := NewTransport(server.Client(), 2*time.Second)
	action := DoorControlNamespace + "/GetDoorInfoList"
	response, err := transport.Call(
		context.Background(),
		server.URL,
		action,
		`<tdc:GetDoorInfoList/>`,
		Credential{Username: "admin", Password: "12345"},
	)
	require.NoError(t, err)
	require.Contains(t, contentType, `action="`+action+`"`)
	require.Contains(t, string(requestBody), "tdc:GetDoorInfoList")
	require.Contains(t, string(requestBody), "UsernameToken")
	require.Contains(t, string(response), "GetDoorInfoListResponse")
}

func TestTransportReturnsTypedSOAPFault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := io.WriteString(w, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value></s:Code><s:Reason><s:Text>Not authorized</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`)
		require.NoError(t, err)
	}))
	defer server.Close()

	_, err := NewTransport(server.Client(), time.Second).Call(
		context.Background(), server.URL, "action", `<tds:GetServices/>`, Credential{},
	)
	var fault *SOAPFault
	require.ErrorAs(t, err, &fault)
	require.Equal(t, "s:Sender", fault.Code)
	require.Equal(t, "Not authorized", fault.Reason)
}
