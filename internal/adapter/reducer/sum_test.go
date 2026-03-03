package reducer

import (
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSum_ScalarInt(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: int64(10)})
	r.Reduce(&model.Document{Key: nil, Value: int64(20)})
	r.Reduce(&model.Document{Key: nil, Value: int64(30)})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	assert.Equal(t, int64(60), doc.Value)
}

func TestSum_ScalarFloat(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: float64(1.5)})
	r.Reduce(&model.Document{Key: nil, Value: float64(2.5)})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	assert.InDelta(t, 4.0, doc.Value, 0.001)
}

func TestSum_MixedIntFloat(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: int64(10)})
	r.Reduce(&model.Document{Key: nil, Value: float64(0.5)})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	assert.InDelta(t, 10.5, doc.Value, 0.001)
}

func TestSum_ArrayElementWise(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{int64(1), int64(2), int64(3)}})
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{int64(4), int64(5), int64(6)}})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	arr, ok := doc.Value.([]interface{})
	require.True(t, ok, "result should be an array")
	require.Len(t, arr, 3)
	assert.Equal(t, int64(5), arr[0])
	assert.Equal(t, int64(7), arr[1])
	assert.Equal(t, int64(9), arr[2])
}

func TestSum_ArrayDifferentLengths(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{int64(1), int64(2), int64(3)}})
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{int64(4), int64(5)}})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	arr, ok := doc.Value.([]interface{})
	require.True(t, ok, "result should be an array")
	require.Len(t, arr, 3)
	assert.Equal(t, int64(5), arr[0])
	assert.Equal(t, int64(7), arr[1])
	assert.Equal(t, int64(3), arr[2]) // padded with 0
}

func TestSum_ArrayWithFloats(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{float64(1.5), int64(2)}})
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{float64(2.5), int64(3)}})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	arr, ok := doc.Value.([]interface{})
	require.True(t, ok, "result should be an array")
	require.Len(t, arr, 2)
	assert.InDelta(t, 4.0, arr[0], 0.001)
	assert.Equal(t, int64(5), arr[1])
}

func TestSum_ScalarPlusArray(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: int64(10)})
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{int64(5), int64(3)}})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	arr, ok := doc.Value.([]interface{})
	require.True(t, ok, "result should be an array")
	require.Len(t, arr, 2)
	assert.Equal(t, int64(15), arr[0])
	assert.Equal(t, int64(3), arr[1])
}

func TestSum_ArrayPlusScalar(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: nil, Value: []interface{}{int64(5), int64(3)}})
	r.Reduce(&model.Document{Key: nil, Value: int64(10)})

	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	arr, ok := doc.Value.([]interface{})
	require.True(t, ok, "result should be an array")
	require.Len(t, arr, 2)
	assert.Equal(t, int64(15), arr[0])
	assert.Equal(t, int64(3), arr[1])
}

func TestSum_GroupedArrays(t *testing.T) {
	r := NewSum()
	r.Reduce(&model.Document{Key: "a", Value: []interface{}{int64(1), int64(2)}})
	r.Reduce(&model.Document{Key: "a", Value: []interface{}{int64(3), int64(4)}})
	r.Reduce(&model.Document{Key: "b", Value: []interface{}{int64(10), int64(20)}})

	result := r.Result()
	require.Len(t, result, 2)

	docA := result[0].(*model.Document)
	assert.Equal(t, "a", docA.Key)
	arrA := docA.Value.([]interface{})
	assert.Equal(t, int64(4), arrA[0])
	assert.Equal(t, int64(6), arrA[1])

	docB := result[1].(*model.Document)
	assert.Equal(t, "b", docB.Key)
	arrB := docB.Value.([]interface{})
	assert.Equal(t, int64(10), arrB[0])
	assert.Equal(t, int64(20), arrB[1])
}
