package aster

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RESTAuth struct {
	key        string
	secret     string
	user       string
	signer     string
	privateKey string
	authMode   string // "hmac" or "agent"
	chainID    int64
	baseURL    string
	client     *http.Client
	timeSkew   int64 // serverTime - localTime (ms)
	recvWindow int64
	userAgent  string

	nonceMu   sync.Mutex
	lastNonce int64

	metaMu    sync.RWMutex
	metaCache map[string]SymbolMeta
}

type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

type RESTAuthConfig struct {
	APIKey     string
	APISecret  string
	User       string
	Signer     string
	PrivateKey string
	AuthMode   string // hmac|agent|auto
	ChainID    int64
	BaseURL    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("http %d %s %s: %s", e.StatusCode, e.Method, e.Path, e.Body)
}

func NewRESTAuth(key, secret string) *RESTAuth {
	cfg := RESTAuthConfig{
		APIKey:    key,
		APISecret: secret,
	}
	return NewRESTAuthWithConfig(cfg)
}

func NewRESTAuthWithConfig(cfg RESTAuthConfig) *RESTAuth {
	baseURL := "https://fapi.asterdex.com" // futures REST host
	if v := strings.TrimSpace(cfg.BaseURL); v != "" {
		baseURL = strings.TrimRight(v, "/")
	} else if v := strings.TrimSpace(os.Getenv("ASTER_BASE_URL")); v != "" {
		baseURL = strings.TrimRight(v, "/")
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.AuthMode))
	if mode == "" || mode == "auto" {
		if strings.TrimSpace(cfg.User) != "" && strings.TrimSpace(cfg.Signer) != "" && strings.TrimSpace(cfg.PrivateKey) != "" {
			mode = "agent"
		} else {
			mode = "hmac"
		}
	}
	chainID := cfg.ChainID
	if chainID == 0 {
		// Mainnet default from Aster API v3 docs.
		chainID = 1666
	}
	return &RESTAuth{
		key:        strings.TrimSpace(cfg.APIKey),
		secret:     strings.TrimSpace(cfg.APISecret),
		user:       strings.TrimSpace(cfg.User),
		signer:     strings.TrimSpace(cfg.Signer),
		privateKey: strings.TrimSpace(cfg.PrivateKey),
		authMode:   mode,
		chainID:    chainID,
		baseURL:    baseURL,
		client:     &http.Client{Timeout: 15 * time.Second},
		recvWindow: 5000,
		userAgent:  "go-machine/aster-rest",
		metaCache:  make(map[string]SymbolMeta),
	}
}

func (r *RESTAuth) SetBaseURL(base string) {
	base = strings.TrimSpace(base)
	if base == "" {
		return
	}
	r.baseURL = strings.TrimRight(base, "/")
}

func (r *RESTAuth) BaseURL() string {
	return r.baseURL
}

func (r *RESTAuth) AuthMode() string {
	return r.authMode
}

func (r *RESTAuth) SetAgentAuth(user, signer, privateKey string, chainID int64) {
	r.user = strings.TrimSpace(user)
	r.signer = strings.TrimSpace(signer)
	r.privateKey = strings.TrimSpace(privateKey)
	if chainID > 0 {
		r.chainID = chainID
	}
	r.authMode = "agent"
}

func (r *RESTAuth) isAgentMode() bool {
	return strings.EqualFold(strings.TrimSpace(r.authMode), "agent")
}

func (r *RESTAuth) SyncTime() error {
	b, err := r.doPublicGET("/fapi/v3/time", nil)
	if err != nil {
		// Backward compatibility for older stacks.
		b, err = r.doPublicGET("/fapi/v1/time", nil)
	}
	if err != nil {
		return err
	}
	var out struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := decodeJSONNumbers(b, &out); err != nil {
		return err
	}
	if out.ServerTime <= 0 {
		return fmt.Errorf("bad serverTime: %s", string(b))
	}
	r.timeSkew = out.ServerTime - time.Now().UnixMilli()
	return nil
}

func (r *RESTAuth) ServerTime() (int64, error) {
	b, err := r.doPublicGET("/fapi/v3/time", nil)
	if err != nil {
		b, err = r.doPublicGET("/fapi/v1/time", nil)
	}
	if err != nil {
		return 0, err
	}
	var out struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := decodeJSONNumbers(b, &out); err != nil {
		return 0, err
	}
	return out.ServerTime, nil
}

func (r *RESTAuth) Ping() error {
	_, err := r.doPublicGETAny([]string{"/fapi/v3/ping", "/fapi/v1/ping"}, nil)
	return err
}

// nowMS returns local ms adjusted by best-known server skew.
func (r *RESTAuth) nowMS() int64 {
	return time.Now().UnixMilli() + r.timeSkew
}

