package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/goydb/goydb/pkg/model"
)

func changesQuery(opts *model.ChangesOptions) url.Values {
	v := url.Values{}
	if opts == nil {
		return v
	}
	if opts.Since != "" {
		v.Set("since", opts.Since)
	}
	if opts.Limit > 0 {
		v.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Timeout > 0 {
		v.Set("timeout", strconv.FormatInt(opts.Timeout.Milliseconds(), 10))
	}
	if opts.Heartbeat > 0 {
		v.Set("heartbeat", strconv.FormatInt(opts.Heartbeat.Milliseconds(), 10))
	}
	if opts.Feed != "" {
		v.Set("feed", opts.Feed)
	}
	if opts.Descending {
		v.Set("descending", "true")
	}
	if opts.Style != "" {
		v.Set("style", opts.Style)
	}
	if opts.Filter != "" {
		v.Set("filter", opts.Filter)
	}
	return v
}

// Changes requests the changes feed with feed=normal (the default) or
// feed=longpoll (set via opts.Feed), returning the full batch of results.
// For "continuous"/"eventsource" streaming feeds, use ChangesContinuous
// instead.
func (d *Database) Changes(ctx context.Context, opts *model.ChangesOptions) (*model.ChangesResponse, error) {
	path := d.path("/_changes")
	if v := changesQuery(opts); len(v) > 0 {
		path += "?" + v.Encode()
	}

	var resp model.ChangesResponse
	if err := d.client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ChangesContinuous streams the changes feed (feed=continuous) and invokes
// fn for each change as it arrives, until ctx is cancelled, fn returns an
// error, or the server closes the connection. opts.Feed is forced to
// "continuous".
func (d *Database) ChangesContinuous(ctx context.Context, opts *model.ChangesOptions, fn func(model.ChangeResult) error) error {
	o := *opts
	o.Feed = "continuous"

	path := d.path("/_changes") + "?" + changesQuery(&o).Encode()
	resp, err := d.client.request(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if err := checkStatus(resp); err != nil {
		return err
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var change model.ChangeResult
		if err := json.Unmarshal(line, &change); err != nil {
			return fmt.Errorf("client: decoding change: %w", err)
		}
		if err := fn(change); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// ChangesEventSource streams the changes feed as Server-Sent Events
// (feed=eventsource, "data: {...}\n\n" framing) and invokes fn for each
// change as it arrives, until ctx is cancelled, fn returns an error, or the
// server closes the connection. opts.Feed is forced to "eventsource".
func (d *Database) ChangesEventSource(ctx context.Context, opts *model.ChangesOptions, fn func(model.ChangeResult) error) error {
	o := *opts
	o.Feed = "eventsource"

	path := d.path("/_changes") + "?" + changesQuery(&o).Encode()
	resp, err := d.client.request(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if err := checkStatus(resp); err != nil {
		return err
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue // blank separator line, or an "event:"/"id:" field
		}
		var change model.ChangeResult
		if err := json.Unmarshal([]byte(data), &change); err != nil {
			return fmt.Errorf("client: decoding change: %w", err)
		}
		if err := fn(change); err != nil {
			return err
		}
	}
	return scanner.Err()
}
