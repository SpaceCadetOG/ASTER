// go-machine/internal/ta/orderbook.go
package ta

type OBWall struct {
	Price       float64 `json:"price"`
	Size        float64 `json:"size"`
	Rank        int     `json:"rank"`
	Side        string  `json:"side"`
	DistanceBps float64 `json:"distanceBps,omitempty"`
	SizeRatio   float64 `json:"sizeRatio,omitempty"`
}

type OBContext struct {
	Symbol         string  `json:"symbol"`
	Imbalance      float64 `json:"imbalance"`
	TopBidWall     *OBWall `json:"topBidWall,omitempty"`
	TopAskWall     *OBWall `json:"topAskWall,omitempty"`
	NearestBidWall *OBWall `json:"nearestBidWall,omitempty"`
	NearestAskWall *OBWall `json:"nearestAskWall,omitempty"`
	BidSum         float64 `json:"bidSum"`
	AskSum         float64 `json:"askSum"`
	AvgBidSize     float64 `json:"avgBidSize"`
	AvgAskSize     float64 `json:"avgAskSize"`
	MidPrice       float64 `json:"midPrice"`
	LevelsUsed     int     `json:"levelsUsed"`
}

func OrderBookContext(symbol string, bids [][2]float64, asks [][2]float64, levels int) OBContext {
	if levels <= 0 {
		levels = 50
	}
	var bidSum, askSum float64
	var topBid *OBWall
	var topAsk *OBWall
	var nearestBid *OBWall
	var nearestAsk *OBWall

	useB := min(len(bids), levels)
	useA := min(len(asks), levels)
	bestBidScore := -1.0
	bestAskScore := -1.0
	mid := 0.0
	if len(bids) > 0 && len(asks) > 0 && bids[0][0] > 0 && asks[0][0] > 0 {
		mid = (bids[0][0] + asks[0][0]) / 2.0
	}

	for i := 0; i < useB; i++ {
		p, q := bids[i][0], bids[i][1]
		bidSum += q
		if topBid == nil || q > topBid.Size {
			tb := OBWall{Price: p, Size: q, Rank: i + 1, Side: "bid"}
			topBid = &tb
		}
	}
	for i := 0; i < useA; i++ {
		p, q := asks[i][0], asks[i][1]
		askSum += q
		if topAsk == nil || q > topAsk.Size {
			ta := OBWall{Price: p, Size: q, Rank: i + 1, Side: "ask"}
			topAsk = &ta
		}
	}

	imb := 0.0
	if (bidSum + askSum) > 0 {
		imb = (bidSum - askSum) / (bidSum + askSum)
	}
	avgBid := 0.0
	avgAsk := 0.0
	if useB > 0 {
		avgBid = bidSum / float64(useB)
	}
	if useA > 0 {
		avgAsk = askSum / float64(useA)
	}
	for i := 0; i < useB; i++ {
		p, q := bids[i][0], bids[i][1]
		sizeRatio := 0.0
		if avgBid > 0 {
			sizeRatio = q / avgBid
		}
		distBps := 0.0
		if mid > 0 {
			distBps = absFloat((p-mid)/mid) * 10000.0
		}
		score := sizeRatio / (1.0 + distBps/10.0)
		if score > bestBidScore {
			bestBidScore = score
			w := OBWall{Price: p, Size: q, Rank: i + 1, Side: "bid", DistanceBps: distBps, SizeRatio: sizeRatio}
			nearestBid = &w
		}
	}
	for i := 0; i < useA; i++ {
		p, q := asks[i][0], asks[i][1]
		sizeRatio := 0.0
		if avgAsk > 0 {
			sizeRatio = q / avgAsk
		}
		distBps := 0.0
		if mid > 0 {
			distBps = absFloat((p-mid)/mid) * 10000.0
		}
		score := sizeRatio / (1.0 + distBps/10.0)
		if score > bestAskScore {
			bestAskScore = score
			w := OBWall{Price: p, Size: q, Rank: i + 1, Side: "ask", DistanceBps: distBps, SizeRatio: sizeRatio}
			nearestAsk = &w
		}
	}

	return OBContext{
		Symbol:         symbol,
		Imbalance:      imb,
		TopBidWall:     topBid,
		TopAskWall:     topAsk,
		NearestBidWall: nearestBid,
		NearestAskWall: nearestAsk,
		BidSum:         bidSum,
		AskSum:         askSum,
		AvgBidSize:     avgBid,
		AvgAskSize:     avgAsk,
		MidPrice:       mid,
		LevelsUsed:     levels,
	}
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
