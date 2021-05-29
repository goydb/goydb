package model

import "time"

type ChangesOptions struct {
	Since   string
	Limit   int
	Timeout time.Duration
}

func (o *ChangesOptions) SinceNow() bool {
	return o.Since == "now"
}
