package aster

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"go-machine/internal/ws"
)

type UserDataBalance struct {
	Asset         string
	WalletBalance float64
	CrossWallet   float64
	BalanceChange float64
}

type UserDataPosition struct {
	Symbol         string
	PositionAmt    float64
	EntryPrice     float64
	UnrealizedPnL  float64
	IsolatedWallet float64
	PositionSide   string
	MarginType     string
}

type UserDataSnapshot struct {
	UpdatedAt time.Time
	Balances  map[string]UserDataBalance
	Positions map[string]UserDataPosition
}

type UserDataState struct {
	mu        sync.RWMutex
	updated   time.Time
	balances  map[string]UserDataBalance
	positions map[string]UserDataPosition
}

func NewUserDataState() *UserDataState {
	return &UserDataState{
		balances:  map[string]UserDataBalance{},
		positions: map[string]UserDataPosition{},
	}
}

func (s *UserDataState) Snapshot() UserDataSnapshot {
	if s == nil {
		return UserDataSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := UserDataSnapshot{
		UpdatedAt: s.updated,
		Balances:  make(map[string]UserDataBalance, len(s.balances)),
		Positions: make(map[string]UserDataPosition, len(s.positions)),
	}
	for k, v := range s.balances {
		out.Balances[k] = v
	}
	for k, v := range s.positions {
		out.Positions[k] = v
	}
	return out
}

func (s *UserDataState) ApplyAccountUpdate(raw userDataAccountUpdate) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	eventAt := time.UnixMilli(raw.EventTime).UTC()
	if eventAt.IsZero() {
		eventAt = time.Now().UTC()
	}
	if eventAt.Before(s.updated) {
		return
	}
	s.updated = eventAt
	for _, b := range raw.Account.Balances {
		asset := strings.ToUpper(strings.TrimSpace(b.Asset))
		if asset == "" {
			continue
		}
		s.balances[asset] = UserDataBalance{
			Asset:         asset,
			WalletBalance: parseFloatString(b.WalletBalance),
			CrossWallet:   parseFloatString(b.CrossWallet),
			BalanceChange: parseFloatString(b.BalanceChange),
		}
	}
	for _, p := range raw.Account.Positions {
		sym := strings.ToUpper(strings.TrimSpace(p.Symbol))
		if sym == "" {
			continue
		}
		amt := parseFloatString(p.PositionAmt)
		if math.Abs(amt) <= 1e-10 && parseFloatString(p.IsolatedWallet) <= 0 {
			delete(s.positions, sym)
			continue
		}
		s.positions[sym] = UserDataPosition{
			Symbol:         sym,
			PositionAmt:    amt,
			EntryPrice:     parseFloatString(p.EntryPrice),
			UnrealizedPnL:  parseFloatString(p.UnrealizedPnL),
			IsolatedWallet: parseFloatString(p.IsolatedWallet),
			PositionSide:   strings.ToUpper(strings.TrimSpace(p.PositionSide)),
			MarginType:     strings.ToUpper(strings.TrimSpace(p.MarginType)),
		}
	}
}

type UserDataStreamClient struct {
	rest   *RESTAuth
	wsBase string
	state  *UserDataState
}

func NewUserDataStreamClient(rest *RESTAuth, state *UserDataState) *UserDataStreamClient {
	if state == nil {
		state = NewUserDataState()
	}
	return &UserDataStreamClient{
		rest:   rest,
		wsBase: "wss://fstream.asterdex.com",
		state:  state,
	}
}

