//go:build nogoja

package reducer

import (
	"math"
	"reflect"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type Stats struct {
	keys   []interface{}
	stats  []statsAccum
}

type statsAccum struct {
	sum    float64
	min    float64
	max    float64
	count  int64
	sumsqr float64
}

func NewStats() port.Reducer {
	return &Stats{}
}

func (r *Stats) indexOf(key interface{}) int {
	n := len(r.keys)
	if n > 0 && reflect.DeepEqual(r.keys[n-1], key) {
		return n - 1
	}
	for i, k := range r.keys {
		if reflect.DeepEqual(k, key) {
			return i
		}
	}
	return -1
}

func (r *Stats) Reduce(doc *model.Document) {
	v := toFloat64(doc.Value)
	idx := r.indexOf(doc.Key)
	if idx >= 0 {
		s := &r.stats[idx]
		s.sum += v
		if v < s.min {
			s.min = v
		}
		if v > s.max {
			s.max = v
		}
		s.count++
		s.sumsqr += v * v
	} else {
		r.keys = append(r.keys, doc.Key)
		r.stats = append(r.stats, statsAccum{
			sum:    v,
			min:    v,
			max:    v,
			count:  1,
			sumsqr: v * v,
		})
	}
}

func (r *Stats) Result() map[interface{}]interface{} {
	out := make(map[interface{}]interface{}, len(r.keys))
	for i, k := range r.keys {
		s := r.stats[i]
		out[i] = &model.Document{
			Key: k,
			Value: map[string]interface{}{
				"sum":    s.sum,
				"min":    s.min,
				"max":    s.max,
				"count":  s.count,
				"sumsqr": s.sumsqr,
			},
		}
	}
	return out
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	default:
		return math.NaN()
	}
}
