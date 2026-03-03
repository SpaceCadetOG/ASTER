package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go-machine/adapters/aster"
	"go-machine/internal/features"
	"go-machine/internal/types"
)

func main() {
	symbol := envStr("VP_SYMBOL", "BTCUSDT")
	tfStr := envStr("VP_TF", "5m")
	n := envInt("VP_N", 300)
	bins := envInt("VP_BINS", 72)
	valuePct := envFloat("VP_VALUE_PCT", 0.70)
	hvnN := envInt("VP_HVN_N", 5)
	lvnN := envInt("VP_LVN_N", 5)
	asJSON := envBool("VP_JSON", false)

	tf, ok := types.ParseTF(tfStr)
	if !ok {
		fmt.Fprintf(os.Stderr, "bad VP_TF=%q\n", tfStr)
		os.Exit(2)
	}

	c := aster.New("")
	bars, err := c.LoadCandles(symbol, tf, n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load candles error: %v\n", err)
		os.Exit(1)
	}
	if len(bars) < 20 {
		fmt.Fprintf(os.Stderr, "not enough candles: %d\n", len(bars))
		os.Exit(1)
	}

	fc := make([]features.Candle, 0, len(bars))
	for _, b := range bars {
		fc = append(fc, features.Candle{Ts: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V})
	}

	eng := features.NewVolumeProfileEngine(features.VolumeProfileConfig{
		Bins:     bins,
		ValuePct: valuePct,
		HVNTopN:  hvnN,
		LVNTopN:  lvnN,
	})
	vp := eng.Eval(fc)

	last := fc[len(fc)-1].C
	ctx := "ABOVE_VAH"
	switch {
	case last < vp.VAL:
		ctx = "BELOW_VAL"
	case last >= vp.VAL && last <= vp.VAH:
		ctx = "INSIDE_VALUE"
	}

	out := map[string]any{
		"symbol":      strings.ToUpper(aster.RawSymbol(symbol)),
		"tf":          tf.String(),
		"candles":     len(fc),
		"lastPrice":   last,
		"context":     ctx,
		"poc":         vp.POCPrice,
		"vah":         vp.VAH,
		"val":         vp.VAL,
		"shape":       vp.Shape,
		"pocShare":    vp.POCShare,
		"vaWidthPct":  vp.VAWidthPct,
		"distToPOCBP": vp.DistToPOCBP,
		"inValueArea": vp.InValueArea,
		"hvn":         vp.HVNs,
		"lvn":         vp.LVNs,
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}

	fmt.Printf("VP %s %s (n=%d bins=%d)\n", out["symbol"], out["tf"], len(fc), bins)
	fmt.Printf("last=%.6f context=%s inValueArea=%v distPOC=%+.1fbp shape=%s\n", last, ctx, vp.InValueArea, vp.DistToPOCBP, vp.Shape)
	fmt.Printf("POC=%.6f | VAH=%.6f | VAL=%.6f\n", vp.POCPrice, vp.VAH, vp.VAL)
	fmt.Printf("POCshare=%.2f%% | VAwidth=%.2f%%\n", vp.POCShare*100.0, vp.VAWidthPct)
	fmt.Println("HVN:")
	for i, x := range vp.HVNs {
		fmt.Printf("  %d) px=%.6f vol=%.2f\n", i+1, x.Price, x.Volume)
	}
	fmt.Println("LVN:")
	for i, x := range vp.LVNs {
		fmt.Printf("  %d) px=%.6f vol=%.2f\n", i+1, x.Price, x.Volume)
	}
}

func envStr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	if v == "" {
		return def
	}
	return !(v == "0" || v == "false" || v == "no" || v == "off")
}