func (c *UserDataStreamClient) Run(ctx context.Context) error {
	if c == nil || c.rest == nil {
		return fmt.Errorf("user data stream requires rest auth")
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		listenKey, err := c.rest.StartUserDataStream()
		if err != nil {
			select {
			case <-time.After(5 * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err = c.runListenKey(ctx, listenKey)
		_ = c.rest.CloseUserDataStream(listenKey)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		if err == nil {
			continue
		}
	}
}

func (c *UserDataStreamClient) runListenKey(ctx context.Context, listenKey string) error {
	u := strings.TrimRight(c.wsBase, "/") + "/ws/" + url.PathEscape(strings.TrimSpace(listenKey))
	conn, err := ws.Dial(ctx, u, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	keepalive := time.NewTicker(50 * time.Minute)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-keepalive.C:
			_ = c.rest.KeepaliveUserDataStream(listenKey)
		default:
		}
		b, err := conn.ReadText(70 * time.Second)
		if err != nil {
			return err
		}
		var base struct {
			Event string `json:"e"`
		}
		if err := json.Unmarshal(b, &base); err != nil {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(base.Event)) {
		case "ACCOUNT_UPDATE":
			var upd userDataAccountUpdate
			if err := json.Unmarshal(b, &upd); err == nil {
				c.state.ApplyAccountUpdate(upd)
			}
		case "LISTENKEYEXPIRED":
			return fmt.Errorf("listenKey expired")
		}
	}
}

type userDataAccountUpdate struct {
	Event     string `json:"e"`
	EventTime int64  `json:"E"`
	Account   struct {
		Reason   string `json:"m"`
		Balances []struct {
			Asset         string `json:"a"`
			WalletBalance string `json:"wb"`
			CrossWallet   string `json:"cw"`
			BalanceChange string `json:"bc"`
		} `json:"B"`
		Positions []struct {
			Symbol         string `json:"s"`
			PositionAmt    string `json:"pa"`
			EntryPrice     string `json:"ep"`
			UnrealizedPnL  string `json:"up"`
			MarginType     string `json:"mt"`
			IsolatedWallet string `json:"iw"`
			PositionSide   string `json:"ps"`
		} `json:"P"`
	} `json:"a"`
}

type UserDataBalanceTestOnly struct {
	Asset         string
	WalletBalance string
	CrossWallet   string
	BalanceChange string
}

type UserDataPositionTestOnly struct {
	Symbol         string
	PositionAmt    string
	EntryPrice     string
	UnrealizedPnL  string
	IsolatedWallet string
	PositionSide   string
}

type UserDataAccountUpdateTestOnly struct {
	Event     string
	EventTime int64
	Balances  []UserDataBalanceTestOnly
	Positions []UserDataPositionTestOnly
}

func (s *UserDataState) ApplyAccountUpdateTestOnly(raw UserDataAccountUpdateTestOnly) {
	if s == nil {
		return
	}
	upd := userDataAccountUpdate{Event: raw.Event, EventTime: raw.EventTime}
	for _, b := range raw.Balances {
		upd.Account.Balances = append(upd.Account.Balances, struct {
			Asset         string `json:"a"`
			WalletBalance string `json:"wb"`
			CrossWallet   string `json:"cw"`
			BalanceChange string `json:"bc"`
		}{
			Asset:         b.Asset,
			WalletBalance: b.WalletBalance,
			CrossWallet:   b.CrossWallet,
			BalanceChange: b.BalanceChange,
		})
	}
	for _, p := range raw.Positions {
		upd.Account.Positions = append(upd.Account.Positions, struct {
			Symbol         string `json:"s"`
			PositionAmt    string `json:"pa"`
			EntryPrice     string `json:"ep"`
			UnrealizedPnL  string `json:"up"`
			MarginType     string `json:"mt"`
			IsolatedWallet string `json:"iw"`
			PositionSide   string `json:"ps"`
		}{
			Symbol:         p.Symbol,
			PositionAmt:    p.PositionAmt,
			EntryPrice:     p.EntryPrice,
			UnrealizedPnL:  p.UnrealizedPnL,
			IsolatedWallet: p.IsolatedWallet,
			PositionSide:   p.PositionSide,
		})
	}
	s.ApplyAccountUpdate(upd)
}

func (r *RESTAuth) StartUserDataStream() (string, error) {
	b, err := r.doSignedPOST("/fapi/v1/listenKey", url.Values{})
	if err != nil {
		return "", err
	}
	var out struct {
		ListenKey string `json:"listenKey"`
	}
	if err := decodeJSONNumbers(b, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ListenKey) == "" {
		return "", fmt.Errorf("listenKey missing from response")
	}
	return strings.TrimSpace(out.ListenKey), nil
}

func (r *RESTAuth) KeepaliveUserDataStream(listenKey string) error {
	vals := url.Values{}
	if strings.TrimSpace(listenKey) != "" {
		vals.Set("listenKey", strings.TrimSpace(listenKey))
	}
	_, err := r.doSignedPUT("/fapi/v1/listenKey", vals)
	return err
}

func (r *RESTAuth) CloseUserDataStream(listenKey string) error {
	vals := url.Values{}
	if strings.TrimSpace(listenKey) != "" {
		vals.Set("listenKey", strings.TrimSpace(listenKey))
	}
	_, err := r.doSignedDELETE("/fapi/v1/listenKey", vals)
	return err
}
