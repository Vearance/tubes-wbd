package manageddb

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/database/targetdb"
)

func TestConnConfig(t *testing.T) {
	db := &ManagedDatabase{
		Host:     "db-a",
		Port:     5432,
		Username: "target",
		DBName:   "shop",
		SSLMode:  "disable",
	}

	require.Equal(t, targetdb.ConnConfig{
		Host:     "db-a",
		Port:     5432,
		User:     "target",
		Password: "target_password",
		DBName:   "shop",
		SSLMode:  "disable",
	}, db.connConfig("target_password"))
}

func TestManagedDatabaseJSONHidesPassword(t *testing.T) {
	db := ManagedDatabase{
		ID:                1,
		Name:              "shop",
		EncryptedPassword: []byte("ciphertext-bytes"),
	}

	out, err := json.Marshal(db)
	require.NoError(t, err)
	require.NotContains(t, string(out), "password")
	require.NotContains(t, string(out), "ciphertext-bytes")
}
