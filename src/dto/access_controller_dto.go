package dto

type AccessControllerDTO struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Name            string `json:"name"`
	IPAddress       string `json:"ipAddress"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	TLS             bool   `json:"tls"`
	VerifyTLS       bool   `json:"verifyTls"`
	State           string `json:"state"`
	SerialNumber    string `json:"serialNumber"`
	MacAddress      string `json:"macAddress"`
	DeviceModel     string `json:"deviceModel"`
	FirmwareVersion string `json:"firmwareVersion"`
	Manufacturer    string `json:"manufacturer"`
	DeviceType      string `json:"deviceType"`    // ISAPI deviceType field
	DeviceInfoRaw   string `json:"deviceInfoRaw"`
	Activate        bool   `json:"activate"`

	// AI-Box scope - nullable for server-based, non-null for AI-Box-based
	AIBoxID   string `json:"aiBoxId,omitempty"`
	PartnerID string `json:"partnerId,omitempty"`
}
