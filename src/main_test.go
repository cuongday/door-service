package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueueNamesUseRegisteredDoorServicePrefix(t *testing.T) {
	got := BuildQueueNames("DOOR_00000000-0000-0000-0000-000000000001")

	require.Equal(t, "DOOR_00000000-0000-0000-0000-000000000001_discovery", got.Discovery)
	require.Equal(t, "DOOR_00000000-0000-0000-0000-000000000001_third_party_access_controller", got.AccessController)
	require.Equal(t, "DOOR_00000000-0000-0000-0000-000000000001_command", got.Command)
	require.Equal(t, "DOOR_00000000-0000-0000-0000-000000000001_door", got.Door)
}

func TestServiceHTTPClientTimesOutHungBootstrapRequest(t *testing.T) {
	client := newServiceHTTPClient(20 * time.Millisecond)
	client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://vms/service-data", nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.Error(t, err)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr))
	require.True(t, netErr.Timeout())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
