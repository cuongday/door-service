package db

import (
	"time"

	"commonkit/database"
)

type ServiceModel struct {
	database.BaseModel
	Name             string    `gorm:"size:255" json:"name,omitempty"`
	Type             string    `gorm:"size:255" json:"type,omitempty"`
	HostName         string    `gorm:"size:255" json:"hostName,omitempty"`
	IPAddress        string    `gorm:"size:255" json:"ipAddress,omitempty"`
	MacAddress       string    `gorm:"size:255" json:"macAddress,omitempty"`
	Heartbeat        time.Time `gorm:"type:timestamp" json:"heartbeat,omitempty"`
	State            string    `gorm:"size:12" json:"state,omitempty"`
	RbmqUsername     string    `gorm:"size:255" json:"rbmqUsername,omitempty"`
	RbmqPassword     string    `gorm:"size:255" json:"rbmqPassword,omitempty"`
	RbmqPrivateHost  string    `gorm:"size:255" json:"rbmqPrivateHost,omitempty"`
	RbmqPrivatePort  int       `json:"rbmqPrivatePort,omitempty"`
	RbmqPublicHost   string    `gorm:"size:255" json:"rbmqPublicHost,omitempty"`
	RbmqPublicPort   int       `json:"rbmqPublicPort,omitempty"`
	RbmqVirtualHost  string    `gorm:"size:255" json:"rbmqVirtualHost,omitempty"`
	RbmqQueuePostfix string    `gorm:"size:255" json:"queuePostfix,omitempty"`
	PartnerID        string    `gorm:"size:255" json:"partnerId,omitempty"`
	PublicKeyPem     string    `gorm:"type:text" json:"publicKeyPem,omitempty"`
}
