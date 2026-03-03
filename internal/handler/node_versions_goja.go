//go:build !nogoja

package handler

func init() {
	RegisterJSEngineInfo(map[string]interface{}{
		"name":    "goja",
		"version": "0.0.0",
	})
}
