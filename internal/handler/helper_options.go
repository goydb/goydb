package handler

import (
	"net/url"
	"strconv"
	"time"
)

func intOption(name string, fallback int64, options url.Values) int64 {
	if len(options[name]) == 0 {
		return fallback
	}
	v, err := strconv.ParseInt(options[name][0], 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func boolOption(name string, fallback bool, options url.Values) bool {
	if len(options[name]) == 0 {
		return fallback
	}
	if options[name][0] == "" {
		return fallback
	}
	return options[name][0] == "true"
}

func stringOption(name string, alias string, options url.Values) string {
	if len(options[name]) > 0 {
		return options[name][0]
	}
	if len(options[alias]) > 0 {
		return options[alias][0]
	}
	return ""
}

func durationOption(name string, unit, fallback time.Duration, options url.Values) time.Duration {
	v := intOption(name, -1, options)
	if v < 0 {
		return fallback
	}
	return time.Duration(v) * unit
}
