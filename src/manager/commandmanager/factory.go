package commandmanager

import (
	"strings"

	"doorservice/dto"
	serviceManager "doorservice/manager"
)

type Factory struct {
	onvif     serviceManager.DoorAdapter
	hikvision serviceManager.DoorAdapter
	zkteco    serviceManager.DoorAdapter
	dahua     serviceManager.DoorAdapter
}

func NewFactory(
	onvif, hikvision, zkteco, dahua serviceManager.DoorAdapter,
) *Factory {
	return &Factory{
		onvif:     onvif,
		hikvision: hikvision,
		zkteco:    zkteco,
		dahua:     dahua,
	}
}

func (f *Factory) ForDoor(door dto.DoorDTO) (serviceManager.DoorAdapter, error) {
	return f.forProtocol(door.DoorType)
}

func (f *Factory) ForController(controller dto.AccessControllerDTO) (serviceManager.DoorAdapter, error) {
	return f.forProtocol(controller.Type)
}

func (f *Factory) forProtocol(protocol string) (serviceManager.DoorAdapter, error) {
	switch strings.ToUpper(strings.TrimSpace(protocol)) {
	case "ONVIF":
		if f.onvif != nil {
			return f.onvif, nil
		}
	case "HIKVISION":
		if f.hikvision != nil {
			return f.hikvision, nil
		}
	case "ZKTECO":
		if f.zkteco != nil {
			return f.zkteco, nil
		}
	case "DAHUA":
		if f.dahua != nil {
			return f.dahua, nil
		}
	}
	return nil, serviceManager.NewServiceError(
		serviceManager.ErrorUnsupportedProtocol,
		"door protocol is not supported: "+protocol,
		nil,
	)
}
