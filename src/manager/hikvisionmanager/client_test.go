package hikvisionmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"doorservice/dto"
	serviceManager "doorservice/manager"

	"github.com/stretchr/testify/require"
)

const deviceInfoXML = `<DeviceInfo><model>DS-K2604</model><serialNumber>SN-1</serialNumber><firmwareVersion>V1.0</firmwareVersion></DeviceInfo>`

// Response shapes captured from a DS-K2602 class controller.
const acsWorkStatusTwoDoors = `{"AcsWorkStatus":{"doorLockStatus":[0,0],"doorStatus":[4,4],"magneticStatus":[0,0],"cardReaderOnlineStatus":[1,2,3,4]}}`

func doorParamXML(name string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<DoorParam version="2.0" xmlns="http://www.isapi.org/ver20/XMLSchema">
<doorName>` + name + `</doorName>
<openDuration>5</openDuration>
<superPassword>MjMyMzIz</superPassword>
</DoorParam>`
}

func discover(t *testing.T, server *httptest.Server) ([]dto.DoorDTO, error) {
	t.Helper()
	host, port := serverAddress(t, server.URL)
	return New(server.Client()).DiscoverDoors(context.Background(), dto.AccessControllerDTO{
		ID: "controller-1", IPAddress: host, Port: port, Username: "admin", Password: "12345",
	})
}

func TestDiscoverDoorsCountsFromAcsWorkStatusAndNamesFromDoorParam(t *testing.T) {
	server := newDigestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ISAPI/System/deviceInfo":
			_, _ = io.WriteString(w, deviceInfoXML)
		case "/ISAPI/AccessControl/AcsWorkStatus":
			require.Equal(t, "json", r.URL.Query().Get("format"))
			_, _ = io.WriteString(w, acsWorkStatusTwoDoors)
		case "/ISAPI/AccessControl/Door/param/1":
			_, _ = io.WriteString(w, doorParamXML("Door1"))
		case "/ISAPI/AccessControl/Door/param/2":
			http.Error(w, "busy", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	doors, err := discover(t, server)

	require.NoError(t, err)
	require.Len(t, doors, 2)
	require.Equal(t, 1, *doors[0].DoorIndex)
	require.Equal(t, "Door1", doors[0].Name)
	require.Equal(t, 2, *doors[1].DoorIndex)
	require.Equal(t, "Door 2", doors[1].Name)
	require.Equal(t, "controller-1", doors[0].AccessControllerID)
	require.Empty(t, doors[0].ID)
}

func TestDiscoverDoorsProbesDoorParamWhenWorkStatusIsUnsupported(t *testing.T) {
	server := newDigestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ISAPI/System/deviceInfo":
			_, _ = io.WriteString(w, deviceInfoXML)
		case "/ISAPI/AccessControl/Door/param/1":
			_, _ = io.WriteString(w, doorParamXML("Front"))
		case "/ISAPI/AccessControl/Door/param/2":
			_, _ = io.WriteString(w, doorParamXML("Back"))
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	doors, err := discover(t, server)

	require.NoError(t, err)
	require.Len(t, doors, 2)
	require.Equal(t, "Front", doors[0].Name)
	require.Equal(t, "Back", doors[1].Name)
}

func TestDiscoverDoorsStopsOnAuthFailure(t *testing.T) {
	var mu sync.Mutex
	paramCalls := 0
	server := newDigestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ISAPI/System/deviceInfo":
			_, _ = io.WriteString(w, deviceInfoXML)
		case strings.HasPrefix(r.URL.Path, "/ISAPI/AccessControl/Door/param/"):
			mu.Lock()
			paramCalls++
			mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	_, err := discover(t, server)

	var serviceErr *serviceManager.ServiceError
	require.ErrorAs(t, err, &serviceErr)
	require.Equal(t, serviceManager.ErrorAuthFailed, serviceErr.Code)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, paramCalls)
}

func TestDiscoverDoorsWithNoDoorsIsAnError(t *testing.T) {
	server := newDigestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ISAPI/System/deviceInfo" {
			_, _ = io.WriteString(w, deviceInfoXML)
			return
		}
		http.NotFound(w, r)
	})
	defer server.Close()

	doors, err := discover(t, server)

	require.Nil(t, doors)
	var serviceErr *serviceManager.ServiceError
	require.ErrorAs(t, err, &serviceErr)
	require.Equal(t, serviceManager.ErrorCommandRejected, serviceErr.Code)
}

func TestOpenAndCloseUseRemoteControlDoorCommands(t *testing.T) {
	var mu sync.Mutex
	commands := make([]string, 0, 2)
	server := newDigestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/ISAPI/AccessControl/RemoteControl/door/1" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		mu.Lock()
		commands = append(commands, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	defer server.Close()

	host, port := serverAddress(t, server.URL)
	index := 1
	door := dto.DoorDTO{
		DoorType: "HIKVISION", IPAddress: host, Port: port,
		Username: "admin", Password: "12345", DoorIndex: &index,
	}
	m := New(server.Client())
	require.NoError(t, m.OpenDoor(context.Background(), door))
	require.NoError(t, m.CloseDoor(context.Background(), door))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, commands, 2)
	require.Contains(t, commands[0], "<cmd>open</cmd>")
	require.Contains(t, commands[1], "<cmd>close</cmd>")
}

func newDigestServer(t *testing.T, authorized http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Digest ") {
			w.Header().Set("WWW-Authenticate", `Digest realm="door-test", nonce="abc123", qop="auth", algorithm=MD5`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authorized(w, r)
	}))
}

func serverAddress(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	return parsed.Hostname(), port
}
