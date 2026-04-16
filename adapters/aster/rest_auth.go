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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
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
	configErr  error

	authModeSource   string
	authModeExplicit bool

	nonceMu   sync.Mutex
	lastNonce int64

	metaMu    sync.RWMutex
	metaCache map[string]SymbolMeta

	traceMu   sync.RWMutex
	lastTrace AgentAuthTrace
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
	ChainIDSet bool
	BaseURL    string
}

type AgentAuthTrace struct {
	Method            string `json:"method"`
	Path              string `json:"path"`
	PrimaryType       string `json:"primary_type"`
	DomainName        string `json:"domain_name"`
	DomainVersion     string `json:"domain_version"`
	DomainChainID     int64  `json:"domain_chain_id"`
	DomainContract    string `json:"domain_verifying_contract"`
	User              string `json:"user"`
	Signer            string `json:"signer"`
	Nonce             string `json:"nonce"`
	CanonicalMsg      string `json:"canonical_msg"`
	SentQuery         string `json:"sent_query"`
	SignatureFormat   string `json:"signature_format"`
	RecoveredSigner   string `json:"recovered_signer"`
	Attempt           string `json:"attempt"`
	OccurredAtRFC3339 string `json:"occurred_at"`
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
	mode, source, explicit, cfgErr := resolveAuthMode(cfg)
	chainID := cfg.ChainID
	if chainID == 0 && mode != "agent" {
		// Mainnet default from Aster API v3 docs.
		chainID = 1666
	}
	r := &RESTAuth{
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
		configErr:  cfgErr,

		authModeSource:   source,
		authModeExplicit: explicit,
	}
	if r.configErr == nil && r.isAgentMode() {
		r.configErr = r.validateAgentConfig()
	}
	return r
}

func resolveAuthMode(cfg RESTAuthConfig) (mode, source string, explicit bool, err error) {
	hasHMAC := strings.TrimSpace(cfg.APIKey) != "" && strings.TrimSpace(cfg.APISecret) != ""
	hasAgentTriplet := strings.TrimSpace(cfg.User) != "" && strings.TrimSpace(cfg.Signer) != "" && strings.TrimSpace(cfg.PrivateKey) != ""
	requested := strings.ToLower(strings.TrimSpace(cfg.AuthMode))
	if requested == "" {
		requested = "auto"
	}
	explicit = requested != "auto"
	switch requested {
	case "agent":
		if !hasAgentTriplet {
			return "agent", "explicit:agent", true, fmt.Errorf("agent auth mode requires ASTER_USER, ASTER_SIGNER, and ASTER_PRIVATE_KEY")
		}
		if !cfg.ChainIDSet || cfg.ChainID <= 0 {
			return "agent", "explicit:agent", true, fmt.Errorf("agent auth mode requires ASTER_CHAIN_ID")
		}
		return "agent", "explicit:agent", true, nil
	case "hmac":
		if !hasHMAC {
			return "hmac", "explicit:hmac", true, fmt.Errorf("hmac auth mode requires ASTER_API_KEY and ASTER_API_SECRET")
		}
		return "hmac", "explicit:hmac", true, nil
	case "auto":
		if hasAgentTriplet {
			if !cfg.ChainIDSet || cfg.ChainID <= 0 {
				return "agent", "auto:agent", false, fmt.Errorf("auto-selected agent auth requires ASTER_CHAIN_ID")
			}
			return "agent", "auto:agent", false, nil
		}
		if hasHMAC {
			return "hmac", "auto:hmac", false, nil
		}
		return "hmac", "auto:none", false, fmt.Errorf("missing credentials: provide agent wallet fields or legacy HMAC credentials")
	default:
		return "hmac", "invalid", true, fmt.Errorf("unknown auth mode %q (expected agent, hmac, or auto)", requested)
	}
}

func (r *RESTAuth) validateAgentConfig() error {
	if r.user == "" || r.signer == "" || r.privateKey == "" {
		return fmt.Errorf("agent auth requires user/signer/private_key")
	}
	if r.chainID <= 0 {
		return fmt.Errorf("agent auth requires positive chainID")
	}
	derived, err := deriveAddressFromPrivateKey(r.privateKey)
	if err != nil {
		return err
	}
	if !strings.EqualFold(derived, r.signer) {
		return fmt.Errorf("signer_private_key_mismatch: derived=%s configured=%s", derived, r.signer)
	}
	return nil
}