// nextNonce returns a monotonic millisecond nonce.
func (r *RESTAuth) nextNonce() int64 {
	n := r.nowMS()
	r.nonceMu.Lock()
	defer r.nonceMu.Unlock()
	if n <= r.lastNonce {
		n = r.lastNonce + 1
	}
	r.lastNonce = n
	return n
}

// nextNonceUS returns a monotonic microsecond nonce for agent mode.
func (r *RESTAuth) nextNonceUS() int64 {
	n := time.Now().UnixMicro()
	r.nonceMu.Lock()
	defer r.nonceMu.Unlock()
	if n <= r.lastNonce {
		n = r.lastNonce + 1
	}
	r.lastNonce = n
	return n
}

// signAndEncode builds a signed payload where signature is always the last field.
func (r *RESTAuth) signAndEncode(vals url.Values, includeNonce bool) string {
	if vals == nil {
		vals = url.Values{}
	}

	ts := r.nextNonce()
	tsStr := strconv.FormatInt(ts, 10)
	if vals.Get("timestamp") == "" {
		vals.Set("timestamp", tsStr)
	}
	if includeNonce && vals.Get("nonce") == "" {
		vals.Set("nonce", tsStr)
	}
	if vals.Get("recvWindow") == "" && r.recvWindow > 0 {
		vals.Set("recvWindow", strconv.FormatInt(r.recvWindow, 10))
	}

	// Always sign without the signature key itself.
	vals.Del("signature")
	qs := vals.Encode()
	mac := hmac.New(sha256.New, []byte(r.secret))
	_, _ = mac.Write([]byte(qs))
	sig := hex.EncodeToString(mac.Sum(nil))
	if qs == "" {
		return "signature=" + sig
	}
	return qs + "&signature=" + sig
}

