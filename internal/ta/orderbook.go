// go-machine/internal/ta/orderbook.go
package ta

type OBWall struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
	Rank  int     `json:"rank"`
	Side  string  `json:"side"`
}

type OBContext struct {
	Symbol     string  `json:"symbol"`
	Imbalance  float64 `json:"imbalance"`
	TopBidWall *OBWall `json:"topBidWall,omitempty"`
	TopAskWall *OBWall `json:"topAskWall,omitempty"`
	BidSum     float64 `json:"bidSum"`
	AskSum     float64 `json:"askSum"`
	LevelsUsed int     `json:"levelsUsed"`
}

func OrderBookContext(symbol string, bids [][2]float64, asks [][2]float64, levels int) OBContext {
	if levels <= 0 {
		levels = 50
	}
	var bidSum, askSum float64
	var topBid *OBWall
	var topAsk *OBWall

	useB := min(len(bids), levels)
	useA := min(len(asks), levels)

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

	return OBContext{
		Symbol:     symbol,
		Imbalance:  imb,
		TopBidWall: topBid,
		TopAskWall: topAsk,
		BidSum:     bidSum,
		AskSum:     askSum,
		LevelsUsed: levels,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
