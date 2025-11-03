package indicators

import "math"

// Rolling VWAP over window p using typical price ((H+L+C)/3) and candle volume V.
// Returns NaNs for warmup.
func VWAP_TypicalPrice(H, L, C, V []float64, p int) []float64 {
	n := len(C)
	if p <= 0 || n == 0 || len(H) != n || len(L) != n || len(V) != n {
		return nil
	}
	out := make([]float64, n)
	var sumPV, sumV float64
	for i := 0; i < n; i++ {
		tp := (H[i] + L[i] + C[i]) / 3.0
		pv := tp * V[i]
		sumPV += pv
		sumV += V[i]

		if i < p-1 {
			out[i] = math.NaN()
			continue
		}
		if i >= p {
			// remove trailing window element
			tpOld := (H[i-p] + L[i-p] + C[i-p]) / 3.0
			sumPV -= tpOld * V[i-p]
			sumV -= V[i-p]
		}
		if sumV == 0 {
			out[i] = math.NaN()
		} else {
			out[i] = sumPV / sumV
		}
	}
	return out
}
