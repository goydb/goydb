package handler

import "github.com/gorilla/mux"

type routerHook func(r *mux.Router, b Base)

var routerHooks []routerHook

// RegisterRouterHook adds a hook that will be called during router setup
// to register additional routes. Used by build-tagged feature files.
func RegisterRouterHook(hook routerHook) {
	routerHooks = append(routerHooks, hook)
}
