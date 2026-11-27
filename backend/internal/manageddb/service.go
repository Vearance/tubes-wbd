package manageddb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"tubes-wbd/internal/database/targetdb"
	"tubes-wbd/internal/telemetry"
)

const defaultSSLMode = "prefer"

type Connector interface {
	Test(ctx context.Context, cfg targetdb.ConnConfig) error
	Invalidate(id targetdb.DatabaseID)
}

type Input struct {
	Name     string
	Host     string
	Port     uint16
	Username string
	Password string
	DBName   string
	SSLMode  string
}

func (in Input) normalize() Input {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.Username = strings.TrimSpace(in.Username)
	in.DBName = strings.TrimSpace(in.DBName)
	in.SSLMode = strings.TrimSpace(in.SSLMode)
	if in.SSLMode == "" {
		in.SSLMode = defaultSSLMode
	}
	return in
}

func (in Input) connConfig() targetdb.ConnConfig {
	return targetdb.ConnConfig{
		Host:     in.Host,
		Port:     in.Port,
		User:     in.Username,
		Password: in.Password,
		DBName:   in.DBName,
		SSLMode:  in.SSLMode,
	}
}

func (in Input) validate(requirePassword bool) error {
	if in.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if requirePassword && in.Password == "" {
		return fmt.Errorf("%w: password is required", ErrInvalidInput)
	}
	if err := in.connConfig().Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return nil
}

type ManagedDBService struct {
	repo   Repository
	cipher *Cipher
	conn   Connector
}

func NewManagedDBService(repo Repository, cipher *Cipher, conn Connector) (*ManagedDBService, error) {
	if repo == nil {
		return nil, errors.New("managed database repository is nil")
	}
	if cipher == nil {
		return nil, errors.New("managed database cipher is nil")
	}
	if conn == nil {
		return nil, errors.New("target database connector is nil")
	}

	return &ManagedDBService{repo: repo, cipher: cipher, conn: conn}, nil
}

func (s *ManagedDBService) Create(ctx context.Context, in Input) (*ManagedDatabase, error) {
	in = in.normalize()
	if err := in.validate(true); err != nil {
		return nil, err
	}

	d := &ManagedDatabase{
		Name:              in.Name,
		Host:              in.Host,
		Port:              in.Port,
		Username:          in.Username,
		EncryptedPassword: s.cipher.Encrypt(in.Password),
		DBName:            in.DBName,
		SSLMode:           in.SSLMode,
	}
	if err := s.repo.Create(ctx, d); err != nil {
		return nil, err
	}

	targetDB := telemetry.TargetDatabaseLabel(d.ID)
	telemetry.SetRequestDatabase(ctx, targetDB)
	slog.InfoContext(ctx, "managed database registered",
		"event", "database.created",
		"database", targetDB,
		"host", d.Host,
		"port", d.Port,
		"db_name", d.DBName,
		"ssl_mode", d.SSLMode,
		"status", "ok",
	)
	return d, nil
}

func (s *ManagedDBService) Get(ctx context.Context, id uint) (*ManagedDatabase, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *ManagedDBService) List(ctx context.Context) ([]ManagedDatabase, error) {
	return s.repo.List(ctx)
}

func (s *ManagedDBService) Update(ctx context.Context, id uint, in Input) (*ManagedDatabase, error) {
	in = in.normalize()
	if err := in.validate(false); err != nil {
		return nil, err
	}

	d, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	d.Name = in.Name
	d.Host = in.Host
	d.Port = in.Port
	d.Username = in.Username
	d.DBName = in.DBName
	d.SSLMode = in.SSLMode
	if in.Password != "" {
		d.EncryptedPassword = s.cipher.Encrypt(in.Password)
	}

	if err := s.repo.Update(ctx, d); err != nil {
		return nil, err
	}
	s.conn.Invalidate(id)

	targetDB := telemetry.TargetDatabaseLabel(id)
	telemetry.SetRequestDatabase(ctx, targetDB)
	slog.InfoContext(ctx, "managed database updated",
		"event", "database.updated",
		"database", targetDB,
		"host", d.Host,
		"port", d.Port,
		"db_name", d.DBName,
		"ssl_mode", d.SSLMode,
		"status", "ok",
	)
	return d, nil
}

func (s *ManagedDBService) Delete(ctx context.Context, id uint) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.conn.Invalidate(id)

	targetDB := telemetry.TargetDatabaseLabel(id)
	telemetry.SetRequestDatabase(ctx, targetDB)
	slog.InfoContext(ctx, "managed database deleted",
		"event", "database.deleted",
		"database", targetDB,
		"status", "ok",
	)
	return nil
}

func (s *ManagedDBService) TestConnection(ctx context.Context, in Input) error {
	in = in.normalize()
	if err := in.connConfig().Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return s.logConnectionTest(ctx, "target:unsaved", in.Host, in.Port, in.DBName, in.SSLMode, in.connConfig())
}

func (s *ManagedDBService) TestStored(ctx context.Context, id uint) error {
	d, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	password, err := s.cipher.Decrypt(d.EncryptedPassword)
	if err != nil {
		return fmt.Errorf("managed database %d: %w", id, err)
	}

	targetDB := telemetry.TargetDatabaseLabel(id)
	telemetry.SetRequestDatabase(ctx, targetDB)
	return s.logConnectionTest(ctx, targetDB, d.Host, d.Port, d.DBName, d.SSLMode, d.connConfig(password))
}

func (s *ManagedDBService) logConnectionTest(
	ctx context.Context,
	dbLabel string,
	host string,
	port uint16,
	dbName string,
	sslMode string,
	cfg targetdb.ConnConfig,
) error {
	start := time.Now()
	err := s.conn.Test(ctx, cfg)
	durationMs := telemetry.RoundMillis(time.Since(start))
	attrs := []any{
		"event", "database.connection.tested",
		"database", dbLabel,
		"host", host,
		"port", port,
		"db_name", dbName,
		"ssl_mode", sslMode,
		"duration_ms", durationMs,
	}
	if err != nil {
		attrs = append(attrs, "status", "error", "error", err)
		if sqlstate := telemetry.SQLState(err); sqlstate != "" {
			attrs = append(attrs, "sqlstate", sqlstate)
		}
		slog.WarnContext(ctx, "managed database connection test failed", attrs...)
		return err
	}
	attrs = append(attrs, "status", "ok")
	slog.InfoContext(ctx, "managed database connection test succeeded", attrs...)
	return nil
}