func (r *RESTAuth) doPublicGET(path string, vals url.Values) ([]byte, error) {
	u := r.baseURL + path
	if vals != nil && len(vals) > 0 {
		u += "?" + vals.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if r.userAgent != "" {
		req.Header.Set("User-Agent", r.userAgent)
	}
	return r.do(req, http.MethodGet, path)
}

func (r *RESTAuth) doSignedGET(path string, vals url.Values) ([]byte, error) {
	return r.doSigned(http.MethodGet, path, vals)
}

func (r *RESTAuth) doSignedPOST(path string, vals url.Values) ([]byte, error) {
	return r.doSigned(http.MethodPost, path, vals)
}

func (r *RESTAuth) doSignedPUT(path string, vals url.Values) ([]byte, error) {
	return r.doSigned(http.MethodPut, path, vals)
}

func (r *RESTAuth) doSignedDELETE(path string, vals url.Values) ([]byte, error) {
	return r.doSigned(http.MethodDelete, path, vals)
}

func (r *RESTAuth) doSigned(method, path string, vals url.Values) ([]byte, error) {
	if r.authMode == "agent" {
		return r.doSignedAgent(method, path, vals)
	}
	// Default flow: timestamp-based signature.
	b, err := r.doSignedAttempt(method, path, vals, false)
	if err == nil {
		return b, nil
	}
	// Compatibility retry: some gateways demand nonce.
	if apiErr, ok := err.(*APIError); ok && strings.Contains(apiErr.Body, "Mandatory parameter 'nonce'") {
		return r.doSignedAttempt(method, path, vals, true)
	}
	return nil, err
}

func (r *RESTAuth) doSignedAgent(method, path string, vals url.Values) ([]byte, error) {
	u := r.baseURL + path
	payload, err := r.signAndEncodeAgent(cloneValues(vals))
	if err != nil {
		return nil, err
	}

	var req *http.Request
	switch method {
	case http.MethodGet, http.MethodDelete:
		req, err = http.NewRequest(method, u+"?"+payload, nil)
	default:
		req, err = http.NewRequest(method, u, strings.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, err
	}
	if r.userAgent != "" {
		req.Header.Set("User-Agent", r.userAgent)
	}
	return r.do(req, method, path)
}

func (r *RESTAuth) signAndEncodeAgent(vals url.Values) (string, error) {
	if vals == nil {
		vals = url.Values{}
	}
	if r.user == "" || r.signer == "" || r.privateKey == "" {
		return "", fmt.Errorf("agent auth requires user/signer/private_key")
	}

	nonce := r.nextNonceUS()
	nonceStr := strconv.FormatInt(nonce, 10)
	vals.Set("nonce", nonceStr)
	vals.Set("user", r.user)
	vals.Set("signer", r.signer)
	vals.Del("signature")

	// Per docs: convert API params to strings and sort ASCII by key.
	// Build business query first (without auth fields), then combine with nonce/user/signer.
	signVals := url.Values{}
	for k, v := range vals {
		if len(v) == 0 {
			continue
		}
		kk := strings.TrimSpace(k)
		if kk == "" {
			continue
		}
		if kk == "nonce" || kk == "user" || kk == "signer" || kk == "signature" {
			continue
		}
		signVals.Set(kk, v[0])
	}

	base := signVals.Encode() // ASCII-sorted by url.Values.Encode
	msg := "user=" + r.user + "&signer=" + r.signer + "&nonce=" + nonceStr
	if base != "" {
		msg = base + "&" + msg
	}
	sig, err := r.signAgentViaPython(msg, nonceStr)
	if err != nil {
		return "", err
	}
	vals.Set("signature", sig)
	return vals.Encode(), nil
}

func (r *RESTAuth) signAgentViaPython(msg, nonceStr string) (string, error) {
	scriptPath := filepath.Join("scripts", "aster_agent_sign.py")
	if _, err := os.Stat(scriptPath); err != nil {
		return "", fmt.Errorf("agent signer script missing at %s: %w", scriptPath, err)
	}

	py := strings.TrimSpace(os.Getenv("ASTER_PYTHON"))
	candidates := []string{}
	if py != "" {
		candidates = append(candidates, py)
	}
	candidates = append(candidates, filepath.Join("venv", "bin", "python3"), "python3", "python")

	var lastErr error
	input := map[string]string{
		"msg":         msg,
		"user":        r.user,
		"signer":      r.signer,
		"private_key": r.privateKey,
		"nonce":       nonceStr,
		"chain_id":    strconv.FormatInt(r.chainID, 10),
	}
	in, _ := json.Marshal(input)

	for _, exe := range candidates {
		cmd := exec.Command(exe, scriptPath)
		cmd.Stdin = bytes.NewReader(in)
		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			lastErr = fmt.Errorf("%s: %v (%s)", exe, err, strings.TrimSpace(stderr.String()))
			continue
		}
		sig := strings.TrimSpace(out.String())
		if strings.HasPrefix(strings.ToLower(sig), "0x") && len(sig) > 10 {
			return sig, nil
		}
		lastErr = fmt.Errorf("%s returned invalid signature: %q", exe, sig)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no python runtime available for agent signing")
	}
	return "", lastErr
}

func (r *RESTAuth) doSignedAttempt(method, path string, vals url.Values, includeNonce bool) ([]byte, error) {
	u := r.baseURL + path
	payload := r.signAndEncode(cloneValues(vals), includeNonce)

	var req *http.Request
	var err error
	switch method {
	case http.MethodGet, http.MethodDelete:
		req, err = http.NewRequest(method, u+"?"+payload, nil)
	default:
		req, err = http.NewRequest(method, u, strings.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", r.key)
	req.Header.Set("x-mbx-apikey", r.key)
	req.Header.Set("X-API-KEY", r.key)
	if r.userAgent != "" {
		req.Header.Set("User-Agent", r.userAgent)
	}
	return r.do(req, method, path)
}

func (r *RESTAuth) doPublicGETAny(paths []string, vals url.Values) ([]byte, error) {
	var lastErr error
	for _, p := range paths {
		b, err := r.doPublicGET(p, vals)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no paths provided")
	}
	return nil, lastErr
}

func (r *RESTAuth) doSignedGETAny(paths []string, vals url.Values) ([]byte, error) {
	var lastErr error
	for _, p := range paths {
		b, err := r.doSignedGET(p, vals)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no paths provided")
	}
	return nil, lastErr
}

func (r *RESTAuth) doSignedPOSTAny(paths []string, vals url.Values) ([]byte, error) {
	var lastErr error
	for _, p := range paths {
		b, err := r.doSignedPOST(p, vals)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no paths provided")
	}
	return nil, lastErr
}

func (r *RESTAuth) doSignedDELETEAny(paths []string, vals url.Values) ([]byte, error) {
	var lastErr error
	for _, p := range paths {
		b, err := r.doSignedDELETE(p, vals)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no paths provided")
	}
	return nil, lastErr
}

func (r *RESTAuth) do(req *http.Request, method, path string) ([]byte, error) {
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, &APIError{StatusCode: resp.StatusCode, Method: method, Path: path, Body: string(b)}
	}
	return b, nil
}

func decodeJSONNumbers[T any](b []byte, out *T) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(out)
}

func cloneValues(v url.Values) url.Values {
	if v == nil {
		return url.Values{}
	}
	out := make(url.Values, len(v))
	for k, vv := range v {
		cp := make([]string, len(vv))
		copy(cp, vv)
		out[k] = cp
	}
	return out
}

func parseFloatString(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parseFloatAny(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		return parseFloatString(x)
	default:
		return parseFloatString(fmt.Sprint(v))
	}
}

func decimalsFromStep(step float64) int {
	if step <= 0 {
		return 8
	}
	s := strconv.FormatFloat(step, 'f', -1, 64)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return len(strings.TrimRight(s[i+1:], "0"))
	}
	return 0
}
