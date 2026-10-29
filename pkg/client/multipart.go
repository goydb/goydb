package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

// MultipartAttachment is one inline attachment part of a PutMultipart
// request.
type MultipartAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// PutMultipart creates or updates a document together with one or more
// inline attachments in a single multipart/related request, mirroring what
// CouchDB/goydb's PUT /{db}/{docid} accepts when sent with a
// "multipart/related" Content-Type: the first part is the document's JSON
// body, each subsequent part is one attachment's raw bytes. Returns the new
// revision.
func (d *Database) PutMultipart(ctx context.Context, doc *model.Document, attachments []MultipartAttachment) (rev string, err error) {
	body, contentType, err := buildMultipartBody(doc, attachments)
	if err != nil {
		return "", err
	}

	resp, err := d.Raw(ctx, http.MethodPut, docPath("", doc.ID), body, map[string]string{"Content-Type": contentType})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if err := checkStatus(resp); err != nil {
		return "", err
	}
	var out SimpleDocResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("client: decoding response: %w", err)
	}
	return out.Rev, nil
}

func buildMultipartBody(doc *model.Document, attachments []MultipartAttachment) (*bytes.Buffer, string, error) {
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)

	docBody := make(map[string]interface{}, len(doc.Data)+1)
	for k, v := range doc.Data {
		docBody[k] = v
	}
	docBody["_id"] = doc.ID
	if doc.Rev != "" {
		docBody["_rev"] = doc.Rev
	}

	jsonPart, err := mw.CreatePart(map[string][]string{"Content-Type": {"application/json"}})
	if err != nil {
		return nil, "", err
	}
	if err := json.NewEncoder(jsonPart).Encode(docBody); err != nil {
		return nil, "", err
	}

	for _, att := range attachments {
		header := map[string][]string{
			"Content-Type":        {att.ContentType},
			"Content-Disposition": {fmt.Sprintf(`attachment; filename=%q`, att.Filename)},
		}
		part, err := mw.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.Copy(part, bytes.NewReader(att.Data)); err != nil {
			return nil, "", err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf, "multipart/related; boundary=" + mw.Boundary(), nil
}
