package dto_test

import (
	"encoding/json"
	"testing"

	"doorservice/dto"
)

func TestDiscoveryDoorInfoDTOContainsOnlyDoorScanFields(t *testing.T) {
	onvifPort := 80
	payload, err := json.Marshal(dto.DiscoveryDoorInfoDTO{
		AIBoxID:   "00000000-0000-0000-0000-000000000552",
		Usernames: []string{"admin"},
		Passwords: []string{"12345"},
		FromIP:    "192.168.1.10",
		ToIP:      "192.168.1.20",
		Ports:     []int{80, 8080},
		ONVIFPort: &onvifPort,
	})
	if err != nil {
		t.Fatalf("marshal discovery door payload: %v", err)
	}

	want := `{"aiBoxId":"00000000-0000-0000-0000-000000000552","usernames":["admin"],"passwords":["12345"],"fromIp":"192.168.1.10","toIp":"192.168.1.20","ports":[80,8080],"onvifPort":80}`
	if string(payload) != want {
		t.Fatalf("unexpected discovery door payload: %s", payload)
	}
}
