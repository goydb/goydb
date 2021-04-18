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
	assert.Equal(t, u.Iterations, 100000)
	assert.NotEmpty(t, u.Salt)
	assert.NotEmpty(t, u.DerivedKey)
	assert.Empty(t, u.Password)
	log.Printf("%#v", u)
}

func TestUserVerifyPassword(t *testing.T) {
	u := User{
		Iterations: 100000,
		DerivedKey: "038a9dbe6d0fd90e38dfd829c7920b01755b7d96",
		Salt:       "820af19388be1b9ec2e22f730e452c75",
	}
	ok, err := u.VerifyPassword("test")
	assert.NoError(t, err)
	assert.True(t, ok)
}
