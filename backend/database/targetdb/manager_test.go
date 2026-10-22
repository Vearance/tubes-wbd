package targetdb

import (
	"context"
	"sync"
	"testing"
	"time"
	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	mu    sync.Mutex
	cfgs  map[DatabaseID]ConnConfig
	calls int
}

func newFakeSource(cfgs map[DatabaseID]ConnConfig) *fakeSource {
	return &fakeSource{cfgs: cfgs}
}

func (f *fakeSource) ConnConfig(_ context.Context, id DatabaseID) (ConnConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	cfg, ok := f.cfgs[id]
	if !ok {
		return ConnConfig{}, ErrUnknownDatabase
	}
	return cfg, nil
}

func (f *fakeSource) set(id DatabaseID, cfg ConnConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfgs[id] = cfg
}

func (f *fakeSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestBuildPoolConfig(t *testing.T) {
	opts := Options{
		MaxConns:          3,
		ConnectTimeout:    2 * time.Second,
		StatementTimeout:  1500 * time.Millisecond,
		MaxConnIdleTime:   time.Minute,
		HealthCheckPeriod: 10 * time.Second,
	}
	c := validConfig()
	c.User = "us@er"
	c.DBName = "my db"

	cfg, err := buildPoolConfig(c, opts)
	require.NoError(t, err)

	require.Equal(t, "localhost", cfg.ConnConfig.Host)
	require.Equal(t, uint16(5432), cfg.ConnConfig.Port)
	require.Equal(t, "us@er", cfg.ConnConfig.User)
	require.Equal(t, "my db", cfg.ConnConfig.Database)
	require.Equal(t, c.Password, cfg.ConnConfig.Password)
	require.Nil(t, cfg.ConnConfig.TLSConfig, "sslmode=disable must not use TLS")
	require.Equal(t, 2*time.Second, cfg.ConnConfig.ConnectTimeout)
	require.Equal(t, "1500", cfg.ConnConfig.RuntimeParams["statement_timeout"])
	require.Equal(t, applicationName, cfg.ConnConfig.RuntimeParams["application_name"])
	require.Equal(t, int32(3), cfg.MaxConns)
	require.Equal(t, int32(0), cfg.MinConns)
	require.Equal(t, time.Minute, cfg.MaxConnIdleTime)
	require.Equal(t, 10*time.Second, cfg.HealthCheckPeriod)

	require.NotContains(t, cfg.ConnString(), c.Password)
}

func TestBuildPoolConfigRejectsInvalidConfig(t *testing.T) {
	c := validConfig()
	c.Host = ""

	_, err := buildPoolConfig(c, DefaultOptions())
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestPoolUnknownDatabase(t *testing.T) {
	src := newFakeSource(map[DatabaseID]ConnConfig{})
	m := NewManager(src, Options{})
	t.Cleanup(m.Close)

	_, err := m.Pool(context.Background(), 42)
	require.ErrorIs(t, err, ErrUnknownDatabase)
	require.Equal(t, 1, src.callCount())
}

func TestPoolInvalidConfig(t *testing.T) {
	c := validConfig()
	c.DBName = ""
	src := newFakeSource(map[DatabaseID]ConnConfig{1: c})
	m := NewManager(src, Options{})
	t.Cleanup(m.Close)

	_, err := m.Pool(context.Background(), 1)
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestTestInvalidConfig(t *testing.T) {
	m := NewManager(newFakeSource(map[DatabaseID]ConnConfig{}), Options{})
	t.Cleanup(m.Close)

	err := m.Test(context.Background(), ConnConfig{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestPoolAfterClose(t *testing.T) {
	src := newFakeSource(map[DatabaseID]ConnConfig{1: validConfig()})
	m := NewManager(src, Options{})
	m.Close()

	_, err := m.Pool(context.Background(), 1)
	require.ErrorIs(t, err, ErrManagerClosed)
	require.Zero(t, src.callCount(), "a closed manager must not look up configs")
}

func TestPoolCanceledContext(t *testing.T) {
	src := newFakeSource(map[DatabaseID]ConnConfig{})
	m := NewManager(src, Options{})
	t.Cleanup(m.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Pool(ctx, 1)
	require.Error(t, err)
}