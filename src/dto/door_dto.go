package dto

type DoorDTO struct {
	ID                 string         `json:"id"`
	DoorType           string         `json:"doorType"`
	Name               string         `json:"name"`
	BrandName          string         `json:"brandName"`
	Username           string         `json:"username"`
	Password           string         `json:"password"`
	IPAddress          string         `json:"ipAddress"`
	Port               int            `json:"port"`
	Activate           bool           `json:"activate"`
	AccessControllerID string         `json:"accessControllerId"`
	DoorIndex          *int           `json:"doorIndex"`
	ONVIFEndpoint      string         `json:"onvifEndpoint"`
	ONVIFDoorToken     string         `json:"onvifDoorToken"`
	ONVIFLockToken     string         `json:"onvifLockToken"`
	ProtocolMetadata   map[string]any `json:"protocolMetadata"`

	// AI-Box scope - nullable for server-based, non-null for AI-Box-based
	AIBoxID   string `json:"aiBoxId,omitempty"`
	PartnerID string `json:"partnerId,omitempty"`
}
