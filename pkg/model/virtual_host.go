package model

import (
	"fmt"
	"net/http"
)

type ProxyType string

const (
	ProxyDB      ProxyType = "db"
	ProxyReverse ProxyType = "reverse"
)

type VirtualHostConfiguration struct {
	Name    string
	Rev     string
	Domains []string         `mapstructure:"domains" json:"domains"`
	Proxy   map[string]Proxy `mapstructure:"proxy" json:"proxy"`
	Static  string           `mapstructure:"static" json:"static"`
	FS      http.FileSystem
}

func (c VirtualHostConfiguration) String() string {
	return fmt.Sprintf("<Proxy type=%v proxy=%v static=%v>", c.Domains, c.Proxy, c.Static)
}

func (c VirtualHostConfiguration) Open(name string) (http.File, error) {
	return nil, nil
}

type Proxy struct {
	Type        ProxyType `mapstructure:"type" json:"type"`
	Target      string    `mapstructure:"target" json:"target"`
	StripPrefix bool      `mapstructure:"stripPrefix" json:"stripPrefix"`
}

func (p Proxy) String() string {
	return fmt.Sprintf("<Proxy type=%q target=%q>", p.Type, p.Target)
}
