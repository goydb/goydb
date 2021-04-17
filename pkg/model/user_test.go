package model

import (
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserGeneratePBKDF2(t *testing.T) {
	u := User{Password: "test"}
	err := u.GeneratePBKDF2()
	assert.NoError(t, err)
	assert.Equal(t, u.Iterations, 4096)
	assert.NotEmpty(t, u.Salt)
	assert.NotEmpty(t, u.DerivedKey)
	assert.Empty(t, u.Password)
	log.Printf("%#v", u)
}

func TestUserVerifyPassword(t *testing.T) {
	u := User{
		Iterations: 4096,
		DerivedKey: "801e3bd2caf360bbc9a3b2e4d6acb16337ed34c8fe6dbb080f82c89d1afc3614",
		Salt:       "82d3004c96fab18065cb37e82b63800c0ad5ef857012ed3c1c1b0dee25c6aa77",
	}
	ok, err := u.VerifyPassword("test")
	assert.NoError(t, err)
	assert.True(t, ok)
}
