package db

import (
	"strings"

	"commonkit/database"
	"gorm.io/gorm"
)

type ServiceRepo struct {
	db *gorm.DB
}

func New(connection *gorm.DB) *ServiceRepo {
	if connection != nil {
		if err := connection.AutoMigrate(&ServiceModel{}); err != nil {
			panic("failed to migrate service model: " + err.Error())
		}
	}
	return &ServiceRepo{db: connection}
}

func (r *ServiceRepo) EnsureConnection() error {
	if r.db != nil {
		return nil
	}

	dbConn, err := database.SetupDatabase("data/database.db")
	if err != nil {
		return err
	}

	if err := dbConn.AutoMigrate(&ServiceModel{}); err != nil {
		return err
	}

	r.db = dbConn
	return nil
}

func (r *ServiceRepo) FindFirst() (*ServiceModel, error) {
	if r.db == nil {
		if err := r.EnsureConnection(); err != nil {
			return nil, err
		}
	}

	var service ServiceModel
	result := r.db.Limit(1).Find(&service)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &service, nil
}

func (r *ServiceRepo) Save(service ServiceModel) (*ServiceModel, error) {
	if r.db == nil {
		if err := r.EnsureConnection(); err != nil {
			return nil, err
		}
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ServiceModel{}).Error; err != nil {
			return err
		}
		return tx.Create(&service).Error
	})
	if err != nil {
		return nil, err
	}
	return &service, nil
}

func (r *ServiceRepo) FindByType(t string) (*ServiceModel, error) {
	if r.db == nil {
		if err := r.EnsureConnection(); err != nil {
			return nil, err
		}
	}

	var service ServiceModel
	result := r.db.Where("type = ?", strings.TrimSpace(t)).Limit(1).Find(&service)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &service, nil
}

func (r *ServiceRepo) DeleteAll() error {
	if r.db == nil {
		if err := r.EnsureConnection(); err != nil {
			return err
		}
	}
	return r.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ServiceModel{}).Error
}
