package executor

import (
	"testing"
	"time"

	"go-machine/internal/features"
)

func TestUpdateTrail_OnlyOnNew15mClose(t *testing.T) {
	now := time.Now().UTC()
	st := NewTrailState("BTCUSDT", SideBuy, 100, 98, TrailConfig{})
	st.AdvancedReady = true

	first := features.Candle{Ts: now.Add(-15 * time.Minute), C: 101}
	upd := UpdateTrail(&st, first, 99.2)
	if !upd.TacticalStopUpdated {
		t.Fatalf("expected tactical stop update on first close")
	}
	prevStop := st.TacticalStop

	// same close timestamp should not update.
	upd = UpdateTrail(&st, first, 100.0)
	if upd.TacticalStopUpdated {
		t.Fatalf("expected no update on same candle close")
	}
	if st.TacticalStop != prevStop {
		t.Fatalf("tactical stop should remain unchanged")
	}
}

func TestUpdateTrail_MovesBreakevenAt1R(t *testing.T) {
	now := time.Now().UTC()
	st := NewTrailState("ETHUSDT", SideBuy, 100, 98, TrailConfig{})
	closeCandle := features.Candle{
		Ts: now,
		C:  102.5, // >1R from entry given risk=2
	}
	upd := UpdateTrail(&st, closeCandle, 99.5)
	if !upd.BreakevenMoved {
		t.Fatalf("expected breakeven move at 1R")
	}
	if st.TacticalStop != 100 {
		t.Fatalf("expected stop at breakeven (100), got %.4f", st.TacticalStop)
	}
}

func TestHardStopTriggered(t *testing.T) {
	st := NewTrailState("SOLUSDT", SideBuy, 100, 97, TrailConfig{})
	if !HardStopTriggered(st, 96) {
		t.Fatalf("expected hard stop trigger below -4%%")
	}
	if HardStopTriggered(st, 99) {
		t.Fatalf("did not expect hard stop above threshold")
	}
}

func TestUpdateProtectedTrailOn15mCloseUsesClosedCandlesOnly(t *testing.T) {
	now := time.Now().UTC().Truncate(15 * time.Minute)
	st := NewTrailState("BTCUSDT", SideBuy, 100, 98, TrailConfig{})
	st.AdvancedReady = true

	history := make([]features.Candle, 0, 22)
	for i := 0; i < 21; i++ {
		base := 100.0 + float64(i)*0.2
		history = append(history, features.Candle{
			Ts: now.Add(time.Duration(i-21) * 15 * time.Minute),
			O:  base - 0.1,
			H:  base + 0.2,
			L:  base - 0.3,
			C:  base,
			V:  1000 + float64(i)*10,
		})
	}
	// Developing candle with extreme move should be ignored by the protected trail anchor.
	history = append(history, features.Candle{
		Ts: now,
		O:  104.0,
		H:  118.0,
		L:  90.0,
		C:  117.0,
		V:  5000,
	})

	upd := UpdateProtectedTrailOn15mClose(&st, history, 1.5)
	if !upd.TacticalStopUpdated {
		t.Fatalf("expected tactical stop update from closed candle anchor")
	}
	if st.Last15mClosedCandle.Equal(history[len(history)-1].Ts) {
		t.Fatalf("expected developing candle to be ignored as trailing anchor")
	}
}
