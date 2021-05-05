package tengoview

import (
	"context"
	"net/url"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewServer_ExecuteView(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		options url.Values
		docs    []*model.Document
		want    []*model.Document
		wantErr bool
	}{
		{
			name:   "empty emit",
			script: `func(doc) {}`,
			docs: []*model.Document{
				{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"test": 1,
				}},
			},
			want:    []*model.Document{},
			wantErr: false,
		},
		{
			name: "one emit",
			script: `func(doc) {
				emit(doc.test, 1)
			}`,
			options: url.Values{},
			docs: []*model.Document{
				{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"test": 1,
				}},
			},
			want: []*model.Document{
				{
					ID:    "1",
					Key:   int64(1),
					Value: int64(1),
				},
			},
			wantErr: false,
		},
		{
			name: "two emit",
			script: `func(doc) {
				emit(doc._id, 1)
			}`,
			options: url.Values{},
			docs: []*model.Document{
				{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"test": 1,
				}},
				{ID: "2", Rev: "0-REV", Data: map[string]interface{}{
					"test": 123,
				}},
			},
			want: []*model.Document{
				{
					ID:    "1",
					Key:   "1",
					Value: int64(1),
				}, {
					ID:    "2",
					Key:   "2",
					Value: int64(1),
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewViewServer(tt.script)
			assert.NoError(t, err)
			got, err := s.ExecuteView(context.Background(), tt.docs)
			if err != nil && !tt.wantErr {
				require.NoError(t, err)
			}

			assert.EqualValues(t, tt.want, got)
		})
	}
}

func TestViewServer_ExecuteSearch(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		options url.Values
		docs    []*model.Document
		want    []*model.SearchIndexDoc
		wantErr bool
	}{
		{
			name:   "empty emit",
			script: `func(doc) {}`,
			docs: []*model.Document{
				{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"test": 1,
				}},
			},
			want:    []*model.SearchIndexDoc{},
			wantErr: false,
		},
		{
			name: "one emit",
			script: `func(doc) {
				index("name", doc.name, { store: true })
				index("upcase", text.to_upper(doc.name), { store: true })
			}`,
			options: url.Values{},
			docs: []*model.Document{
				{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"name": "test",
				}},
			},
			want: []*model.SearchIndexDoc{{
				ID: "1",
				Fields: map[string]interface{}{
					"name":   "test",
					"upcase": "TEST",
				},
				Options: map[string]model.SearchIndexOption{
					"name":   {Store: true},
					"upcase": {Store: true},
				},
			}},
			wantErr: false,
		},
		{
			name: "two emit",
			script: `func(doc) {
				index("name", doc.name, {})
			}`,
			options: url.Values{},
			docs: []*model.Document{
				{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"name": "test",
				}},
				{ID: "2", Rev: "0-REV", Data: map[string]interface{}{
					"name": "test",
				}},
			},
			want: []*model.SearchIndexDoc{
				{
					ID: "1",
					Fields: map[string]interface{}{
						"name": "test",
					},
					Options: map[string]model.SearchIndexOption{
						"name": {},
					},
				},
				{
					ID: "2",
					Fields: map[string]interface{}{
						"name": "test",
					},
					Options: map[string]model.SearchIndexOption{
						"name": {},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewViewServer(tt.script)
			assert.NoError(t, err)
			got, err := s.ExecuteSearch(context.Background(), tt.docs)
			if err != nil && !tt.wantErr {
				require.NoError(t, err)
			}

			assert.EqualValues(t, tt.want, got)
		})
	}
}
