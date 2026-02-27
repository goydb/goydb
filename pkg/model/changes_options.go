package model

import "time"

type ChangesOptions struct {
	Since     string
	Limit     int
	Timeout   time.Duration
	Heartbeat time.Duration
	DocIDs    []string
	Feed      string // "normal", "longpoll", "continuous", "eventsource"

	// Filter fields
	Filter   string                 // Filter type: "_selector", "_view", "_design/ddoc/filtername"
	Selector map[string]interface{} // For filter=_selector
	View     string                 // For filter=_view: "_design/ddoc/viewname"
}

func (o *ChangesOptions) SinceNow() bool {
	return o.Since == "now"
}
