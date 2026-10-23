package targetdb

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
)

const applicationName = "dbm-platform"

type Manager struct {
	src  ConfigSource
	opts Options
	mu     sync.Mutex
	pools  map[DatabaseID]*pgxpool.Pool
	gen    map[DatabaseID]uint64
	closed bool
	sf singleflight.Group
}

func NewManager(src ConfigSource, opts Options) *Manager {
	return &Manager{
		src:   src,
		opts:  opts.withDefaults(),
		pools: make(map[DatabaseID]*pgxpool.Pool),
		gen:   make(map[DatabaseID]uint64),
	}
}

func (m *Manager) Pool(ctx context.Context, id DatabaseID) (*pgxpool.Pool, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrManagerClosed
	}
	pool, ok := m.pools[id]
	m.mu.Unlock()
	if ok {
		return pool, nil
	}

	flightCtx := context.WithoutCancel(ctx)
	ch := m.sf.DoChan(strconv.FormatUint(uint64(id), 10), func() (any, error) {
		return m.open(flightCtx, id)
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*pgxpool.Pool), nil
	case <-ctx.Done():
		return nil, mapError(ctx.Err())
	}
}

func (m *Manager) open(ctx context.Context, id DatabaseID) (*pgxpool.Pool, error) {
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrManagerClosed
		}
		if pool, ok := m.pools[id]; ok {
			m.mu.Unlock()
			return pool, nil
		}
		gen := m.gen[id]
		m.mu.Unlock()

		pool, err := m.openPool(ctx, id)
		if err != nil {
			return nil, err
		}

		m.mu.Lock()
		stale := m.closed || m.gen[id] != gen
		if !stale {
			m.pools[id] = pool
		}
		m.mu.Unlock()
		if !stale {
			return pool, nil
		}
		pool.Close()
	}
}

func (m *Manager) openPool(ctx context.Context, id DatabaseID) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, m.opts.ConnectTimeout)
	defer cancel()

	cfg, err := m.src.ConnConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	return m.connect(ctx, cfg)
}

func (m *Manager) Test(ctx context.Context, cfg ConnConfig) error {
	ctx, cancel := context.WithTimeout(ctx, m.opts.ConnectTimeout)
	defer cancel()

	pool, err := m.connect(ctx, cfg)
	if err != nil {
		return err
	}
	pool.Close()
	return nil
}

func (m *Manager) Invalidate(id DatabaseID) {
	m.mu.Lock()
	m.gen[id]++
	pool := m.pools[id]
	delete(m.pools, id)
	m.mu.Unlock()

	if pool != nil {
		pool.Close()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	pools := m.pools
	m.pools = make(map[DatabaseID]*pgxpool.Pool)
	m.mu.Unlock()

	for _, pool := range pools {
		pool.Close()
	}
}

func (m *Manager) connect(ctx context.Context, c ConnConfig) (*pgxpool.Pool, error) {
	cfg, err := buildPoolConfig(c, m.opts)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, mapError(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, mapError(err)
	}
	return pool, nil
}

func buildPoolConfig(c ConnConfig, o Options) (*pgxpool.Config, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	u := url.URL{
		Scheme:   "postgres",
		User:     url.User(c.User),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))),
		Path:     "/" + c.DBName,
		RawQuery: url.Values{"sslmode": {c.sslMode()}}.Encode(),
	}
	cfg, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	cfg.ConnConfig.Password = c.Password
	cfg.ConnConfig.ConnectTimeout = o.ConnectTimeout
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(o.StatementTimeout.Milliseconds(), 10)
	cfg.ConnConfig.RuntimeParams["application_name"] = applicationName

	cfg.MaxConns = o.MaxConns
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = o.MaxConnIdleTime
	cfg.HealthCheckPeriod = o.HealthCheckPeriod
	return cfg, nil
}