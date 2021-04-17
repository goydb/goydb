package model

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"

	"github.com/mitchellh/mapstructure"
	"golang.org/x/crypto/pbkdf2"
)

type User struct {
	Name           string   `mapstructure:"name" json:"name"`
	Roles          []string `mapstructure:"roles" json:"roles"`
	Type           string   `mapstructure:"type" json:"type"`
	Password       string   `mapstructure:"password" json:"password"`
	PasswordScheme string   `mapstructure:"password_scheme" json:"password_scheme"`
	Iterations     int      `mapstructure:"iterations" json:"iterations"`
	DerivedKey     string   `mapstructure:"derived_key" json:"derived_key"`
	Salt           string   `mapstructure:"salt" json:"salt"`
}

var (
	userHash           = sha1.New
	userHashIterations = 20
)

func (u User) Session() *Session {
	s := &Session{
		Name:  u.Name,
		Roles: u.Roles,
	}
	if s.Roles == nil {
		s.Roles = make([]string, 0, 1)
	}
	return s
}

func (u *User) FromDocument(doc *Document) error {
	return mapstructure.Decode(doc.Data, u)
}

// Verify the PBKDF2 using a couchdb compatible way
func (u *User) VerifyPassword(password string) (bool, error) {
	key, err := hex.DecodeString(u.DerivedKey)
	if err != nil {
		return false, err
	}
	dk := pbkdf2.Key([]byte(password), []byte(u.Salt), u.Iterations, userHashIterations, userHash)
	return bytes.Compare(key, dk) == 0, nil
}

// GeneratePBKDF2 generates a pbkdf in a couchdb compatible fashion
func (u *User) GeneratePBKDF2() error {
	u.Iterations = 100000
	var salt [16]byte
	_, err := rand.Read(salt[:])
	if err != nil {
		return err
	}
	u.Salt = hex.EncodeToString(salt[:])
	dk := pbkdf2.Key([]byte(u.Password), []byte(u.Salt), u.Iterations, userHashIterations, userHash)
	u.DerivedKey = hex.EncodeToString(dk)
	u.Password = ""
	return nil
}
