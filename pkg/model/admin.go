package model

import (
	"fmt"
	"strings"
)

const RoleServerAdmin = "_admin"

type AdminUser struct {
	Username string
	Password string
}

func (u AdminUser) String() string {
	return "<AdminUser Username=" + u.Username + ">"
}

func (u AdminUser) Session() *Session {
	return &Session{
		Name:  u.Username,
		Roles: []string{RoleServerAdmin},
	}
}

type AdminUsers []AdminUser

func (a AdminUsers) Authenticate(username, password string) *AdminUser {
	for _, user := range a {
		if user.Username == username && user.Password == password {
			return &user
		}
	}
	return nil
}

func ParseAdmins(admins string) (AdminUsers, error) {
	userParts := strings.Split(admins, ",")
	users := make(AdminUsers, len(userParts))
	if len(userParts) == 0 {
		return nil, fmt.Errorf("invalid admins string")
	}

	for i, userPart := range userParts {
		userPass := strings.Split(userPart, ":")
		if len(userPass) <= 1 {
			return nil, fmt.Errorf("invalid admins string part %d", i+1)
		}
		users[i].Username = userPass[0]
		users[i].Password = strings.Join(userPass[1:], ":")
	}

	return users, nil
}
