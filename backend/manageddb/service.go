package manageddb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"tubes-wbd/backend/database/targetdb"
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

type Service struct {
	repo   Repository
	cipher *Cipher
	conn   Connector
}

func NewService(repo Repository, cipher *Cipher, conn Connector) (*Service, error) {
	if repo == nil {
		return nil, errors.New("managed database repository is nil")
	}
	if cipher == nil {
		return nil, errors.New("managed database cipher is nil")
	}
	if conn == nil {
		return nil, errors.New("target database connector is nil")
	}

	return &Service{repo: repo, cipher: cipher, conn: conn}, nil
}

func (s *Service) Create(ctx context.Context, in Input) (*ManagedDatabase, error) {
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

	return d, nil
}

func (s *Service) Get(ctx context.Context, id uint) (*ManagedDatabase, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]ManagedDatabase, error) {
	return s.repo.List(ctx)
}

func (s *Service) Update(ctx context.Context, id uint, in Input) (*ManagedDatabase, error) {
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

	return d, nil
}

func (s *Service) Delete(ctx context.Context, id uint) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.conn.Invalidate(id)

	return nil
}

func (s *Service) TestConnection(ctx context.Context, in Input) error {
	in = in.normalize()
	if err := in.connConfig().Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	return s.conn.Test(ctx, in.connConfig())
}

func (s *Service) TestStored(ctx context.Context, id uint) error {
	d, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	password, err := s.cipher.Decrypt(d.EncryptedPassword)
	if err != nil {
		return fmt.Errorf("managed database %d: %w", id, err)
	}

	return s.conn.Test(ctx, d.connConfig(password))
}
