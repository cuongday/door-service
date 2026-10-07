package onvifmanager

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net"
	"sort"
	"strings"

	"doorservice/dto"
)

type serviceEndpoint struct {
	Namespace string
	XAddr     string
}

func deviceEndpoint(controller dto.AccessControllerDTO) string {
	scheme := "http"
	port := controller.Port
	if controller.TLS {
		scheme = "https"
		if port == 0 {
			port = 443
		}
	} else if port == 0 {
		port = 80
	}
	return fmt.Sprintf("%s://%s/onvif/device_service", scheme, net.JoinHostPort(controller.IPAddress, fmt.Sprint(port)))
}

func applyDeviceInformation(controller dto.AccessControllerDTO, document []byte) dto.AccessControllerDTO {
	controller.Manufacturer = FindFirstText(document, "Manufacturer")
	controller.DeviceModel = FindFirstText(document, "Model")
	controller.FirmwareVersion = FindFirstText(document, "FirmwareVersion")
	controller.SerialNumber = FindFirstText(document, "SerialNumber")
	controller.DeviceInfoRaw = string(document)
	controller.Type = "ONVIF"
	controller.State = "CONNECTED"
	controller.Activate = true
	return controller
}

func parseServiceEndpoints(document []byte) []serviceEndpoint {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	result := make([]serviceEndpoint, 0)
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Service" {
			continue
		}
		var raw struct {
			Namespace string `xml:"Namespace"`
			XAddr     string `xml:"XAddr"`
		}
		if err := decoder.DecodeElement(&raw, &start); err != nil {
			continue
		}
		result = append(result, serviceEndpoint{
			Namespace: strings.TrimSpace(raw.Namespace),
			XAddr:     strings.TrimSpace(raw.XAddr),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Namespace < result[j].Namespace })
	return result
}

func findServiceEndpoint(endpoints []serviceEndpoint, namespace string) string {
	for _, endpoint := range endpoints {
		if endpoint.Namespace == namespace {
			return endpoint.XAddr
		}
	}
	return ""
}
