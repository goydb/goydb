package client

import (
	"context"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

// Replicate triggers a one-shot or continuous replication job via
// POST /_replicate.
func (c *Client) Replicate(ctx context.Context, doc *model.ReplicationDoc) (*model.ReplicationResult, error) {
	var result model.ReplicationResult
	if err := c.do(ctx, http.MethodPost, "/_replicate", doc, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SchedulerJobs lists currently scheduled replication jobs.
func (c *Client) SchedulerJobs(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_scheduler/jobs", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// SchedulerDocs lists replication documents known to the scheduler.
func (c *Client) SchedulerDocs(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.do(ctx, http.MethodGet, "/_scheduler/docs", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}
