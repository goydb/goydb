package model

type Security struct {
	Members Members `json:"members"`
	Admins  Admins  `json:"admins"`
}

type Members struct {
	Roles []string `json:"roles"`
	Names []string `json:"names"`
}

type Admins struct {
	Roles []string `json:"roles"`
	Names []string `json:"names"`
}

func DefaultSecurity() *Security {
	return &Security{
		Admins: Admins{
			Roles: []string{"_admin"},
		},
		Members: Members{
			Roles: []string{"_admin"},
		},
	}
}
