package paperreport

import (
	"encoding/json"
	"os"
	"strings"
)

func LoadClosedTradesJSONL(path string) ([]ClosedTradeRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	out := make([]ClosedTradeRecord, 0, 256)
	for dec.More() {
		var rec ClosedTradeRecord
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}
		rec.Symbol = strings.ToUpper(strings.TrimSpace(rec.Symbol))
		rec.Side = strings.ToUpper(strings.TrimSpace(rec.Side))
		out = append(out, rec)
	}
	return out, nil
}
