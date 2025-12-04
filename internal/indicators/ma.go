package indicators

import "math"

// SMA over the last `p` points; returns a slice aligned to input length with NaNs for warmup.
func SMA(x []float64, p int) []float64 {
	if p <= 0 {
		return nil
	}
	out := make([]float64, len(x))
	var sum float64
	for i := range x {
		sum += x[i]
		if i < p-1 {
			out[i] = math.NaN()
			continue
		}
		if i >= p {
			sum -= x[i-p]
		}
		out[i] = sum / float64(p)
	}
	return out
}

// EMA (standard smoothing 2/(p+1)); NaNs for warmup until i==p-1, then seed with SMA.
func EMA(x []float64, p int) []float64 {
	if p <= 0 {
		return nil
	}
	out := make([]float64, len(x))
	k := 2.0 / float64(p+1)

	// seed with SMA(p)
	var seed float64
	if len(x) < p {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	for i := 0; i < p; i++ {
		seed += x[i]
	}
	seed /= float64(p)
	for i := 0; i < p-1; i++ {
		out[i] = math.NaN()
	}
	out[p-1] = seed
	for i := p; i < len(x); i++ {
		out[i] = (x[i]-out[i-1])*k + out[i-1]
	}
	return out
}

// Rolling mean/std (population) over window p; NaNs for warmup.
func MeanStd(x []float64, p int) (mean, std []float64) {
	if p <= 0 {
		return nil, nil
	}
	n := len(x)
	mean = make([]float64, n)
	std = make([]float64, n)

	var sum float64
	var sum2 float64
	for i := 0; i < n; i++ {
		sum += x[i]
		sum2 += x[i] * x[i]
		if i < p-1 {
			mean[i] = math.NaN()
			std[i] = math.NaN()
			continue
		}
		if i >= p {
			sum -= x[i-p]
			sum2 -= x[i-p] * x[i-p]
		}
		m := sum / float64(p)
		v := (sum2/float64(p) - m*m)
		if v < 0 {
			v = 0
		}
		mean[i] = m
		std[i] = math.Sqrt(v)
	}
	return
}
