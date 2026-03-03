package handler

var jsEngineInfo map[string]interface{}

// RegisterJSEngineInfo sets the JavaScript engine information
// displayed by the /_node/{node}/_versions endpoint.
func RegisterJSEngineInfo(info map[string]interface{}) {
	jsEngineInfo = info
}