func deriveAddressFromPrivateKey(privateKey string) (string, error) {
	keyHex := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(privateKey, "0x"), "0X"))
	if keyHex == "" {
		return "", fmt.Errorf("empty private key")
	}
	priv, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid private key: %w", err)
	}
	return crypto.PubkeyToAddress(priv.PublicKey).Hex(), nil
}

func maskAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if len(addr) <= 12 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}

func envBoolRaw(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
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

func (r *RESTAuth) AuthModeSource() string {
	return r.authModeSource
}

func (r *RESTAuth) AuthModeExplicit() bool {
	return r.authModeExplicit
}

func (r *RESTAuth) ConfigError() error {
	return r.configErr
}

func (r *RESTAuth) ChainID() int64 {
	return r.chainID
}

func (r *RESTAuth) MaskedUser() string {
	return maskAddress(r.user)
}

func (r *RESTAuth) MaskedSigner() string {
	return maskAddress(r.signer)
}

func (r *RESTAuth) authDebugEnabled() bool {
	return envBoolRaw("ASTER_AUTH_DEBUG", false)
}

func (r *RESTAuth) LastAgentAuthTrace() AgentAuthTrace {
	r.traceMu.RLock()
	defer r.traceMu.RUnlock()
	return r.lastTrace
}

func (r *RESTAuth) setLastAgentAuthTrace(trace AgentAuthTrace) {
	r.traceMu.Lock()
	r.lastTrace = trace
	r.traceMu.Unlock()
	if r.authDebugEnabled() {
		b, _ := json.Marshal(trace)
		fmt.Fprintf(os.Stderr, "aster-auth-debug %s\n", string(b))
	}
}

func (r *RESTAuth) StartupAuthSummary() map[string]any {
	out := map[string]any{
		"auth_mode":      r.authMode,
		"auth_source":    r.authModeSource,
		"auth_explicit":  r.authModeExplicit,
		"base_url":       r.baseURL,
		"chain_id":       r.chainID,
		"user":           r.MaskedUser(),
		"signer":         r.MaskedSigner(),
		"has_hmac_creds": strings.TrimSpace(r.key) != "" && strings.TrimSpace(r.secret) != "",
		"has_agent":      strings.TrimSpace(r.user) != "" && strings.TrimSpace(r.signer) != "" && strings.TrimSpace(r.privateKey) != "",
	}
	if r.configErr != nil {
		out["config_error"] = r.configErr.Error()
	}
	return out
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
	if r.configErr != nil {
		return nil, r.configErr
	}
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
	payload, trace, err := r.signAndEncodeAgent(cloneValues(vals), method, path, true)
	if err != nil {
		return nil, err
	}
	r.setLastAgentAuthTrace(trace)

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
	b, err := r.do(req, method, path)
	if err == nil {
		return b, nil
	}
	apiErr, ok := err.(*APIError)
	if !ok || !strings.Contains(apiErr.Body, "Signature check failed") {
		return nil, err
	}
	payload, trace, sigErr := r.signAndEncodeAgent(cloneValues(vals), method, path, false)
	if sigErr != nil {
		return nil, err
	}
	trace.Attempt = "retry_after_signature_check_failed"
	r.setLastAgentAuthTrace(trace)
	switch method {
	case http.MethodGet, http.MethodDelete:
		req, sigErr = http.NewRequest(method, u+"?"+payload, nil)
	default:
		req, sigErr = http.NewRequest(method, u, strings.NewReader(payload))
		if sigErr == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if sigErr != nil {
		return nil, err
	}
	if r.userAgent != "" {
		req.Header.Set("User-Agent", r.userAgent)
	}
	b, finalErr := r.do(req, method, path)
	if finalErr == nil {
		trace.Attempt = "fallback_succeeded_after_signature_check_failed"
		r.setLastAgentAuthTrace(trace)
		return b, nil
	}
	return nil, finalErr
}

func (r *RESTAuth) signAndEncodeAgent(vals url.Values, method, path string, legacyV bool) (string, AgentAuthTrace, error) {
	trace := AgentAuthTrace{
		Method:            method,
		Path:              path,
		PrimaryType:       "Message",
		DomainName:        "AsterSignTransaction",
		DomainVersion:     "1",
		DomainChainID:     r.chainID,
		DomainContract:    "0x0000000000000000000000000000000000000000",
		User:              r.MaskedUser(),
		Signer:            r.MaskedSigner(),
		OccurredAtRFC3339: time.Now().UTC().Format(time.RFC3339),
	}
	if vals == nil {
		vals = url.Values{}
	}
	if r.user == "" || r.signer == "" || r.privateKey == "" {
		return "", trace, fmt.Errorf("agent auth requires user/signer/private_key")
	}

	nonce := r.nextNonceUS()
	nonceStr := strconv.FormatInt(nonce, 10)
	canonicalVals := normalizeSignedValues(vals)
	canonicalVals.Set("nonce", nonceStr)
	canonicalVals.Set("user", r.user)
	canonicalVals.Set("signer", r.signer)
	msg := encodeCanonicalQuery(canonicalVals)
	trace.Nonce = nonceStr
	trace.CanonicalMsg = msg
	sig, recovered, err := r.signAgent(msg, legacyV)
	if err != nil {
		return "", trace, err
	}
	trace.RecoveredSigner = recovered
	if legacyV {
		trace.SignatureFormat = "v=27/28"
		trace.Attempt = "initial"
	} else {
		trace.SignatureFormat = "v=0/1"
		trace.Attempt = "fallback"
	}
	if !strings.EqualFold(recovered, r.signer) {
		return "", trace, fmt.Errorf("signer_private_key_mismatch: recovered=%s configured=%s", recovered, r.signer)
	}
	wire := msg
	if wire != "" {
		wire += "&"
	}
	wire += "signature=" + url.QueryEscape(sig)
	trace.SentQuery = wire
	return wire, trace, nil
}

func normalizeSignedValues(vals url.Values) url.Values {
	out := url.Values{}
	for k, vv := range vals {
		key := strings.TrimSpace(k)
		if key == "" || key == "signature" {
			continue
		}
		cleaned := make([]string, 0, len(vv))
		for _, v := range vv {
			v = strings.TrimSpace(v)
			if v != "" {
				cleaned = append(cleaned, v)
			}
		}
		if len(cleaned) == 0 {
			continue
		}
		sort.Strings(cleaned)
		out[key] = cleaned
	}
	return out
}

func encodeCanonicalQuery(vals url.Values) string {
	if len(vals) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		encodedKey := url.QueryEscape(k)
		for _, v := range vals[k] {
			parts = append(parts, encodedKey+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func (r *RESTAuth) signAgent(msg string, legacyV bool) (string, string, error) {
	keyHex := strings.TrimSpace(strings.TrimPrefix(r.privateKey, "0x"))
	keyHex = strings.TrimSpace(strings.TrimPrefix(keyHex, "0X"))
	if keyHex == "" {
		return "", "", fmt.Errorf("agent auth requires private_key")
	}
	priv, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return "", "", fmt.Errorf("invalid agent private key: %w", err)
	}
	hash, err := asterAgentTypedDataHash(msg, r.chainID)
	if err != nil {
		return "", "", err
	}
	sig, err := crypto.Sign(hash, priv)
	if err != nil {
		return "", "", fmt.Errorf("sign agent payload: %w", err)
	}
	recoverySig := append([]byte(nil), sig...)
	pub, err := crypto.SigToPub(hash, recoverySig)
	if err != nil {
		return "", "", fmt.Errorf("recover signer: %w", err)
	}
	recovered := crypto.PubkeyToAddress(*pub).Hex()
	if legacyV {
		// Match eth_account output used by the previous Python helper.
		sig[64] += 27
	}
	return "0x" + hex.EncodeToString(sig), recovered, nil
}

func asterAgentTypedDataHash(msg string, chainID int64) ([]byte, error) {
	if strings.TrimSpace(msg) == "" {
		return nil, fmt.Errorf("agent signing message cannot be empty")
	}
	if chainID <= 0 {
		return nil, fmt.Errorf("agent signing chain id must be positive")
	}
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Message": []apitypes.Type{
				{Name: "msg", Type: "string"},
			},
		},
		PrimaryType: "Message",
		Domain: apitypes.TypedDataDomain{
			Name:              "AsterSignTransaction",
			Version:           "1",
			ChainId:           gethmath.NewHexOrDecimal256(chainID),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			"msg": msg,
		},
	}
	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return nil, fmt.Errorf("hash agent typed data: %w", err)
	}
	return hash, nil
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
