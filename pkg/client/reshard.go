package client

import (
	"context"
	"net/http"
)

// GetReshard returns global resharding status/configuration.
func (c *Client) GetReshard(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_reshard", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetReshardState returns the resharding state.
func (c *Client) GetReshardState(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_reshard/state", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// PutReshardState sets the resharding state (e.g. {"state":"stopped"}).
func (c *Client) PutReshardState(ctx context.Context, state map[string]interface{}) error {
	return c.do(ctx, http.MethodPut, "/_reshard/state", state, nil)
}

// ListReshardJobs lists resharding jobs.
func (c *Client) ListReshardJobs(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_reshard/jobs", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// CreateReshardJob submits a new resharding job.
func (c *Client) CreateReshardJob(ctx context.Context, job map[string]interface{}) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodPost, "/_reshard/jobs", job, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}
