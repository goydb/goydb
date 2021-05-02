package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_DesignDocFn(t *testing.T) {
	ddfn := DesignDocFn{
		Type:        SearchFn,
		DesignDocID: "_design/all",
		FnName:      "searchByDescription",
	}
	assert.Equal(t, "indexes:all:searchByDescription", ddfn.String())

	ddfn = DesignDocFn{
		Type:        ViewFn,
		DesignDocID: "_design/some",
		FnName:      "name",
	}
	assert.Equal(t, "views:some:name", ddfn.String())
}
