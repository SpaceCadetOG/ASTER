package ta

import "testing"

func TestOrderbook_AskDominant(t *testing.T) {
	// asks heavier than bids
	bids := [][2]float64{{100, 5}, {99.9, 4}}
	asks := [][2]float64{{100.1, 20}, {100.2, 15}}
	ob := OrderBookContext("TEST", bids, asks, 50)

	if !(ob.Imbalance < -0.2) {
		t.Fatalf("expected ask-dominant (imbalance< -0.2), got %.3f", ob.Imbalance)
	}
	if ob.TopAskWall == nil || ob.TopAskWall.Rank != 1 || ob.TopAskWall.Size < 20 {
		t.Fatalf("expected top ask wall rank=1 around size>=20, got %+v", ob.TopAskWall)
	}
}
