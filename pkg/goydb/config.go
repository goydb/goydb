package goydb

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"

	"github.com/caarlos0/env/v6"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/internal/adapter/view/gojaview"
	"github.com/goydb/goydb/internal/adapter/view/tengoview"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/public"
)

type Config struct {
	// DatabaseDir directory path where the databases and
	// attachments should be stored
	DatabaseDir string `env:"GOYDB_DB_DIR" envDefault:"./dbs"`
	// PublicDir the public directory that should be served by
	// the default server implementation
	PublicDir string `env:"GOYDB_PUBLIC" envDefault:"./public"`
	// EnablePublicDir allows to enable/disable the public directory serving
	EnablePublicDir bool `env:"GOYDB_ENABLE_PUBLIC" envDefault:"true"`
	// ListenAddress contains the default address to listen to.
	// Not respected if the server is generated by the user.
	ListenAddress string `env:"GOYDB_LISTEN" envDefault:":7070"`
	// CookieSecret a secret that is used to verify the
	// integrity, usually generated using openssl rand -hex 32
	CookieSecret string `env:"GOYDB_SECRET" envDefault:"21ccf11d067cec8eebd663af3dc8785521d1cc366f0d533e6740c1cb6840dceb"`
	// Aministrators list of username:password sperated by ","
	// for multiple users
	Aministrators string `env:"GOYDB_ADMINS" envDefault:"admin:secret"`
	// Containers are zip file based containers that should be mounted before the
	// database application
	Containers []public.Container
}

// NewConfig will create a new configuration
// based on the given environment values.
// Errors while parsing the environment are returned.
func NewConfig() (*Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing the environment config: %v", err)
	}
	return &cfg, nil
}

// ParseFlags to change the configuration
func (c *Config) ParseFlags() {
	flag.StringVar(&c.DatabaseDir, "dbs", c.DatabaseDir, "directory with databases")
	flag.StringVar(&c.PublicDir, "public", c.PublicDir, "directory public data")
	flag.StringVar(&c.ListenAddress, "addr", c.ListenAddress, "listening address")
	flag.StringVar(&c.CookieSecret, "cookie-secret", c.CookieSecret, "secret for the cookies")
	flag.StringVar(&c.Aministrators, "admins", c.Aministrators, "admins for the databases")

	flag.Parse()
}

// BuildDatabase builds the storage and http handler based
// on the given configuration.
func (c *Config) BuildDatabase() (*Goydb, error) {
	var gdb Goydb

	secret, err := hex.DecodeString(c.CookieSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cookie secret: %w", err)
	}
	store := sessions.NewCookieStore(secret)

	admins, err := model.ParseAdmins(c.Aministrators)
	if err != nil {
		return nil, fmt.Errorf("failed to parse admins: %w", err)
	}

	s, err := storage.Open(
		c.DatabaseDir,
		storage.WithEngine("javascript", gojaview.NewViewServer),
		storage.WithEngine("tengo", tengoview.NewViewServer),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open database dir: %w", err)
	}
	tc := controller.Task{
		Storage: s,
	}
	go tc.Run(context.Background())
	gdb.Storage = s

	r := mux.NewRouter()
	for _, c := range c.Containers {
		public.MountContainer(r, c)
	}
	gdb.Handler = r

	if c.EnablePublicDir {
		err := public.Public{Dir: c.PublicDir}.Mount(r)
		if err != nil {
			return nil, fmt.Errorf("failed to mount public dir: %w", err)
		}
	}

	err = handler.Router{
		SessionStore: store,
		Storage:      s,
		Admins:       admins,
	}.Build(r)
	if err != nil {
		return nil, fmt.Errorf("failed to build router: %w", err)
	}

	return &gdb, nil
}
