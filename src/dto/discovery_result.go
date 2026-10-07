package dto

type DiscoveryResult struct {
	AIBoxID      string               `json:"aiBoxId,omitempty"`
	IP           string               `json:"ip"`
	Port         int                  `json:"port"`
	ONVIFPort    int                  `json:"onvif_port"`
	Username     string               `json:"username,omitempty"`
	Password     string               `json:"password,omitempty"`
	State        bool                 `json:"state"`
	ResultLevel  string               `json:"result_level"`
	Controller   *AccessControllerDTO `json:"accessController,omitempty"`
	Doors        []DoorDTO            `json:"doors,omitempty"`
	ErrorCode    string               `json:"errorCode,omitempty"`
	ErrorMessage string               `json:"errorMessage,omitempty"`
}
