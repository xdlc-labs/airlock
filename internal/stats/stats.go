package stats

import (
	"math"
	"math/rand/v2"
	"slices"
)

// Interval is a confidence interval for a rate or delta.
type Interval struct {
	Estimate float64 `json:"estimate"`
	Low      float64 `json:"low"`
	High     float64 `json:"high"`
	N        int     `json:"n"`
	Level    float64 `json:"level"`
	Method   string  `json:"method,omitempty"` // wilson|bootstrap|normal
}

func WilsonCI(k, n int, level float64) Interval {
	if n <= 0 {
		return Interval{Level: level, Method: "wilson"}
	}
	if level <= 0 || level >= 1 {
		level = 0.95
	}
	z := zFor(level)
	p := float64(k) / float64(n)
	z2 := z * z
	den := 1 + z2/float64(n)
	center := (p + z2/(2*float64(n))) / den
	marg := (z / den) * math.Sqrt(p*(1-p)/float64(n)+z2/(4*float64(n)*float64(n)))
	return Interval{
		Estimate: p,
		Low:      clamp01(center - marg),
		High:     clamp01(center + marg),
		N:        n,
		Level:    level,
		Method:   "wilson",
	}
}

// PairedDeltaCI delegates to bootstrap.
func PairedDeltaCI(basePass, candPass []bool, level float64) Interval {
	return BootstrapPairedDeltaCI(basePass, candPass, level, 999, 42)
}

// BootstrapPairedDeltaCI percentile bootstrap on paired (cand-base) Bernoulli diffs.
func BootstrapPairedDeltaCI(basePass, candPass []bool, level float64, B int, seed uint64) Interval {
	n := min(len(basePass), len(candPass))
	if n == 0 {
		return Interval{Level: level, Method: "bootstrap"}
	}
	if level <= 0 || level >= 1 {
		level = 0.95
	}
	if B < 100 {
		B = 999
	}
	diffs := make([]float64, n)
	sum := 0.0
	for i := 0; i < n; i++ {
		b, c := 0.0, 0.0
		if basePass[i] {
			b = 1
		}
		if candPass[i] {
			c = 1
		}
		diffs[i] = c - b
		sum += diffs[i]
	}
	mean := sum / float64(n)

	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	boots := make([]float64, B)
	for bi := range B {
		s := 0.0
		for range n {
			s += diffs[rng.IntN(n)]
		}
		boots[bi] = s / float64(n)
	}
	slices.Sort(boots)
	alpha := (1 - level) / 2
	loIdx := int(math.Floor(alpha * float64(B)))
	hiIdx := int(math.Ceil((1-alpha)*float64(B))) - 1
	loIdx = max(0, loIdx)
	hiIdx = min(B-1, max(loIdx, hiIdx))
	return Interval{
		Estimate: mean,
		Low:      boots[loIdx],
		High:     boots[hiIdx],
		N:        n,
		Level:    level,
		Method:   "bootstrap",
	}
}

// SamplesToClearMin returns the smallest sample count whose Wilson lower bound
// can reach minRate, assuming a flawless run. The Wilson lower bound for k==n
// is n/(n+z^2), so a high min needs far more samples than people expect: a
// 0.99 gate cannot resolve below ~381 samples no matter how clean the run is,
// and under that it sits at INCONCLUSIVE forever. Any observed failure pushes
// the real requirement higher, so treat this as a floor.
func SamplesToClearMin(minRate, level float64) int {
	if minRate <= 0 {
		return 1
	}
	if minRate >= 1 {
		return 0
	}
	z := zFor(level)
	n := minRate * z * z / (1 - minRate)
	return int(math.Ceil(n))
}

func PassesMin(ci Interval, minRate float64) bool {
	return ci.Low >= minRate
}

func FailsMin(ci Interval, minRate float64) bool {
	return ci.High < minRate
}

func PassesMaxRegression(delta Interval, maxRegressionPP float64) bool {
	floor := -maxRegressionPP / 100.0
	return delta.Low >= floor
}

func FailsMaxRegression(delta Interval, maxRegressionPP float64) bool {
	floor := -maxRegressionPP / 100.0
	return delta.High < floor
}

func zFor(level float64) float64 {
	switch {
	case level >= 0.99:
		return 2.576
	case level >= 0.95:
		return 1.96
	default:
		return 1.645
	}
}

func clamp01(x float64) float64 {
	return min(1, max(0, x))
}
