package onvifmanager

import (
	"context"
	"testing"

	"doorservice/dto"

	"github.com/stretchr/testify/require"
)

func TestOpenDoorUsesAccessDoorAndCloseUsesLockDoor(t *testing.T) {
	transport := &fakeSOAPTransport{}
	m := NewWithTransport(transport)
	door := dto.DoorDTO{
		ONVIFEndpoint:  "http://panel/onvif/door_control",
		ONVIFDoorToken: "door_1",
		Username:       "admin",
		Password:       "12345",
	}

	require.NoError(t, m.OpenDoor(context.Background(), door))
	require.NoError(t, m.CloseDoor(context.Background(), door))
	require.Equal(t, []string{
		DoorControlNamespace + "/AccessDoor",
		DoorControlNamespace + "/LockDoor",
	}, transport.actions)
	require.Contains(t, transport.bodies[0], "door_1")
	require.Contains(t, transport.bodies[1], "door_1")
}

func TestDiscoverDoorsReturnsOneDoorPerDoorInfoToken(t *testing.T) {
	transport := &fakeSOAPTransport{responses: map[string][]byte{
		DeviceNamespace + "/GetDeviceInformation": []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>VendorX</tds:Manufacturer><tds:Model>AC-4000</tds:Model><tds:FirmwareVersion>1.2.3</tds:FirmwareVersion><tds:SerialNumber>SN-1</tds:SerialNumber></tds:GetDeviceInformationResponse></s:Body></s:Envelope>`),
		DeviceNamespace + "/GetServices":          []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Service><tds:Namespace>http://www.onvif.org/ver10/doorcontrol/wsdl</tds:Namespace><tds:XAddr>http://panel/onvif/door_control</tds:XAddr></tds:Service></tds:GetServicesResponse></s:Body></s:Envelope>`),
		DoorControlNamespace + "/GetDoorInfoList": []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><tdc:GetDoorInfoListResponse xmlns:tdc="http://www.onvif.org/ver10/doorcontrol/wsdl"><tdc:DoorInfo token="door_1"><tdc:Name>Main Entrance</tdc:Name><tdc:LockToken>lock_1</tdc:LockToken></tdc:DoorInfo><tdc:DoorInfo token="door_2"><tdc:Name>Side Door</tdc:Name></tdc:DoorInfo></tdc:GetDoorInfoListResponse></s:Body></s:Envelope>`),
	}}
	m := NewWithTransport(transport)
	doors, err := m.DiscoverDoors(context.Background(), dto.AccessControllerDTO{
		ID: "controller-1", IPAddress: "192.168.1.20", Port: 80,
		Username: "admin", Password: "12345", Activate: true,
	})

	require.NoError(t, err)
	require.Len(t, doors, 2)
	require.Equal(t, "door_1", doors[0].ONVIFDoorToken)
	require.Equal(t, "lock_1", doors[0].ONVIFLockToken)
	require.Equal(t, "door_2", doors[1].ONVIFDoorToken)
	require.Equal(t, "http://panel/onvif/door_control", doors[0].ONVIFEndpoint)
}

type fakeSOAPTransport struct {
	responses map[string][]byte
	actions   []string
	bodies    []string
}

func (f *fakeSOAPTransport) Call(
	_ context.Context,
	_ string,
	action string,
	body string,
	_ Credential,
) ([]byte, error) {
	f.actions = append(f.actions, action)
	f.bodies = append(f.bodies, body)
	return f.responses[action], nil
}
