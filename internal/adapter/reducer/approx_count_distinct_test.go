package reducer

import (
	"fmt"
	"math"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApproxCountDistinct_Basic(t *testing.T) {
	r := NewApproxCountDistinct()
	n := 10000
	for i := 0; i < n; i++ {
		r.Reduce(&model.Document{Key: nil, Value: fmt.Sprintf("key-%d", i)})
	}
	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	est := doc.Value.(int64)
	// HyperLogLog with 2048 registers should be within ~5% of the true count.
	assert.InDelta(t, float64(n), float64(est), float64(n)*0.05,
		"estimated %d, expected ~%d", est, n)
}

func TestApproxCountDistinct_Duplicates(t *testing.T) {
	r := NewApproxCountDistinct()
	// Insert 100 distinct keys, each repeated 10 times.
	for repeat := 0; repeat < 10; repeat++ {
		for i := 0; i < 100; i++ {
			r.Reduce(&model.Document{Key: nil, Value: fmt.Sprintf("dup-%d", i)})
		}
	}
	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	est := doc.Value.(int64)
	assert.InDelta(t, 100.0, float64(est), 10.0,
		"estimated %d, expected ~100", est)
}

func TestApproxCountDistinct_Groups(t *testing.T) {
	r := NewApproxCountDistinct()
	for i := 0; i < 500; i++ {
		r.Reduce(&model.Document{Key: "groupA", Value: fmt.Sprintf("a-%d", i)})
	}
	for i := 0; i < 200; i++ {
		r.Reduce(&model.Document{Key: "groupB", Value: fmt.Sprintf("b-%d", i)})
	}
	result := r.Result()
	require.Len(t, result, 2)

	docA := result[0].(*model.Document)
	assert.Equal(t, "groupA", docA.Key)
	estA := docA.Value.(int64)
	assert.InDelta(t, 500.0, float64(estA), 500.0*0.05)

	docB := result[1].(*model.Document)
	assert.Equal(t, "groupB", docB.Key)
	estB := docB.Value.(int64)
	assert.InDelta(t, 200.0, float64(estB), 200.0*0.10) // slightly more lenient for smaller set
}

func TestApproxCountDistinct_SingleKey(t *testing.T) {
	r := NewApproxCountDistinct()
	r.Reduce(&model.Document{Key: nil, Value: "only-key"})
	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	est := doc.Value.(int64)
	assert.Equal(t, int64(1), est)
}

func TestApproxCountDistinct_NumericKeys(t *testing.T) {
	r := NewApproxCountDistinct()
	for i := 0; i < 1000; i++ {
		r.Reduce(&model.Document{Key: nil, Value: i})
	}
	result := r.Result()
	require.Len(t, result, 1)
	doc := result[0].(*model.Document)
	est := doc.Value.(int64)
	assert.InDelta(t, 1000.0, float64(est), 1000.0*0.05)
}

func TestHLLEstimate_AllZeros(t *testing.T) {
	// All-zero registers should estimate 0 using linear counting (m * ln(m/m) = 0).
	registers := make([]uint8, hllM)
	est := hllEstimate(registers)
	// m * ln(m/m) = m * ln(1) = 0
	assert.InDelta(t, 0.0, est, 1.0)
}

func TestHLLEstimate_AllMax(t *testing.T) {
	// All registers set to a high value → large cardinality.
	registers := make([]uint8, hllM)
	for i := range registers {
		registers[i] = 20
	}
	est := hllEstimate(registers)
	assert.True(t, est > 0 && !math.IsInf(est, 0), "estimate should be finite positive")
}
