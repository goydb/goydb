package port

import "github.com/goydb/goydb/pkg/model"

type SessionBuilder interface {
	Session() *model.Session
}
