package reducer

import (
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStats_Basic(t *testing.T) {
	r := NewStats()
	for _, v := range []float64{1, 2, 3, 4, 5} {
		r.Reduce(&model.Document{Key: nil, Value: v})
	}
	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	stats := doc.Value.(map[string]interface{})
	assert.InDelta(t, 15.0, stats["sum"], 0.001)
	assert.InDelta(t, 5.0, stats["count"], 0.001)
	assert.InDelta(t, 1.0, stats["min"], 0.001)
	assert.InDelta(t, 5.0, stats["max"], 0.001)
	assert.InDelta(t, 55.0, stats["sumsqr"], 0.001) // 1+4+9+16+25
}

func TestStats_GroupKeys(t *testing.T) {
	r := NewStats()
	r.Reduce(&model.Document{Key: "a", Value: float64(10)})
	r.Reduce(&model.Document{Key: "a", Value: float64(20)})
	r.Reduce(&model.Document{Key: "b", Value: float64(5)})

	result := r.Result()
	require.Len(t, result, 2)

	docA := result[0].(*model.Document)
	assert.Equal(t, "a", docA.Key)
	statsA := docA.Value.(map[string]interface{})
	assert.InDelta(t, 30.0, statsA["sum"], 0.001)
	assert.InDelta(t, 2.0, statsA["count"], 0.001)
	assert.InDelta(t, 10.0, statsA["min"], 0.001)
	assert.InDelta(t, 20.0, statsA["max"], 0.001)

	docB := result[1].(*model.Document)
	assert.Equal(t, "b", docB.Key)
	statsB := docB.Value.(map[string]interface{})
	assert.InDelta(t, 5.0, statsB["sum"], 0.001)
	assert.InDelta(t, 1.0, statsB["count"], 0.001)
}

func TestStats_IntValues(t *testing.T) {
	r := NewStats()
	r.Reduce(&model.Document{Key: nil, Value: int64(3)})
	r.Reduce(&model.Document{Key: nil, Value: int64(7)})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	stats := doc.Value.(map[string]interface{})
	assert.InDelta(t, 10.0, stats["sum"], 0.001)
	assert.InDelta(t, 2.0, stats["count"], 0.001)
	assert.InDelta(t, 3.0, stats["min"], 0.001)
	assert.InDelta(t, 7.0, stats["max"], 0.001)
	assert.InDelta(t, 58.0, stats["sumsqr"], 0.001) // 9+49
}

func TestStats_SingleValue(t *testing.T) {
	r := NewStats()
	r.Reduce(&model.Document{Key: nil, Value: float64(42)})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	stats := doc.Value.(map[string]interface{})
	assert.InDelta(t, 42.0, stats["sum"], 0.001)
	assert.InDelta(t, 1.0, stats["count"], 0.001)
	assert.InDelta(t, 42.0, stats["min"], 0.001)
	assert.InDelta(t, 42.0, stats["max"], 0.001)
	assert.InDelta(t, 1764.0, stats["sumsqr"], 0.001)
}
