package main

import (
	"context"
	"net/http"
	"os"

	adapterlogger "github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/pkg/goydb"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/public"
	"github.com/goydb/utils"
)

func main() {
	// Create bootstrap logger for startup errors
	bootstrapLogger := adapterlogger.New(model.LogLevelInfo, os.Stdout)

	cfg, err := goydb.NewConfig()
	if err != nil {
		bootstrapLogger.Errorf(context.Background(), "config initialization failed", "error", err)
		os.Exit(1)
	}

	cfg.ParseFlags()
	cfg.Containers = []public.Container{
		utils.Fauxton{},
	}

	gdb, err := cfg.BuildDatabase()
	if err != nil {
		bootstrapLogger.Errorf(context.Background(), "database build failed", "error", err)
		os.Exit(1)
	}

	// Override listen address from persisted config (e.g. after PUT /_config/httpd/port).
	if addr, ok := gdb.Config.Get("httpd", "bind_address"); ok {
		if port, ok2 := gdb.Config.Get("httpd", "port"); ok2 && port != "" {
			if addr == "" {
				cfg.ListenAddress = ":" + port
			} else {
				cfg.ListenAddress = addr + ":" + port
			}
		}
	}

	defer gdb.Storage.Close()

	loggedRouter := adapterlogger.NewHTTPLoggingMiddleware(gdb.Handler, gdb.Logger)

	// Use configured logger for runtime info
	gdb.Logger.Infof(context.Background(), "server starting", "address", cfg.ListenAddress)
	err = http.ListenAndServe(cfg.ListenAddress, loggedRouter)
	if err != nil {
		gdb.Logger.Errorf(context.Background(), "server failed", "error", err)
		os.Exit(1)
	}
}
