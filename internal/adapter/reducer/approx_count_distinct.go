package reducer

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"math/bits"
	"reflect"

	"github.com/goydb/goydb/pkg/model"
)

const (
	hllP = 11        // precision: 2^11 = 2048 registers
	hllM = 1 << hllP // number of registers
)

// ApproxCountDistinct implements CouchDB's _approx_count_distinct built-in
// reduce function using the HyperLogLog algorithm with 2048 registers
// (~2% relative error).
type ApproxCountDistinct struct {
	keys      []interface{}
	registers [][]uint8 // one set of HLL registers per group key
}

func NewApproxCountDistinct() *ApproxCountDistinct {
	return &ApproxCountDistinct{}
}

func (r *ApproxCountDistinct) indexOf(key interface{}) int {
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

// Reduce processes a document. doc.Key is the grouped key (for bucketing),
// doc.Value is the original emitted key (set by the controller).
func (r *ApproxCountDistinct) Reduce(doc *model.Document) {
	h := hashValue(doc.Value)
	idx := r.indexOf(doc.Key)
	if idx < 0 {
		r.keys = append(r.keys, doc.Key)
		regs := make([]uint8, hllM)
		r.registers = append(r.registers, regs)
		idx = len(r.keys) - 1
	}
	// Use the first hllP bits to select the register.
	j := h >> (64 - hllP)
	// Count leading zeros of remaining bits + 1.
	w := (h << hllP) | (1 << (hllP - 1)) // guard bit to avoid 64 leading zeros
	rho := uint8(bits.LeadingZeros64(w) + 1)
	if rho > r.registers[idx][j] {
		r.registers[idx][j] = rho
	}
}

func (r *ApproxCountDistinct) Result() map[interface{}]interface{} {
	out := make(map[interface{}]interface{}, len(r.keys))
	for i, k := range r.keys {
		est := hllEstimate(r.registers[i])
		out[i] = &model.Document{Key: k, Value: int64(est + 0.5)} // round to nearest int
	}
	return out
}

// hllEstimate computes the HyperLogLog cardinality estimate from registers.
func hllEstimate(registers []uint8) float64 {
	m := float64(hllM)
	// Compute harmonic mean of 2^(-register[j]).
	var sum float64
	zeros := 0
	for _, val := range registers {
		sum += math.Pow(2, -float64(val))
		if val == 0 {
			zeros++
		}
	}
	// alpha_m constant for bias correction.
	alpha := 0.7213 / (1 + 1.079/m)
	estimate := alpha * m * m / sum

	// Small range correction: use linear counting if estimate is small
	// and there are empty registers.
	if estimate <= 2.5*m && zeros > 0 {
		estimate = m * math.Log(m/float64(zeros))
	}

	return estimate
}

// hashValue serializes the value to JSON and hashes it with FNV-1a 64-bit,
// then applies the splitmix64 finalizer for better bit distribution across
// all 64 bits (critical for HLL register selection from the top bits).
func hashValue(v interface{}) uint64 {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte("null")
	}
	h := fnv.New64a()
	h.Write(data)
	x := h.Sum64()
	// splitmix64 finalizer — ensures uniform distribution of top bits.
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}
