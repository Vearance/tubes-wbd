package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash string, password string) error
}

type BcryptHasher struct {
	Cost int
}

func NewBcryptHasher(cost int) BcryptHasher {
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}

	return BcryptHasher{Cost: cost}
}

func (h BcryptHasher) Hash(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.Cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

func (h BcryptHasher) Compare(hash string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
