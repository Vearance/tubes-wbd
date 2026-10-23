package manageddb

import (
	"context"
	"errors"
	"fmt"

	"tubes-wbd/backend/database/targetdb"
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