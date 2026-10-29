package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ViewOptions configures a Database.View call.
type ViewOptions struct {
	Key          interface{}
	Keys         []interface{}
	StartKey     interface{}
	EndKey       interface{}
	Limit        int
	Skip         int
	Descending   bool
	Reduce       *bool
	Group        bool
	GroupLevel   int
	IncludeDocs  bool
	InclusiveEnd *bool
}

func (o ViewOptions) values() url.Values {
	v := url.Values{}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Skip > 0 {
		v.Set("skip", strconv.Itoa(o.Skip))
	}
	if o.Descending {
		v.Set("descending", "true")
	}
	if o.Reduce != nil {
		v.Set("reduce", boolStr(*o.Reduce))
	}
	if o.Group {
		v.Set("group", "true")
	}
	if o.GroupLevel > 0 {
		v.Set("group_level", strconv.Itoa(o.GroupLevel))
	}
	if o.IncludeDocs {
		v.Set("include_docs", "true")
	}
	if o.InclusiveEnd != nil {
		v.Set("inclusive_end", boolStr(*o.InclusiveEnd))
	}
	setJSONParam(v, "key", o.Key)
	setJSONParam(v, "startkey", o.StartKey)
	setJSONParam(v, "endkey", o.EndKey)
	return v
}

func (d *Database) viewPath(designDoc, view string) string {
	return d.path("/_design/" + url.PathEscape(designDoc) + "/_view/" + url.PathEscape(view))
}

// View queries a view defined in a design document.
func (d *Database) View(ctx context.Context, designDoc, view string, opts ViewOptions) (*RowsResponse, error) {
	path := d.viewPath(designDoc, view)
	var resp RowsResponse
	var err error
	if len(opts.Keys) > 0 {
		err = d.client.do(ctx, http.MethodPost, path+"?"+opts.values().Encode(), map[string]interface{}{"keys": opts.Keys}, &resp)
	} else {
		if v := opts.values(); len(v) > 0 {
			path += "?" + v.Encode()
		}
		err = d.client.do(ctx, http.MethodGet, path, nil, &resp)
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// DesignDocInfo returns metadata about a design document's view group.
func (d *Database) DesignDocInfo(ctx context.Context, designDoc string) (map[string]interface{}, error) {
	var resp map[string]interface{}
	path := d.path("/_design/" + url.PathEscape(designDoc) + "/_info")
	if err := d.client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Show calls a design document's _show function.
func (d *Database) Show(ctx context.Context, designDoc, fn, docID string) ([]byte, error) {
	path := d.path("/_design/" + url.PathEscape(designDoc) + "/_show/" + url.PathEscape(fn))
	if docID != "" {
		path += "/" + url.PathEscape(docID)
	}
	return d.rawGet(ctx, path)
}

// List calls a design document's _list function over a view.
func (d *Database) List(ctx context.Context, designDoc, fn, view string) ([]byte, error) {
	path := d.path("/_design/" + url.PathEscape(designDoc) + "/_list/" + url.PathEscape(fn) + "/" + url.PathEscape(view))
	return d.rawGet(ctx, path)
}

// Update calls a design document's _update function.
func (d *Database) Update(ctx context.Context, designDoc, fn, docID string, body interface{}) ([]byte, error) {
	path := d.path("/_design/" + url.PathEscape(designDoc) + "/_update/" + url.PathEscape(fn))
	if docID != "" {
		path += "/" + url.PathEscape(docID)
	}
	return d.rawRequest(ctx, http.MethodPut, path, body)
}

// Rewrite invokes a design document's _rewrite handler for the given
// sub-path.
func (d *Database) Rewrite(ctx context.Context, designDoc, subPath string) ([]byte, error) {
	path := d.path("/_design/"+url.PathEscape(designDoc)+"/_rewrite/") + subPath
	return d.rawGet(ctx, path)
}

func (d *Database) rawGet(ctx context.Context, path string) ([]byte, error) {
	return d.rawRequest(ctx, http.MethodGet, path, nil)
}

func (d *Database) rawRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	resp, err := d.client.request(ctx, method, path, body, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if err := checkStatus(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}
