package handler

import "sort"

// registeredFeatures is the dynamic feature registry. Features are appended
// via init() functions in build-tagged files so that the GET / response
// reflects exactly the features compiled into the binary.
var registeredFeatures []string

// RegisterFeature appends one or more feature names to the registry.
func RegisterFeature(names ...string) {
	registeredFeatures = append(registeredFeatures, names...)
}

// Features returns a sorted copy of all registered feature names.
func Features() []string {
	out := make([]string, len(registeredFeatures))
	copy(out, registeredFeatures)
	sort.Strings(out)
	return out
}
