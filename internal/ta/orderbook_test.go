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

func TestOrderbook_NearestWallUsesRelativeSizeAndDistance(t *testing.T) {
	bids := [][2]float64{
		{100.0, 4},
		{99.95, 24},
		{99.90, 8},
	}
	asks := [][2]float64{
		{100.05, 5},
		{100.10, 6},
		{100.15, 7},
	}
	ob := OrderBookContext("TEST", bids, asks, 3)

	if ob.NearestBidWall == nil {
		t.Fatalf("expected nearest bid wall")
	}
	if ob.NearestBidWall.Price != 99.95 {
		t.Fatalf("expected relative-size wall at 99.95, got %+v", ob.NearestBidWall)
	}
	if ob.NearestBidWall.SizeRatio < 2.0 {
		t.Fatalf("expected elevated size ratio, got %.2f", ob.NearestBidWall.SizeRatio)
	}
	if ob.NearestBidWall.DistanceBps <= 0 {
		t.Fatalf("expected positive wall distance, got %.2f", ob.NearestBidWall.DistanceBps)
	}
}
