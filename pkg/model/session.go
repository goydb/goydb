package model

type Session struct {
	Name  string   `json:"name,omitempty"`
	Roles []string `json:"roles"`
}

func (s Session) Authenticated() bool {
	return s.Name != ""
}

func (s Session) IsServerAdmin() bool {
	for _, role := range s.Roles {
		if role == RoleServerAdmin {
			return true
		}
	}
	return false
}

func (s Session) Store(values map[interface{}]interface{}) {
	values["name"] = s.Name
	values["roles"] = s.Roles
}

func (s *Session) Restore(values map[interface{}]interface{}) {
	if name, ok := values["name"].(string); ok {
		s.Name = name
	}
	if roles, ok := values["roles"].([]string); ok {
		s.Roles = roles
	}
}
