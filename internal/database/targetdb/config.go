package targetdb

import (
	"context"
	"fmt"
	"time"
)

type DatabaseID = uint
// connection config yang diperluin buat connect ke database
type ConnConfig struct {
	Host     string
	Port     uint16
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// ngatur apakah koneksi client ke server dienkripsi pake TLS atau ngga,
// dan kalaupun dienkripsi, seberapa ketat client memverifikasi cert dari server.
var validSSLModes = map[string]bool{
	"disable":     true,
	"allow":       true,
	"prefer":      true,
	"require":     true,
	"verify-ca":   true,
	"verify-full": true,
}

// validasi koneksi ke database
func (c ConnConfig) Validate() error {
	switch {
	case c.Host == "":
		return fmt.Errorf("%w: host is required", ErrInvalidConfig)
	case c.Port == 0:
		return fmt.Errorf("%w: port is required", ErrInvalidConfig)
	case c.User == "":
		return fmt.Errorf("%w: user is required", ErrInvalidConfig)
	case c.DBName == "":
		return fmt.Errorf("%w: database name is required", ErrInvalidConfig)
	case c.SSLMode != "" && !validSSLModes[c.SSLMode]:
		return fmt.Errorf("%w: unsupported sslmode %q", ErrInvalidConfig, c.SSLMode)
	}
	return nil
}

func (c ConnConfig) sslMode() string {
	if c.SSLMode == "" {
		return "prefer"
	}
	return c.SSLMode
}

func (c ConnConfig) String() string {
	return fmt.Sprintf("%s@%s:%d/%s", c.User, c.Host, c.Port, c.DBName)
}

func (c ConnConfig) GoString() string {
	return "targetdb.ConnConfig{" + c.String() + "}"
}

type Options struct {
	MaxConns int32
	ConnectTimeout time.Duration
	StatementTimeout time.Duration
	MaxConnIdleTime time.Duration
	HealthCheckPeriod time.Duration
}

func DefaultOptions() Options {
	return Options{
		MaxConns:          5,
		ConnectTimeout:    5 * time.Second,
		StatementTimeout:  30 * time.Second,
		MaxConnIdleTime:   5 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.MaxConns <= 0 {
		o.MaxConns = d.MaxConns
	}
	if o.ConnectTimeout <= 0 {
		o.ConnectTimeout = d.ConnectTimeout
	}
	if o.StatementTimeout <= 0 {
		o.StatementTimeout = d.StatementTimeout
	}
	if o.MaxConnIdleTime <= 0 {
		o.MaxConnIdleTime = d.MaxConnIdleTime
	}
	if o.HealthCheckPeriod <= 0 {
		o.HealthCheckPeriod = d.HealthCheckPeriod
	}
	return o
}

type ConfigSource interface {
	ConnConfig(ctx context.Context, id DatabaseID) (ConnConfig, error)
}