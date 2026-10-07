package db_test

import (
	"testing"

	"commonkit/database"
	"doorservice/db"

	"github.com/stretchr/testify/require"
)

func TestServiceRepoPersistsRegisteredService(t *testing.T) {
	connection, err := database.SetupDatabase(t.TempDir() + "/service.db")
	require.NoError(t, err)

	repo := db.New(connection)
	saved, err := repo.Save(db.ServiceModel{
		BaseModel: database.BaseModel{ID: "service-1"},
		Name:      "DOOR_SERVICE",
		Type:      "DOOR",
	})
	require.NoError(t, err)
	require.Equal(t, "service-1", saved.ID)

	service, err := repo.FindFirst()
	require.NoError(t, err)
	require.Equal(t, "service-1", service.ID)
	require.Equal(t, "DOOR", service.Type)
}

func TestServiceRepoSaveReplacesPreviousRegistration(t *testing.T) {
	connection, err := database.SetupDatabase(t.TempDir() + "/service.db")
	require.NoError(t, err)
	repo := db.New(connection)

	_, err = repo.Save(db.ServiceModel{BaseModel: database.BaseModel{ID: "a-old-service"}, Type: "DOOR"})
	require.NoError(t, err)
	_, err = repo.Save(db.ServiceModel{BaseModel: database.BaseModel{ID: "z-new-service"}, Type: "DOOR"})
	require.NoError(t, err)

	service, err := repo.FindFirst()
	require.NoError(t, err)
	require.Equal(t, "z-new-service", service.ID)
}
