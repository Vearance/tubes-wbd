package manageddb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"tubes-wbd/internal/database/targetdb"
)

type Source struct {
	repo   Repository
	cipher *Cipher
}

var _ targetdb.ConfigSource = (*Source)(nil)

func NewSource(repo Repository, cipher *Cipher) (*Source, error) {
	if repo == nil {
		return nil, errors.New("managed database repository is nil")
	}
	if cipher == nil {
		return nil, errors.New("managed database cipher is nil")
	}

	return &Source{repo: repo, cipher: cipher}, nil
}

func (s *Source) EncryptedSnapshot(ctx context.Context, id targetdb.DatabaseID) ([]byte, error) {
	config, err := s.ConnConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encoding target database configuration snapshot: %w", err)
	}
	return s.cipher.Encrypt(string(encoded)), nil
}

func (s *Source) ResolveSnapshot(encrypted []byte) (targetdb.ConnConfig, error) {
	encoded, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return targetdb.ConnConfig{}, fmt.Errorf("decrypting target database configuration snapshot: %w", err)
	}
	var config targetdb.ConnConfig
	if err := json.Unmarshal([]byte(encoded), &config); err != nil {
		return targetdb.ConnConfig{}, fmt.Errorf("decoding target database configuration snapshot: %w", err)
	}
	if err := config.Validate(); err != nil {
		return targetdb.ConnConfig{}, fmt.Errorf("invalid target database configuration snapshot: %w", err)
	}
	return config, nil
}

func (s *Source) ConnConfig(ctx context.Context, id targetdb.DatabaseID) (targetdb.ConnConfig, error) {
	d, err := s.repo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return targetdb.ConnConfig{}, fmt.Errorf("%w: %d", targetdb.ErrUnknownDatabase, id)
		}
		return targetdb.ConnConfig{}, err
	}

	password, err := s.cipher.Decrypt(d.EncryptedPassword)
	if err != nil {
		return targetdb.ConnConfig{}, fmt.Errorf("managed database %d: %w", id, err)
	}

	return d.connConfig(password), nil
}
