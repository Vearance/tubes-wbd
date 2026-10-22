package targetdb

import (
	"fmt"
	"testing"
	"time"
	"github.com/stretchr/testify/require"
)

func validConfig() ConnConfig {
	return ConnConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "target",
		Password: "s3cret-pass",
		DBName:   "target_one",
		SSLMode:  "disable",
	}
}

func TestConnConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*ConnConfig)
		wantErr bool
	}{
		{name: "valid", modify: func(*ConnConfig) {}},
		{name: "empty sslmode defaults to prefer", modify: func(c *ConnConfig) { c.SSLMode = "" }},
		{name: "verify-full sslmode", modify: func(c *ConnConfig) { c.SSLMode = "verify-full" }},
		{name: "missing host", modify: func(c *ConnConfig) { c.Host = "" }, wantErr: true},
		{name: "missing port", modify: func(c *ConnConfig) { c.Port = 0 }, wantErr: true},
		{name: "missing user", modify: func(c *ConnConfig) { c.User = "" }, wantErr: true},
		{name: "missing database name", modify: func(c *ConnConfig) { c.DBName = "" }, wantErr: true},
		{name: "unknown sslmode", modify: func(c *ConnConfig) { c.SSLMode = "always" }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidConfig)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestConnConfigFormattingHidesPassword(t *testing.T) {
	cfg := validConfig()
	for _, verb := range []string{"%s", "%v", "%+v", "%#v"} {
		out := fmt.Sprintf(verb, cfg)
		require.NotContains(t, out, cfg.Password, "verb %s", verb)
		require.Contains(t, out, "target@localhost:5432/target_one", "verb %s", verb)
	}
}

func TestOptionsWithDefaults(t *testing.T) {
	require.Equal(t, DefaultOptions(), Options{}.withDefaults())
	custom := Options{
		MaxConns:          2,
		ConnectTimeout:    time.Second,
		StatementTimeout:  2 * time.Second,
		MaxConnIdleTime:   3 * time.Second,
		HealthCheckPeriod: 4 * time.Second,
	}
	require.Equal(t, custom, custom.withDefaults())
}