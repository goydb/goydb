package gojaview

import (
	"context"
	"net/url"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewServer_Process(t *testing.T) {
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
			script: `function(doc) {}`,
			docs: []*model.Document{
				&model.Document{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"test": 1,
				}},
			},
			want:    []*model.Document{},
			wantErr: false,
		},
		{
			name: "one emit",
			script: `function(doc) {
				emit(doc.test, 1)
			}`,
			options: url.Values{},
			docs: []*model.Document{
				&model.Document{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"test": 1,
				}},
			},
			want: []*model.Document{&model.Document{
				ID:    "1",
				Key:   int64(1),
				Value: int64(1),
			}},
			wantErr: false,
		},
		{
			name: "two emit",
			script: `function(doc) {
				emit(doc._id, 1)
			}`,
			options: url.Values{},
			docs: []*model.Document{
				&model.Document{ID: "1", Rev: "0-REV", Data: map[string]interface{}{
					"test": 1,
				}},
				&model.Document{ID: "2", Rev: "0-REV", Data: map[string]interface{}{
					"test": 123,
				}},
			},
			want: []*model.Document{&model.Document{
				ID:    "1",
				Key:   "1",
				Value: int64(1),
			}, &model.Document{
				ID:    "2",
				Key:   "2",
				Value: int64(1),
			}},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewViewServer(tt.script)
			assert.NoError(t, err)
			got, err := s.Process(context.Background(), tt.docs)
			if err != nil && !tt.wantErr {
				require.NoError(t, err)
			}

			assert.EqualValues(t, tt.want, got)
		})
	}
}
