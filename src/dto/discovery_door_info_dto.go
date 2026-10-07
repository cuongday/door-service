package dto

type DiscoveryDoorInfoDTO struct {
	AIBoxID   string   `json:"aiBoxId,omitempty"`
	Usernames []string `json:"usernames"`
	Passwords []string `json:"passwords"`
	FromIP    string   `json:"fromIp"`
	ToIP      string   `json:"toIp"`
	Ports     []int    `json:"ports"`
	ONVIFPort *int     `json:"onvifPort,omitempty"`
}
