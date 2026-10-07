package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadDoorServiceDataUsesM2MEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/m2m/service/data" {
			t.Fatalf("request path = %s, want /api/m2m/service/data", r.URL.Path)
		}
		if r.URL.Query().Get("id") != "service-1" || r.URL.Query().Get("type") != "DOOR" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"id":"service-1","type":"DOOR","accessControllers":[],"doors":[]}`)
	}))
	defer server.Close()

	client := New(server.URL, nil)
	client.client = server.Client()
	data, err := client.ReadDoorServiceData("service-1")
	if err != nil {
		t.Fatalf("read door service data: %v", err)
	}
	if data == nil {
		t.Fatal("door service data is nil")
	}
}
