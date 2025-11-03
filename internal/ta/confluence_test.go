package ta

import "testing"

func TestComputeConfluence_Long_BidSupport(t *testing.T) {
	tr := TrendResult{
		TrendScore: 65,
		EMARatio:   1.01,
		AboveVWAP:  0.7,
		Slope9:     4,
		Slope21:    2,
		Bias:       "bull",
	}
	ef := EffortResult{
		EffortScore:  35,
		EMAvol:       12,
		MeanVol:      10,
		SpikeDensity: 0.08,
	}
	ob := OBContext{
		Imbalance:  +0.30,
		TopBidWall: &OBWall{Rank: 1, Size: 40, Side: "bid"},
		TopAskWall: &OBWall{Rank: 5, Size: 10, Side: "ask"},
		LevelsUsed: 50,
	}

	res := ComputeConfluence(tr, ef, ob, "long")
	if res.Score < 70 {
		t.Fatalf("expected high score for long w/ bid support, got %.2f", res.Score)
	}
	if res.Label != "A" && res.Label != "B" {
		t.Fatalf("expected A/B label, got %s", res.Label)
	}
}

func TestComputeConfluence_Short_AskPressure(t *testing.T) {
	tr := TrendResult{
		TrendScore: 55,
		EMARatio:   0.995,
		AboveVWAP:  0.4,
		Slope9:     -3,
		Slope21:    -1,
		Bias:       "bear",
	}
	ef := EffortResult{EffortScore: 30}
	ob := OBContext{
		Imbalance:  -0.35,
		TopBidWall: &OBWall{Rank: 5, Size: 10, Side: "bid"},
		TopAskWall: &OBWall{Rank: 1, Size: 35, Side: "ask"},
		LevelsUsed: 50,
	}

	res := ComputeConfluence(tr, ef, ob, "short")
	if res.Score < 60 {
		t.Fatalf("expected decent score for short w/ ask pressure, got %.2f", res.Score)
	}
	if res.Label != "A" && res.Label != "B" {
		t.Fatalf("expected A/B label, got %s", res.Label)
	}
}

func TestComputeConfluence_SideAwareness(t *testing.T) {
	tr := TrendResult{TrendScore: 60, EMARatio: 1.0, AboveVWAP: 0.5, Slope9: 1, Slope21: 1, Bias: "bull"}
	ef := EffortResult{EffortScore: 30}
	obAskHeavy := OBContext{
		Imbalance:  -0.40,
		TopAskWall: &OBWall{Rank: 1, Size: 50, Side: "ask"},
		TopBidWall: &OBWall{Rank: 5, Size: 10, Side: "bid"},
	}
	longRes := ComputeConfluence(tr, ef, obAskHeavy, "long")
	shortRes := ComputeConfluence(tr, ef, obAskHeavy, "short")

	if !(shortRes.Score > longRes.Score) {
		t.Fatalf("expected short score > long score when asks dominate; got long=%.2f short=%.2f",
			longRes.Score, shortRes.Score)
	}
}
