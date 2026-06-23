package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/data"
	"go-machine/internal/inplay"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
)

type triggerState string

const (
	triggerOFAbsorb    triggerState = "OF_ABSORB"
	triggerOFReclaim   triggerState = "OF_RECLAIM"
	triggerStackedBid  triggerState = "OF_STACKED_BID"
	triggerStackedAsk  triggerState = "OF_STACKED_ASK"
	triggerDeltaFlip   triggerState = "OF_DELTA_FLIP"
	triggerExhaustion  triggerState = "OF_EXHAUSTION"
	triggerImpulseCont triggerState = "OF_IMPULSE_CONT"
	triggerFailReclaim triggerState = "OF_FAIL_RECLAIM"
	triggerNone        triggerState = "OF_NONE"
)

type missedOpportunity struct {
	Symbol       string
	Side         string
	Session      string
	Rank         float64
	Discovery    float64
	Trigger      float64
	Execution    float64
	Combined     float64
	TriggerState string
	Category     string
	Reason       string
	Entry        float64
	CreatedAt    time.Time
	SeenPullback bool
	Forward1m    float64
	Forward3m    float64
	Forward5m    float64
	Forward15m   float64
	MaxForward   float64
	Emitted      bool
}

type OpportunityPersistence struct {
	Symbol             string
	Side               string
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
	SeenCount          int
	TopNCount          int
	BestRank           float64
	VolumeTrendUp      bool
	MomentumStableOrUp bool
	DirectionStable    bool
	RejectedReasons    []string
	HadStarterSignal   bool
	HadEntrySignal     bool
	WasTraded          bool
	Expired            bool

	LastCombined     float64
	LastVolumeUSD    float64
	LastVolumeRatio  float64
	LastSlope        float64
	LastScore        float64
	LastOFIZ         float64
	LastRejectReason string
	ReadyAt          time.Time
	ReadyReason      string
	LastReadyLogAt   time.Time
}

type dayLeaderSnapshot struct {
	Symbol       string
	Side         string
	DayKey       string
	BestRank     float64
	BestScore    float64
	Grade        string
	State        string
	Close        float64
	RelStrength  float64
	VolumeUSD    float64
	LastObserved time.Time
}

type softRejectMemory struct {
	Symbol     string
	Side       string
	Reason     string
	RecordedAt time.Time
	ExpiresAt  time.Time
	ImprovedAt time.Time
	PromotedAt time.Time
}

type missedTracker struct {
	items             map[string]*missedOpportunity
	opp               map[string]*OpportunityPersistence
	softRejects       map[string]softRejectMemory
	lastPriority      map[string]time.Time
	currentDayKey     string
	currentDayLeaders map[string]*dayLeaderSnapshot
	priorDayLeaders   map[string]dayLeaderSnapshot
}

func newMissedTracker() *missedTracker {
	return &missedTracker{
		items:             map[string]*missedOpportunity{},
		opp:               map[string]*OpportunityPersistence{},
		softRejects:       map[string]softRejectMemory{},
		lastPriority:      map[string]time.Time{},
		currentDayLeaders: map[string]*dayLeaderSnapshot{},
		priorDayLeaders:   map[string]dayLeaderSnapshot{},
	}
}

func missedKey(symbol, side string, ts time.Time) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.ToUpper(strings.TrimSpace(side)) + "|" + ts.UTC().Format(time.RFC3339)
}

func persistenceKey(symbol, side string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.ToUpper(strings.TrimSpace(side))
}

func priorDayLeaderMinRank() float64 {
	return clamp(envFloat("LIVE_PRIOR_DAY_LEADER_MIN_RANK", 0.78), 0, 2.0)
}

func priorDayLeaderMinScore() float64 {
	return envFloat("LIVE_PRIOR_DAY_LEADER_MIN_SCORE", 86.0)
}

func priorDayLeaderMinVolumeUSD() float64 {
	return maxFloat(0, envFloat("LIVE_PRIOR_DAY_LEADER_MIN_VOL_USD", 1_000_000))
}

func normalizeLeaderState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}

func (t *missedTracker) ensureDayLeaderWindow(now time.Time) {
	if t == nil {
		return
	}
	dayKey := dayUTCResetKey(now)
	if t.currentDayLeaders == nil {
		t.currentDayLeaders = map[string]*dayLeaderSnapshot{}
	}
	if t.priorDayLeaders == nil {
		t.priorDayLeaders = map[string]dayLeaderSnapshot{}
	}
	if strings.TrimSpace(t.currentDayKey) == "" {
		t.currentDayKey = dayKey
		return
	}
	if t.currentDayKey == dayKey {
		return
	}
	nextPrior := make(map[string]dayLeaderSnapshot, len(t.currentDayLeaders))
	for key, snap := range t.currentDayLeaders {
		if snap == nil {
			continue
		}
		cp := *snap
		nextPrior[key] = cp
	}
	t.priorDayLeaders = nextPrior
	t.currentDayLeaders = map[string]*dayLeaderSnapshot{}
	t.currentDayKey = dayKey
}

func (t *missedTracker) observeDayLeader(now time.Time, c candidate) {
	if t == nil {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if raw == "" {
		return
	}
	t.ensureDayLeaderWindow(now)
	key := persistenceKey(raw, c.Side)
	snap := t.currentDayLeaders[key]
	if snap == nil {
		snap = &dayLeaderSnapshot{
			Symbol: raw,
			Side:   strings.ToUpper(strings.TrimSpace(c.Side)),
			DayKey: t.currentDayKey,
		}
		t.currentDayLeaders[key] = snap
	}
	rankNow := maxFloat(c.CombinedScore, c.Entry.Rank)
	if rankNow >= snap.BestRank {
		snap.BestRank = rankNow
		snap.BestScore = maxFloat(snap.BestScore, c.Entry.CurrentScore)
		snap.Grade = strings.ToUpper(strings.TrimSpace(c.Entry.CurrentGrade))
		snap.State = normalizeLeaderState(fmt.Sprint(c.Entry.State))
		snap.Close = maxFloat(c.LastClose, snap.Close)
		snap.RelStrength = c.DayUTC24h
		snap.VolumeUSD = maxFloat(c.VolumeUSD, snap.VolumeUSD)
	}
	if c.Entry.CurrentScore > snap.BestScore {
		snap.BestScore = c.Entry.CurrentScore
	}
	if c.VolumeUSD > snap.VolumeUSD {
		snap.VolumeUSD = c.VolumeUSD
	}
	if c.LastClose > 0 {
		snap.Close = c.LastClose
	}
	snap.LastObserved = now
}

func priorDayLeaderGenuine(p dayLeaderSnapshot) bool {
	if p.BestRank < priorDayLeaderMinRank() {
		return false
	}
	if p.BestScore < priorDayLeaderMinScore() {
		return false
	}
	if p.VolumeUSD > 0 && p.VolumeUSD < priorDayLeaderMinVolumeUSD() {
		return false
	}
	state := normalizeLeaderState(p.State)
	if strings.Contains(state, "dump") || strings.Contains(state, "exhaust") {
		return false
	}
	return true
}

func continuationLeaderAmplifier(c candidate, p dayLeaderSnapshot) (float64, []string) {
	if !priorDayLeaderGenuine(p) {
		return 0, nil
	}
	if candidateExhaustionActive(c) || candidateRapidExpansion(c) || continuationDeteriorating(c) {
		return 0, nil
	}
	if strings.EqualFold(strings.TrimSpace(c.Entry.EntryStyle), "avoid_chase") {
		return 0, nil
	}
	if candidateExtendedForBotAdd(c) && !(hasFreshStructureReset(c) || c.ReclaimHold || c.RetestHold || c.ResetRebreak) {
		return 0, nil
	}
	if !continuationStructureConfirmed(c) && !hasFreshStructureReset(c) {
		return 0, nil
	}
	if c.Entry.State == inplay.StateDumping || c.Entry.State == inplay.StateExhausted {
		return 0, nil
	}
	boost := 0.38
	if c.Entry.CurrentScore >= 92 {
		boost += 0.07
	}
	if c.VolumeRatio >= 1.2 {
		boost += 0.05
	}
	if c.Entry.ScoreSlope >= envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02) {
		boost += 0.04
	}
	return clamp(boost, 0, 0.65), []string{
		"prior_day_leader_continuation",
		fmt.Sprintf("prior_rank=%.2f", p.BestRank),
		fmt.Sprintf("prior_score=%.1f", p.BestScore),
	}
}

func revivalLeaderAmplifier(now time.Time, c candidate, p dayLeaderSnapshot) (float64, []string) {
	if !priorDayLeaderGenuine(p) {
		return 0, nil
	}
	if !(hasFreshStructureReset(c) || c.ReclaimHold || c.RetestHold || c.ResetRebreak) {
		return 0, nil
	}
	if candidateExhaustionActive(c) || continuationDeteriorating(c) || c.Entry.State == inplay.StateExhausted {
		return 0, nil
	}
	if minutesSinceDayUTCReset(now) > envFloat("LIVE_PRIOR_DAY_REVIVAL_MAX_MIN", 600.0) {
		return 0, nil
	}
	boost := 0.32
	if hasFreshStructureReset(c) {
		boost += 0.10
	}
	if c.ReclaimHold || c.RetestHold {
		boost += 0.06
	}
	return clamp(boost, 0, 0.60), []string{
		"prior_day_leader_revival",
		fmt.Sprintf("reset_age_min=%.1f", minutesSinceDayUTCReset(now)),
	}
}

func (t *missedTracker) applyPriorDayLeaderAmplifier(now time.Time, c candidate) candidate {
	if t == nil {
		return c
	}
	t.ensureDayLeaderWindow(now)
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if raw == "" || t.priorDayLeaders == nil {
		return c
	}
	p, ok := t.priorDayLeaders[persistenceKey(raw, c.Side)]
	if !ok {
		return c
	}
	c.PriorDayBestRank = p.BestRank
	c.PriorDayBestScore = p.BestScore
	c.PriorDayGrade = p.Grade
	c.PriorDayState = p.State
	c.PriorDayClose = p.Close
	c.PriorDayRelStrength = p.RelStrength
	c.PriorDayVolumeUSD = p.VolumeUSD

	contBoost, contReasons := continuationLeaderAmplifier(c, p)
	revBoost, revReasons := revivalLeaderAmplifier(now, c, p)
	if revBoost > contBoost {
		c.PriorDayLeaderBoost = revBoost
		c.PriorDayLeaderMode = "revival_reset"
		c.PriorDayLeaderReasons = append(c.PriorDayLeaderReasons, revReasons...)
	} else if contBoost > 0 {
		c.PriorDayLeaderBoost = contBoost
		c.PriorDayLeaderMode = "continuation"
		c.PriorDayLeaderReasons = append(c.PriorDayLeaderReasons, contReasons...)
	}
	return c
}

func loadOpportunityTrackConfig() opportunityTrackConfig {
	cfg := opportunityTrackConfig{
		Enable:                 envBool("LIVE_OPP_TRACK_ENABLE", true),
		Window:                 time.Duration(envInt("LIVE_OPP_TRACK_WINDOW_SEC", 1800)) * time.Second,
		MinSeenCount:           envInt("LIVE_OPP_MIN_SEEN_COUNT", 3),
		MinTopNCount:           envInt("LIVE_OPP_MIN_TOPN_COUNT", 2),
		SoftRejectMemoryEnable: envBool("LIVE_SOFT_REJECT_MEMORY_ENABLE", true),
		SoftRejectMemoryTTL:    time.Duration(envInt("LIVE_SOFT_REJECT_MEMORY_TTL_SEC", 3600)) * time.Second,
		PersistenceEntryEnable: envBool("LIVE_PERSISTENCE_ENTRY_ENABLE", true),
		PersistenceMinRank:     envFloat("LIVE_PERSISTENCE_MIN_RANK", 0.70),
		AllowStableVolume:      envBool("LIVE_PERSISTENCE_ALLOW_STABLE_VOLUME", true),
		AllowStableMomentum:    envBool("LIVE_PERSISTENCE_ALLOW_STABLE_MOMENTUM", true),
	}
	if cfg.Window <= 0 {
		cfg.Window = 30 * time.Minute
	}
	if cfg.MinSeenCount < 1 {
		cfg.MinSeenCount = 1
	}
	if cfg.MinTopNCount < 1 {
		cfg.MinTopNCount = 1
	}
	if cfg.SoftRejectMemoryTTL <= 0 {
		cfg.SoftRejectMemoryTTL = time.Hour
	}
	if cfg.PersistenceMinRank < 0 {
		cfg.PersistenceMinRank = 0
	}
	return cfg
}

type opportunityTrackConfig struct {
	Enable                 bool
	Window                 time.Duration
	MinSeenCount           int
	MinTopNCount           int
	SoftRejectMemoryEnable bool
	SoftRejectMemoryTTL    time.Duration
	PersistenceEntryEnable bool
	PersistenceMinRank     float64
	AllowStableVolume      bool
	AllowStableMomentum    bool
}

func softRejectReason(reason string) bool {
	switch {
	case strings.Contains(reason, "vol_ratio:"):
		return true
	case strings.Contains(reason, "below_vwap_ema"), strings.Contains(reason, "above_vwap_ema"):
		return true
	case strings.Contains(reason, "continuation_no_structure_confirm"):
		return true
	case strings.Contains(reason, "hybrid_stop_rr_too_low"), strings.Contains(reason, "hybrid_stop_too_wide"):
		return true
	default:
		return false
	}
}

func volumeIncreasing(prevVolUSD, prevRatio float64, c candidate, allowStable bool) bool {
	curVol := maxFloat(c.VolumeUSD, 0)
	curRatio := maxFloat(c.VolumeRatio, 0)
	if prevVolUSD <= 0 && prevRatio <= 0 {
		return curRatio >= 1.0 || curVol > 0
	}
	if prevVolUSD > 0 && curVol >= prevVolUSD*1.02 {
		return true
	}
	if prevRatio > 0 && curRatio >= prevRatio*1.02 {
		return true
	}
	if !allowStable {
		return false
	}
	stableRatio := prevRatio > 0 && curRatio >= prevRatio*0.92 && maxFloat(curRatio, prevRatio) >= 1.0
	stableVol := prevVolUSD > 0 && curVol >= prevVolUSD*0.92
	return stableRatio || stableVol
}

func momentumStableOrImproving(prevSlope, prevScore float64, c candidate, allowStable bool) bool {
	curSlope := c.Entry.ScoreSlope
	curScore := c.Entry.CurrentScore
	if prevSlope == 0 && prevScore == 0 {
		return curSlope >= 0 || curScore > 0
	}
	if curSlope >= prevSlope+0.005 {
		return true
	}
	if curScore >= prevScore+0.5 {
		return true
	}
	if !allowStable {
		return false
	}
	if curSlope >= prevSlope-0.015 && curScore >= prevScore-1.25 {
		return true
	}
	return curSlope >= 0.02 && curScore >= prevScore-2.0
}

func directionPersistent(prevSide string, c candidate) bool {
	if strings.TrimSpace(prevSide) == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(prevSide), strings.TrimSpace(c.Side))
}

func persistenceOFIAligned(c candidate) bool {
	return continuationFastOFIAgrees(c, envFloat("LIVE_CONT_FAST_MIN_OFI_Z", 0.35))
}

func persistenceHardInvalidationReason(c candidate) string {
	if candidateExhaustionActive(c) {
		return "exhaustion_active"
	}
	if strings.EqualFold(c.Side, "BUY") && c.Entry.LongDemotionFlag {
		return "long_demotion"
	}
	if strings.EqualFold(c.Side, "SELL") && c.Entry.ShortDemotionFlag {
		return "short_demotion"
	}
	if strings.EqualFold(strings.TrimSpace(c.Entry.EntryStyle), "avoid_chase") {
		return "avoid_chase"
	}
	if !continuationStateTrending(c.Entry.State) && !hasFreshStructureReset(c) {
		return "state_not_persistent"
	}
	if !persistenceOFIAligned(c) && c.OFISamples >= maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8)) {
		return "ofi_misaligned"
	}
	return ""
}

func appendUniqueReason(reasons []string, reason string, limit int) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if strings.EqualFold(existing, reason) {
			return reasons
		}
	}
	reasons = append(reasons, reason)
	if limit > 0 && len(reasons) > limit {
		reasons = reasons[len(reasons)-limit:]
	}
	return reasons
}

func (t *missedTracker) pruneSoftRejects(now time.Time) {
	if t == nil {
		return
	}
	for key, mem := range t.softRejects {
		if now.After(mem.ExpiresAt) {
			delete(t.softRejects, key)
		}
	}
}

func (t *missedTracker) ObserveCandidate(now time.Time, c candidate, topN bool) {
	cfg := loadOpportunityTrackConfig()
	if t == nil || !cfg.Enable {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if raw == "" {
		return
	}
	t.observeDayLeader(now, c)
	t.pruneSoftRejects(now)
	key := persistenceKey(raw, c.Side)
	st := t.opp[key]
	if st == nil || st.Expired || (!st.LastSeenAt.IsZero() && now.Sub(st.LastSeenAt) > cfg.Window) {
		st = &OpportunityPersistence{
			Symbol:             raw,
			Side:               strings.ToUpper(strings.TrimSpace(c.Side)),
			FirstSeenAt:        now,
			VolumeTrendUp:      true,
			MomentumStableOrUp: true,
			DirectionStable:    true,
		}
		t.opp[key] = st
	}
	prevSeen := st.SeenCount
	st.LastSeenAt = now
	st.Expired = false
	st.SeenCount++
	if topN {
		st.TopNCount++
	}
	st.BestRank = maxFloat(st.BestRank, maxFloat(c.CombinedScore, c.Entry.Rank))
	st.VolumeTrendUp = volumeIncreasing(st.LastVolumeUSD, st.LastVolumeRatio, c, cfg.AllowStableVolume)
	st.MomentumStableOrUp = momentumStableOrImproving(st.LastSlope, st.LastScore, c, cfg.AllowStableMomentum)
	st.DirectionStable = directionPersistent(st.Side, c)
	st.HadEntrySignal = st.HadEntrySignal || (!strings.EqualFold(strings.TrimSpace(c.Strat), "") && !strings.EqualFold(c.Strat, "none"))
	if strings.TrimSpace(c.RejectReason) != "" {
		st.LastRejectReason = c.RejectReason
		st.RejectedReasons = appendUniqueReason(st.RejectedReasons, c.RejectReason, 8)
		if cfg.SoftRejectMemoryEnable && softRejectReason(c.RejectReason) {
			t.softRejects[key] = softRejectMemory{
				Symbol:     raw,
				Side:       st.Side,
				Reason:     c.RejectReason,
				RecordedAt: now,
				ExpiresAt:  now.Add(cfg.SoftRejectMemoryTTL),
			}
		}
	}
	st.LastCombined = c.CombinedScore
	st.LastVolumeUSD = c.VolumeUSD
	st.LastVolumeRatio = c.VolumeRatio
	st.LastSlope = c.Entry.ScoreSlope
	st.LastScore = c.Entry.CurrentScore
	st.LastOFIZ = c.OFIZ
	if envBool("LIVE_OPP_TRACK_VERBOSE", false) && prevSeen != st.SeenCount && (topN || st.SeenCount >= maxInt(2, cfg.MinSeenCount-1) || strings.TrimSpace(st.LastRejectReason) != "") {
		fmt.Printf("MISSED_OPP_TRACK symbol=%s side=%s rank=%.2f volume_trend=%v momentum_trend=%v seen=%d topn=%d last_reject=%s\n",
			st.Symbol, st.Side, maxFloat(c.CombinedScore, c.Entry.Rank), st.VolumeTrendUp, st.MomentumStableOrUp, st.SeenCount, st.TopNCount, firstNonEmpty(st.LastRejectReason, "none"))
	}
}

func (t *missedTracker) persistenceState(now time.Time, c candidate) (*OpportunityPersistence, bool, []string) {
	cfg := loadOpportunityTrackConfig()
	if t == nil || !cfg.Enable || !cfg.PersistenceEntryEnable {
		return nil, false, nil
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	key := persistenceKey(raw, c.Side)
	st := t.opp[key]
	if st == nil || st.Expired || st.WasTraded {
		return st, false, nil
	}
	if now.Sub(st.LastSeenAt) > cfg.Window {
		return st, false, nil
	}
	if c.Strat != "" && !strings.EqualFold(c.Strat, "none") {
		return st, false, nil
	}
	if reason := persistenceHardInvalidationReason(c); reason != "" {
		return st, false, []string{reason}
	}
	effectiveSeen := cfg.MinSeenCount
	effectiveTopN := cfg.MinTopNCount
	if mem, ok := t.softRejects[key]; ok && now.Before(mem.ExpiresAt) {
		effectiveSeen = maxInt(2, cfg.MinSeenCount-1)
		effectiveTopN = maxInt(1, cfg.MinTopNCount-1)
	}
	reasons := []string{}
	if st.SeenCount < effectiveSeen {
		reasons = append(reasons, fmt.Sprintf("seen_count:%d<%d", st.SeenCount, effectiveSeen))
	}
	if st.TopNCount < effectiveTopN {
		reasons = append(reasons, fmt.Sprintf("topn_count:%d<%d", st.TopNCount, effectiveTopN))
	}
	rankNow := maxFloat(c.CombinedScore, 0)
	if rankNow < cfg.PersistenceMinRank && st.BestRank < cfg.PersistenceMinRank {
		reasons = append(reasons, fmt.Sprintf("rank:%.2f<%.2f", rankNow, cfg.PersistenceMinRank))
	}
	if !st.VolumeTrendUp {
		reasons = append(reasons, "volume_not_stable_or_up")
	}
	if !st.MomentumStableOrUp {
		reasons = append(reasons, "momentum_not_stable_or_up")
	}
	if !st.DirectionStable {
		reasons = append(reasons, "direction_not_persistent")
	}
	if !persistenceOFIAligned(c) && c.OFISamples >= maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8)) {
		reasons = append(reasons, "ofi_not_aligned")
	}
	if len(reasons) > 0 {
		return st, false, reasons
	}
	return st, true, nil
}

func (t *missedTracker) PromoteCandidate(now time.Time, c candidate, execMgr *liveExecManager, log *stats.EventLogger) candidate {
	_ = now
	_ = execMgr
	_ = log
	return c
}

func (t *missedTracker) PrioritySuggestions(now time.Time) []operatorSuggestion {
	cfg := loadOpportunityTrackConfig()
	if t == nil || !cfg.Enable {
		return nil
	}
	t.ensureDayLeaderWindow(now)
	t.pruneSoftRejects(now)
	rows := make([]operatorSuggestion, 0, len(t.opp))
	type ranked struct {
		key string
		opp *OpportunityPersistence
	}
	list := make([]ranked, 0, len(t.opp))
	for key, opp := range t.opp {
		if opp == nil || opp.Expired || opp.WasTraded {
			continue
		}
		if now.Sub(opp.LastSeenAt) > cfg.Window {
			continue
		}
		if opp.SeenCount < maxInt(2, cfg.MinSeenCount-1) {
			continue
		}
		list = append(list, ranked{key: key, opp: opp})
	}
	sort.SliceStable(list, func(i, j int) bool {
		wi := 0.0
		if p, ok := t.priorDayLeaders[list[i].key]; ok && priorDayLeaderGenuine(p) {
			wi += 0.35
		}
		if list[i].opp.MomentumStableOrUp {
			wi += 0.10
		}
		if list[i].opp.VolumeTrendUp {
			wi += 0.08
		}
		wj := 0.0
		if p, ok := t.priorDayLeaders[list[j].key]; ok && priorDayLeaderGenuine(p) {
			wj += 0.35
		}
		if list[j].opp.MomentumStableOrUp {
			wj += 0.10
		}
		if list[j].opp.VolumeTrendUp {
			wj += 0.08
		}
		if wi != wj {
			return wi > wj
		}
		if list[i].opp.TopNCount == list[j].opp.TopNCount {
			return list[i].opp.BestRank > list[j].opp.BestRank
		}
		return list[i].opp.TopNCount > list[j].opp.TopNCount
	})
	for _, item := range list {
		opp := item.opp
		rows = append(rows, operatorSuggestion{
			Symbol:       opp.Symbol,
			Side:         opp.Side,
			Source:       "missed_opportunity_persistence",
			PreferredLev: 0,
			CreatedAt:    now,
			ExpiresAt:    now.Add(smallerDuration(5*time.Minute, cfg.Window)),
		})
		t.lastPriority[item.key] = now
		if len(rows) >= 6 {
			break
		}
	}
	return rows
}

func (t *missedTracker) HasPriority(now time.Time) bool {
	return len(t.PrioritySuggestions(now)) > 0
}

func (t *missedTracker) MarkTraded(now time.Time, c candidate) {
	if t == nil {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	key := persistenceKey(raw, c.Side)
	if opp := t.opp[key]; opp != nil {
		opp.WasTraded = true
		opp.LastSeenAt = now
	}
}

func (t *missedTracker) ReviewLines(now time.Time, limit int) []string {
	cfg := loadOpportunityTrackConfig()
	if t == nil || !cfg.Enable {
		return nil
	}
	if limit <= 0 {
		limit = 3
	}
	type row struct {
		opp *OpportunityPersistence
	}
	rows := make([]row, 0, len(t.opp))
	for _, opp := range t.opp {
		if opp == nil || opp.Expired || opp.WasTraded {
			continue
		}
		if now.Sub(opp.LastSeenAt) > cfg.Window {
			continue
		}
		if opp.SeenCount < 2 {
			continue
		}
		rows = append(rows, row{opp: opp})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].opp.TopNCount == rows[j].opp.TopNCount {
			return rows[i].opp.BestRank > rows[j].opp.BestRank
		}
		return rows[i].opp.TopNCount > rows[j].opp.TopNCount
	})
	out := make([]string, 0, minInt(limit, len(rows)))
	for _, r := range rows {
		opp := r.opp
		status := "tracking"
		if !opp.ReadyAt.IsZero() {
			status = "ready"
		}
		out = append(out, fmt.Sprintf("%s %s seen=%d topN=%d best=%.2f status=%s lastReject=%s",
			opp.Symbol, opp.Side, opp.SeenCount, opp.TopNCount, opp.BestRank, status, firstNonEmpty(opp.LastRejectReason, "none")))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (t *missedTracker) Observe(now time.Time, c candidate, reason string) {
	if t == nil || strings.TrimSpace(reason) == "" {
		return
	}
	cfg := loadOpportunityTrackConfig()
	if cfg.SoftRejectMemoryEnable {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
		key := persistenceKey(raw, c.Side)
		if softRejectReason(reason) {
			t.softRejects[key] = softRejectMemory{
				Symbol:     raw,
				Side:       strings.ToUpper(strings.TrimSpace(c.Side)),
				Reason:     reason,
				RecordedAt: now,
				ExpiresAt:  now.Add(cfg.SoftRejectMemoryTTL),
			}
		}
		if opp := t.opp[key]; opp != nil {
			opp.LastRejectReason = reason
			opp.RejectedReasons = appendUniqueReason(opp.RejectedReasons, reason, 8)
		}
	}
	if c.DiscoveryScore < envFloat("LIVE_MISS_TRACK_MIN_DISCOVERY", 0.72) && c.CombinedScore < envFloat("LIVE_MISS_TRACK_MIN_COMBINED", 0.62) {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if raw == "" || c.LastClose <= 0 {
		return
	}
	key := missedKey(raw, c.Side, now)
	t.items[key] = &missedOpportunity{
		Symbol:       raw,
		Side:         strings.ToUpper(strings.TrimSpace(c.Side)),
		Session:      string(data.CurrentRegimeCT(now)),
		Rank:         c.Entry.Rank,
		Discovery:    c.DiscoveryScore,
		Trigger:      c.TriggerScore,
		Execution:    c.ExecutionScore,
		Combined:     c.CombinedScore,
		TriggerState: c.TriggerState,
		Category:     categorizeMissReason(reason),
		Reason:       reason,
		Entry:        c.LastClose,
		CreatedAt:    now,
	}
}

func (t *missedTracker) Update(now time.Time, meta map[string]symbolMeta, longCurrent, shortCurrent map[string]inplay.Entry, log *stats.EventLogger) {
	if t == nil {
		return
	}
	cfg := loadOpportunityTrackConfig()
	t.pruneSoftRejects(now)
	for key, item := range t.items {
		m := meta[item.Symbol]
		px := m.LastPrice
		if px <= 0 || item.Entry <= 0 {
			continue
		}
		fwd := forwardExcursionPct(item.Side, item.Entry, px)
		if fwd > item.MaxForward {
			item.MaxForward = fwd
		}
		age := now.Sub(item.CreatedAt)
		if !item.SeenPullback {
			item.SeenPullback = alternatePullbackExists(item, longCurrent, shortCurrent)
		}
		if age >= time.Minute && item.Forward1m == 0 {
			item.Forward1m = fwd
		}
		if age >= 3*time.Minute && item.Forward3m == 0 {
			item.Forward3m = fwd
		}
		if age >= 5*time.Minute && item.Forward5m == 0 {
			item.Forward5m = fwd
		}
		if age >= 15*time.Minute && !item.Emitted {
			item.Forward15m = fwd
			item.Emitted = true
			if log != nil {
				log.Emit(stats.Event{
					Timestamp:    now,
					Type:         "MISSED_OPPORTUNITY",
					Symbol:       item.Symbol,
					Side:         item.Side,
					TF:           "1m",
					TriggerState: item.TriggerState,
					Score:        item.Rank,
					Discovery:    item.Discovery,
					Trigger:      item.Trigger,
					Execution:    item.Execution,
					Combined:     item.Combined,
					MissCategory: item.Category,
					EntryPx:      item.Entry,
					ExitPx:       px,
					PnLPct:       item.Forward15m,
					Reason:       fmt.Sprintf("%s|max=%.2f|pullback=%v|fwd1=%.2f|fwd3=%.2f|fwd5=%.2f", item.Reason, item.MaxForward, item.SeenPullback, item.Forward1m, item.Forward3m, item.Forward5m),
				})
			}
			delete(t.items, key)
		}
	}
	for key, opp := range t.opp {
		if opp == nil {
			delete(t.opp, key)
			continue
		}
		expireReason := ""
		switch {
		case opp.WasTraded && now.Sub(opp.LastSeenAt) > 10*time.Minute:
			expireReason = "traded"
		case now.Sub(opp.LastSeenAt) > cfg.Window:
			expireReason = "stale_window"
		default:
			var cur inplay.Entry
			var ok bool
			if strings.EqualFold(opp.Side, "BUY") {
				cur, ok = longCurrent[opp.Symbol]
			} else {
				cur, ok = shortCurrent[opp.Symbol]
			}
			if ok {
				if cur.State == inplay.StateExhausted {
					expireReason = "exhaustion"
				} else if volumeCollapseDetected(now, cur) {
					expireReason = "volume_collapsed"
				} else if cur.Rank > 0 && opp.BestRank > 0 && cur.Rank < opp.BestRank*0.70 {
					expireReason = "rank_faded"
				} else if cur.TimeInStateMin > envFloat("LIVE_LATE_CYCLE_MAX_STATE_MIN", 20.0) && cur.ScoreSlope < 0.02 {
					expireReason = "late_cycle"
				} else if cur.ScoreSlope < -0.05 {
					expireReason = "lost_momentum"
				}
			} else if len(opp.RejectedReasons) >= 3 {
				expireReason = "soft_block_stack_too_heavy"
			}
		}
		if expireReason == "" {
			continue
		}
		opp.Expired = true
		delete(t.opp, key)
	}
}

func smallerDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

func forwardExcursionPct(side string, entry, px float64) float64 {
	if entry <= 0 || px <= 0 {
		return 0
	}
	if strings.EqualFold(side, "BUY") {
		return ((px - entry) / entry) * 100.0
	}
	return ((entry - px) / entry) * 100.0
}

func volumeCollapseDetected(now time.Time, cur inplay.Entry) bool {
	offPeakMax := envFloat("LIVE_VOLUME_COLLAPSE_OFFPEAK_MAX_PCT", 20.0)
	decayMax := envFloat("LIVE_VOLUME_COLLAPSE_DECAY_MAX_SCORE", 0.55)
	if isWeekendLocal(now) {
		relax := envFloat("LIVE_WEEKEND_VOLUME_COLLAPSE_RELAX_MULT", 1.25)
		if relax > 1 {
			offPeakMax *= relax
			decayMax = math.Min(0.99, decayMax*relax)
		}
	}
	return cur.ScoreOffPeakPct > offPeakMax || cur.FollowThroughDecayScore > decayMax
}

func isWeekendLocal(ts time.Time) bool {
	switch ts.Weekday() {
	case time.Saturday, time.Sunday:
		return true
	default:
		return false
	}
}

func alternatePullbackExists(item *missedOpportunity, longCurrent, shortCurrent map[string]inplay.Entry) bool {
	if item == nil {
		return false
	}
	var cur inplay.Entry
	var ok bool
	if strings.EqualFold(item.Side, "BUY") {
		cur, ok = longCurrent[item.Symbol]
	} else {
		cur, ok = shortCurrent[item.Symbol]
	}
	if !ok {
		return false
	}
	switch cur.State {
	case inplay.StateHeating, inplay.StateInPlay, inplay.StateBalanced:
		return cur.ScoreSlope >= envFloat("LIVE_PULLBACK_RECHECK_MIN_SLOPE", 0.02)
	default:
		return false
	}
}

func deriveTriggerState(c candidate) (string, float64, []string) {
	if c.LastClose <= 0 {
		return string(triggerNone), 0.10, []string{"no_last_close"}
	}
	extVWAP := math.Abs(relativePct(c.LastClose, c.SessionVWAP))
	extEMA := math.Abs(relativePct(c.LastClose, c.EMA9))
	stackedBid := c.BookImbalance >= envFloat("LIVE_OF_STACKED_BID_IMB", 1.08)
	stackedAsk := c.BookImbalance > 0 && c.BookImbalance <= envFloat("LIVE_OF_STACKED_ASK_IMB", 0.92)
	spreadTight := c.SpreadBps > 0 && c.SpreadBps <= envFloat("LIVE_OF_MAX_SPREAD_BPS", 18.0)
	impulseSlope := c.Entry.ScoreSlope >= envFloat("LIVE_OF_IMPULSE_MIN_SLOPE", 0.10)
	contSlope := c.Entry.ScoreSlope >= envFloat("LIVE_OF_CONT_MIN_SLOPE", 0.03)
	shortSlope := c.Entry.ScoreSlope <= -envFloat("LIVE_OF_CONT_MIN_SLOPE", 0.03)
	pullbackBias := envBool("LIVE_PULLBACK_ENTRY_BIAS", true)
	maxExtVWAP := envFloat("LIVE_MAX_EXTENSION_FROM_VWAP_PCT", 1.25)
	maxExtEMA := envFloat("LIVE_MAX_EXTENSION_FROM_EMA_PCT", 1.00)
	reasons := []string{}

	if strings.EqualFold(c.Side, "BUY") {
		if c.WallMode == "wall_defense" && c.WallSide == "bid" && c.WallConfidence >= envFloat("LIVE_WALL_DEFENSE_MIN_CONF", 0.55) {
			return string(triggerOFAbsorb), clamp(0.72+c.WallConfidence*0.18-c.WallSpoofRisk*0.20, 0, 0.92), append([]string{"wall_defense"}, c.WallReasons...)
		}
		if c.WallMode == "wall_consumption" && c.WallSide == "ask" && c.WallConfidence >= envFloat("LIVE_WALL_CONSUMPTION_MIN_CONF", 0.45) {
			return string(triggerImpulseCont), clamp(0.72+c.WallConfidence*0.16-c.WallSpoofRisk*0.15, 0, 0.92), append([]string{"wall_consumption"}, c.WallReasons...)
		}
		if c.WallMode == "wall_failure" && c.WallSide == "bid" {
			return string(triggerFailReclaim), clamp(0.18+c.WallConfidence*0.18, 0, 0.50), append([]string{"wall_failure"}, c.WallReasons...)
		}
		if spreadTight && extVWAP <= maxExtVWAP && extEMA <= maxExtEMA && c.OFIZ >= envFloat("LIVE_OF_RECLAIM_MIN_OFI_Z", 0.45) && c.LastClose >= c.SessionVWAP && c.LastClose >= c.EMA9 {
			return string(triggerOFReclaim), 0.92, []string{"vwap_reclaim", "ema_hold", fmt.Sprintf("ofi_z=%.2f", c.OFIZ)}
		}
		if spreadTight && stackedBid && c.OFIZ >= envFloat("LIVE_OF_STACK_MIN_OFI_Z", 0.35) && (c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay) {
			return string(triggerStackedBid), 0.84, []string{"stacked_bid", fmt.Sprintf("imb=%.2f", c.BookImbalance)}
		}
		if impulseSlope && c.OFIZ >= envFloat("LIVE_OF_IMPULSE_MIN_OFI_Z", 0.65) && extVWAP <= maxExtVWAP*1.4 {
			return string(triggerImpulseCont), 0.86, []string{"impulse_cont", fmt.Sprintf("slope=%.3f", c.Entry.ScoreSlope)}
		}
		if c.Entry.ReversalWatchFlag && c.OFIZ <= -envFloat("LIVE_OF_EXHAUSTION_MIN_OFI_Z", 0.55) {
			return string(triggerExhaustion), 0.32, []string{"exhaustion_watch"}
		}
		if pullbackBias && (extVWAP > maxExtVWAP || extEMA > maxExtEMA) {
			reasons = append(reasons, fmt.Sprintf("extended_vwap=%.2f", extVWAP), fmt.Sprintf("extended_ema=%.2f", extEMA))
			return string(triggerExhaustion), 0.28, reasons
		}
		if c.OFIZ <= -envFloat("LIVE_OF_FAIL_RECLAIM_Z", 0.20) && c.LastClose < c.SessionVWAP {
			return string(triggerFailReclaim), 0.18, []string{"fail_reclaim"}
		}
		if contSlope {
			return string(triggerDeltaFlip), 0.58, []string{"delta_flip"}
		}
		return string(triggerNone), 0.20, []string{"trigger_not_ready"}
	}

	if c.WallMode == "wall_defense" && c.WallSide == "ask" && c.WallConfidence >= envFloat("LIVE_WALL_DEFENSE_MIN_CONF", 0.55) {
		return string(triggerOFAbsorb), clamp(0.72+c.WallConfidence*0.18-c.WallSpoofRisk*0.20, 0, 0.92), append([]string{"wall_defense_short"}, c.WallReasons...)
	}
	if c.WallMode == "wall_consumption" && c.WallSide == "bid" && c.WallConfidence >= envFloat("LIVE_WALL_CONSUMPTION_MIN_CONF", 0.45) {
		return string(triggerImpulseCont), clamp(0.72+c.WallConfidence*0.16-c.WallSpoofRisk*0.15, 0, 0.92), append([]string{"wall_consumption_short"}, c.WallReasons...)
	}
	if c.WallMode == "wall_failure" && c.WallSide == "ask" {
		return string(triggerFailReclaim), clamp(0.18+c.WallConfidence*0.18, 0, 0.50), append([]string{"wall_failure_short"}, c.WallReasons...)
	}
	if spreadTight && extVWAP <= maxExtVWAP && extEMA <= maxExtEMA && c.OFIZ <= -envFloat("LIVE_OF_RECLAIM_MIN_OFI_Z", 0.45) && c.LastClose <= c.SessionVWAP && c.LastClose <= c.EMA9 {
		return string(triggerOFReclaim), 0.92, []string{"vwap_reclaim_short", "ema_hold_short", fmt.Sprintf("ofi_z=%.2f", c.OFIZ)}
	}
	if spreadTight && stackedAsk && c.OFIZ <= -envFloat("LIVE_OF_STACK_MIN_OFI_Z", 0.35) && (c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay) {
		return string(triggerStackedAsk), 0.84, []string{"stacked_ask", fmt.Sprintf("imb=%.2f", c.BookImbalance)}
	}
	if shortSlope && c.OFIZ <= -envFloat("LIVE_OF_IMPULSE_MIN_OFI_Z", 0.65) && extVWAP <= maxExtVWAP*1.4 {
		return string(triggerImpulseCont), 0.86, []string{"impulse_cont_short", fmt.Sprintf("slope=%.3f", c.Entry.ScoreSlope)}
	}
	if c.Entry.ReversalWatchFlag && c.OFIZ >= envFloat("LIVE_OF_EXHAUSTION_MIN_OFI_Z", 0.55) {
		return string(triggerExhaustion), 0.32, []string{"exhaustion_watch_short"}
	}
	if pullbackBias && (extVWAP > maxExtVWAP || extEMA > maxExtEMA) {
		reasons = append(reasons, fmt.Sprintf("extended_vwap=%.2f", extVWAP), fmt.Sprintf("extended_ema=%.2f", extEMA))
		return string(triggerExhaustion), 0.28, reasons
	}
	if c.OFIZ >= envFloat("LIVE_OF_FAIL_RECLAIM_Z", 0.20) && c.LastClose > c.SessionVWAP {
		return string(triggerFailReclaim), 0.18, []string{"fail_reclaim_short"}
	}
	if shortSlope {
		return string(triggerDeltaFlip), 0.58, []string{"delta_flip_short"}
	}
	return string(triggerNone), 0.20, []string{"trigger_not_ready"}
}

func relativePct(px, anchor float64) float64 {
	if px <= 0 || anchor <= 0 {
		return 0
	}
	return ((px - anchor) / anchor) * 100.0
}

func chooseExitProfile(c candidate) string {
	switch candidateTradeHorizon(c, time.Now().UTC()) {
	case "swing":
		return "SWING"
	}
	switch c.SetupFamily {
	case "reset_impulse_breakout":
		return "IMPULSE"
	case "micro_pullback_continuation", "breakout_retest", "deep_pullback_reclaim", "reversal_exhaustion":
		return "ROTATION"
	}
	if c.TriggerState == string(triggerImpulseCont) || c.Entry.Momentum && c.VolumeRatio >= envFloat("LIVE_EXIT_IMPULSE_MIN_VOL_RATIO", 1.40) {
		return "IMPULSE"
	}
	return "ROTATION"
}

func profileTargetRs(c candidate, base1, base2, base3 float64) (string, float64, float64, float64) {
	profile := chooseExitProfile(c)
	if profile == "IMPULSE" {
		return profile,
			envFloat("LIVE_IMPULSE_TP1_R", maxFloat(base1, 1.1)),
			envFloat("LIVE_IMPULSE_TP2_R", maxFloat(base2, 2.6)),
			envFloat("LIVE_IMPULSE_TP3_R", maxFloat(base3, 4.2))
	}
	if profile == "SWING" {
		return profile,
			envFloat("LIVE_SWING_TP1_R", maxFloat(base1, 1.25)),
			envFloat("LIVE_SWING_TP2_R", maxFloat(base2, 3.0)),
			envFloat("LIVE_SWING_TP3_R", maxFloat(base3, 5.0))
	}
	rotationTP1 := minPositive(base1, 0.9)
	rotationTP2 := minPositive(base2, 1.8)
	rotationTP3 := minPositive(base3, 2.8)
	return profile,
		envFloat("LIVE_ROTATION_TP1_R", rotationTP1),
		envFloat("LIVE_ROTATION_TP2_R", rotationTP2),
		envFloat("LIVE_ROTATION_TP3_R", rotationTP3)
}

func computeDynamicTargetLadder(c candidate, entry, stopDist, base1, base2, base3 float64) (string, float64, float64, float64) {
	profile, tp1R, tp2R, tp3R := profileTargetRs(c, base1, base2, base3)
	if entry <= 0 || stopDist <= 0 {
		return profile, entry, entry, entry
	}
	sideBuy := strings.EqualFold(c.Side, "BUY")
	base := []float64{
		targetPriceForR(sideBuy, entry, stopDist, tp1R),
		targetPriceForR(sideBuy, entry, stopDist, tp2R),
		targetPriceForR(sideBuy, entry, stopDist, tp3R),
	}
	cands := append([]float64{}, base...)
	useStructure := envBool("LIVE_DYNAMIC_TP_USE_STRUCTURE", true)
	useATR := envBool("LIVE_DYNAMIC_TP_USE_ATR", true)
	useVWAPBands := envBool("LIVE_DYNAMIC_TP_USE_VWAP_BANDS", true)
	expectedMoveMult := envFloat("LIVE_DYNAMIC_TP_EXPECTED_MOVE_MULT", 1.0)

	if useStructure && c.Sig.VPTargetLevel > 0 && priceInTradeDirection(sideBuy, entry, c.Sig.VPTargetLevel) {
		cands = append(cands, c.Sig.VPTargetLevel)
	}
	if useVWAPBands && c.SessionVWAP > 0 {
		vwapDist := math.Abs(entry - c.SessionVWAP)
		if vwapDist > 0 {
			cands = append(cands,
				targetPriceAbsolute(sideBuy, entry, vwapDist*1.25),
				targetPriceAbsolute(sideBuy, entry, vwapDist*2.00),
			)
		}
	}
	if useATR && c.ATR > 0 {
		atrMults := []float64{1.25, 2.20, 3.40}
		if profile == "IMPULSE" {
			atrMults = []float64{1.60, 2.80, 4.20}
		} else if profile == "SWING" {
			atrMults = []float64{1.80, 3.20, 5.00}
		}
		for _, mult := range atrMults {
			cands = append(cands, targetPriceAbsolute(sideBuy, entry, c.ATR*mult))
		}
	}
	if c.ATRPct > 0 && expectedMoveMult > 0 {
		move := entry * c.ATRPct * expectedMoveMult
		if profile == "IMPULSE" {
			move *= 1.25
		} else if profile == "SWING" {
			move *= 1.50
		}
		cands = append(cands,
			targetPriceAbsolute(sideBuy, entry, maxFloat(stopDist*tp1R, move)),
			targetPriceAbsolute(sideBuy, entry, maxFloat(stopDist*tp2R, move*1.8)),
			targetPriceAbsolute(sideBuy, entry, maxFloat(stopDist*tp3R, move*2.4)),
		)
	}
	if c.DepthBid > 0 && c.DepthAsk > 0 {
		depthBias := math.Abs(c.DepthBid-c.DepthAsk) / maxFloat(c.DepthBid+c.DepthAsk, 1)
		if depthBias > 0.05 {
			depthMove := stopDist * (1.0 + depthBias*4)
			cands = append(cands, targetPriceAbsolute(sideBuy, entry, depthMove))
		}
	}

	levels := directionalLevels(cands, sideBuy, entry)
	if len(levels) == 0 {
		return profile, base[0], base[1], base[2]
	}
	picked := make([]float64, 0, 3)
	minSep := stopDist * envFloat("LIVE_DYNAMIC_TP_MIN_SEPARATION_R", 0.40)
	for _, lv := range levels {
		if len(picked) == 0 {
			if riskRewardToPrice(entry, stopDist, lv) >= envFloat("LIVE_STOP_MIN_RR_TO_TP1", 1.00) {
				picked = append(picked, lv)
			}
			continue
		}
		if math.Abs(lv-picked[len(picked)-1]) < minSep {
			continue
		}
		picked = append(picked, lv)
		if len(picked) == 3 {
			break
		}
	}
	for len(picked) < 3 {
		picked = append(picked, base[len(picked)])
	}
	return profile, picked[0], picked[1], picked[2]
}

func targetPriceForR(sideBuy bool, entry, stopDist, r float64) float64 {
	return targetPriceAbsolute(sideBuy, entry, stopDist*r)
}

func targetPriceAbsolute(sideBuy bool, entry, dist float64) float64 {
	if sideBuy {
		return entry + dist
	}
	return entry - dist
}

func priceInTradeDirection(sideBuy bool, entry, px float64) bool {
	if sideBuy {
		return px > entry
	}
	return px < entry
}

func directionalLevels(levels []float64, sideBuy bool, entry float64) []float64 {
	out := make([]float64, 0, len(levels))
	seen := map[int64]struct{}{}
	for _, lv := range levels {
		if lv <= 0 || !priceInTradeDirection(sideBuy, entry, lv) {
			continue
		}
		key := int64(lv * 1e8)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, lv)
	}
	sort.Slice(out, func(i, j int) bool {
		if sideBuy {
			return out[i] < out[j]
		}
		return out[i] > out[j]
	})
	return out
}

func riskRewardToPrice(entry, stopDist, target float64) float64 {
	if entry <= 0 || stopDist <= 0 || target <= 0 {
		return 0
	}
	return math.Abs(target-entry) / stopDist
}

func trailProfileMultiplier(profile string) float64 {
	switch strings.ToUpper(strings.TrimSpace(profile)) {
	case "IMPULSE":
		return envFloat("LIVE_TRAIL_IMPULSE_MULT", 1.15)
	case "SWING":
		return envFloat("LIVE_TRAIL_SWING_MULT", 1.25)
	case "ROTATION":
		return envFloat("LIVE_TRAIL_ROTATION_MULT", 0.90)
	default:
		return 1.0
	}
}

func structureTrailDistance(ref, friction float64) float64 {
	if ref <= 0 || friction <= 0 {
		return 0
	}
	return math.Abs(ref-friction) * envFloat("LIVE_TRAIL_STRUCTURE_FRAC", 0.55)
}

func quickCandidateSelectionReject(c candidate, now time.Time, pureMode, allowDeadSessionTrading bool, preEODEntryBlockMin int, localMaintNow time.Time, maintEOD maintenanceWindow, postSLCooldown time.Duration, paper *paperTrader, execMgr *liveExecManager, safety safetyConfig, lastOrderAt time.Time, lastOrderBySymbol map[string]time.Time, lastOrderBySymbolSide map[string]time.Time, orderCountByDay, orderCountByHour map[string]int, symbolStopoutLockUntil map[string]time.Time) string {
	raw := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	paperMode := paper != nil && paper.enabled
	_ = preEODEntryBlockMin
	_ = maintEOD
	_ = allowDeadSessionTrading
	if !paperMode {
		if window, active := activeMaintenanceWindow(localMaintNow, true, runtimeMaintenanceWindows()...); active {
			return blockedWindowReason(window)
		}
	}
	if postSLCooldown > 0 && hasRecentStopLoss(raw, c.Side, now, postSLCooldown, paper, execMgr) {
		if !shouldBypassWithFastLane(c, "POST_SL_COOLDOWN") {
			return "POST_SL_COOLDOWN"
		}
	}
	if execMgr != nil {
		if !paperMode {
			if reason := execMgr.degradedEntryReason(now, c.Entry.Symbol); reason != "" {
				return reason
			}
		}
	}
	if !pureMode {
		if reason := safetyReject(safety, c, localMaintNow, lastOrderAt, lastOrderBySymbol, lastOrderBySymbolSide, orderCountByDay, orderCountByHour, symbolStopoutLockUntil); reason != "" {
			if !shouldBypassWithFastLane(c, reason) {
				return reason
			}
		}
	}
	if execMgr != nil && execMgr.HasActiveSymbol(c.Entry.Symbol) {
		return "already_active_in_exec_state"
	}
	return ""
}

func prefilterCandidatesBeforeExpensiveWork(cands []candidate, execMgr *liveExecManager) ([]candidate, map[string]string) {
	if len(cands) == 0 || execMgr == nil {
		return cands, nil
	}
	return cands, nil
}

func paperMarkLastPrices(m symbolMeta, ob aster.OrderBook, model string, divBps float64) (float64, float64) {
	last := m.LastPrice
	bid, ask := topOfBook(ob, last)
	mark := last
	if bid > 0 && ask > 0 {
		mark = (bid + ask) / 2.0
	}
	if mark <= 0 {
		mark = last
	}
	if last <= 0 {
		last = mark
	}
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "mark_bias":
		if divBps > 0 {
			last = mark * (1 + divBps/10000.0)
		}
	case "last_bias":
		if divBps > 0 {
			mark = last * (1 - divBps/10000.0)
		}
	}
	return mark, last
}

func triggerPriceForRef(ref string, mark, last float64) float64 {
	switch strings.ToLower(strings.TrimSpace(ref)) {
	case "mark":
		if mark > 0 {
			return mark
		}
		return last
	case "last":
		if last > 0 {
			return last
		}
		return mark
	default:
		if mark > 0 {
			return mark
		}
		return last
	}
}

func depthFillRatio(side string, qty float64, ob aster.OrderBook) float64 {
	if qty <= 0 {
		return 1
	}
	levels := ob.Asks
	if !strings.EqualFold(side, "BUY") {
		levels = ob.Bids
	}
	if len(levels) == 0 {
		return 1
	}
	avail := 0.0
	for _, lv := range levels {
		if lv[1] > 0 {
			avail += lv[1]
		}
	}
	if avail <= 0 {
		return 1
	}
	return clamp(avail/qty, 0, 1)
}

func applyPaperPartialFill(qty float64, side string, ob aster.OrderBook, enabled bool, minFrac float64) float64 {
	if !enabled || qty <= 0 {
		return qty
	}
	ratio := depthFillRatio(side, qty, ob)
	if ratio >= 1 {
		return qty
	}
	frac := maxFloat(minFrac, ratio)
	return qty * clamp(frac, 0.05, 1.0)
}

func categorizeMissReason(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case r == "":
		return "uncategorized"
	case strings.Contains(r, "not_selected"), strings.Contains(r, "candidate_expired"):
		return "architecture_miss"
	case strings.Contains(r, "spread"), strings.Contains(r, "execution"), strings.Contains(r, "insufficient"), strings.Contains(r, "active"):
		return "execution_miss"
	case strings.Contains(r, "risk"), strings.Contains(r, "liq"), strings.Contains(r, "funding"), strings.Contains(r, "cooldown"), strings.Contains(r, "shadow"), strings.Contains(r, "reserve"), strings.Contains(r, "max_open"), strings.Contains(r, "correlated"):
		return "risk_miss"
	default:
		return "filter_miss"
	}
}

func nextFundingBoundary(now time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		return time.Time{}
	}
	slot := now.UTC().Truncate(interval)
	if slot.Equal(now.UTC()) {
		return slot.Add(interval)
	}
	return slot.Add(interval)
}

func fundingHazardWindow(now time.Time, interval, hazard, skipNew time.Duration) bool {
	if interval <= 0 {
		return false
	}
	next := nextFundingBoundary(now, interval)
	if next.IsZero() {
		return false
	}
	if skipNew < 0 {
		skipNew = 0
	}
	if hazard < 0 {
		hazard = 0
	}
	until := next.Sub(now.UTC())
	if until < 0 {
		until = -until
	}
	return until <= maxDuration(skipNew, hazard)
}

func paperFundingEntryBlocked(now time.Time, raw, side string, m symbolMeta, p *paperTrader) bool {
	if p == nil || !p.fundingEnabled || !fundingCostsPosition(side, m.FundingRate) {
		return false
	}
	interval := p.fundingEvery
	if p.fundingBySym != nil {
		if d, ok := p.fundingBySym[raw]; ok && d > 0 {
			interval = d
		}
	}
	return fundingHazardWindow(now, interval, p.fundingHazardSec, p.fundingSkipNewPos)
}

func estimatedFundingReserve(notional, fundingRate float64, interval time.Duration) float64 {
	if notional <= 0 || fundingRate == 0 || interval <= 0 {
		return 0
	}
	holdH := maxFloat(envFloat("LIVE_EXPECTED_HOLD_HOURS", 2.0), 2.0)
	intervals := holdH / maxFloat(interval.Hours(), 1.0)
	if intervals < 1 {
		intervals = 1
	}
	return notional * math.Abs(fundingRate) * intervals
}

type queueDeepPreflightCtx struct {
	Now                   time.Time
	LocalMaintNow         time.Time
	PureMode              bool
	OBFilterEnable        bool
	EntryDepth            map[string]aster.OrderBook
	OBLevels              int
	OBImbMin              float64
	OBMaxSpreadBps        float64
	RiskShell             risk.Config
	RiskFallbackStopPct   float64
	RiskHoldHours         float64
	LeverageMode          string
	LeverageFixed         int
	LeverageMin           int
	MaxLeverage           int
	EffectiveReserve      float64
	EffectiveMargin       float64
	AvailableUSDT         float64
	MetaBySymbol          map[string]symbolMeta
	InMaint               bool
	MaintWarmup           time.Duration
	MaintState            *maintenanceState
	Safety                safetyConfig
	Acct                  accountSnapshot
	Paper                 *paperTrader
	ReserveGate           *reserveLockGate
	EventLockoutMin       int
	CorrGroups            map[string]string
	MaxCorrelatedExposure float64
	RequireShadowDays     int
	ShadowEquityFile      string
	MaxOpenPos            int
	MaxOpenPerSide        int
	ExecMgr               *liveExecManager
}

type queueDeepPreflightResult struct {
	RejectReason string
	SpreadBps    float64
	BookImb      float64
}

func deepQueuePreflight(c candidate, ctx queueDeepPreflightCtx) queueDeepPreflightResult {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	meta := ctx.MetaBySymbol[raw]
	if raw == "" {
		return queueDeepPreflightResult{RejectReason: "empty_symbol"}
	}
	_ = ctx.InMaint
	_ = ctx.MaintWarmup
	_ = ctx.LocalMaintNow
	_ = ctx.MaintState
	if c.WallSpoofRisk >= envFloat("LIVE_WALL_SPOOF_RISK_REJECT", 0.75) {
		return queueDeepPreflightResult{RejectReason: "wall_spoof_risk"}
	}
	if c.WallMode == "wall_failure" && c.WallConfidence >= envFloat("LIVE_WALL_FAILURE_REJECT_CONF", 0.55) {
		return queueDeepPreflightResult{RejectReason: "wall_failed_on_touch"}
	}
	if c.WallMode == "wall_consumption" && c.WallConfidence < envFloat("LIVE_WALL_CONSUMPTION_MIN_CONF", 0.45) {
		return queueDeepPreflightResult{RejectReason: "wall_consumption_not_confirmed"}
	}
	if c.WallConfidence > 0 && c.WallPersistence < time.Duration(envInt("LIVE_WALL_MIN_PERSIST_MS", 3000))*time.Millisecond {
		return queueDeepPreflightResult{RejectReason: "wall_not_persistent"}
	}
	if ctx.OBFilterEnable {
		ob := ctx.EntryDepth[raw]
		okOB, obReason, obSpreadBps, obImb := orderbookEntryDecision(ob, c.Side, ctx.OBLevels, ctx.OBImbMin, ctx.OBMaxSpreadBps)
		if !okOB {
			return queueDeepPreflightResult{RejectReason: obReason, SpreadBps: obSpreadBps, BookImb: obImb}
		}
	}
	spreadBps, bookImb := orderbookRiskMetrics(raw, c.Side, ctx.EntryDepth, ctx.MetaBySymbol, ctx.OBLevels)
	entryPx := c.Sig.Entry
	if entryPx <= 0 {
		entryPx = meta.LastPrice
	}
	stopPx := c.Sig.Stop
	if stopPx <= 0 && entryPx > 0 {
		d := ctx.RiskFallbackStopPct / 100.0
		if strings.EqualFold(c.Side, "BUY") {
			stopPx = entryPx * (1 - d)
		} else {
			stopPx = entryPx * (1 + d)
		}
	}
	if !ctx.PureMode {
		effectiveLev := computeLeverage(c, ctx.LeverageMode, ctx.LeverageFixed, ctx.LeverageMin, ctx.MaxLeverage)
		riskDec := risk.Approve(ctx.RiskShell, risk.Input{
			Symbol:            raw,
			Side:              strings.ToUpper(strings.TrimSpace(c.Side)),
			Entry:             entryPx,
			Stop:              stopPx,
			Leverage:          float64(maxInt(1, effectiveLev)),
			NotionalUSD:       ctx.EffectiveMargin * float64(maxInt(1, effectiveLev)),
			FundingRate:       meta.FundingRate,
			HoldHours:         ctx.RiskHoldHours,
			SpreadBps:         spreadBps,
			BookImbalance:     bookImb,
			RecentSlippageBps: 0,
			VenueHealthy:      meta.LastPrice > 0,
		})
		if !riskDec.Approved {
			return queueDeepPreflightResult{RejectReason: riskDec.RejectReason, SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	if ctx.Paper != nil && paperFundingEntryBlocked(ctx.Now, raw, c.Side, meta, ctx.Paper) {
		return queueDeepPreflightResult{RejectReason: "paper_funding_hazard", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.ExecMgr != nil && ctx.ExecMgr.fundingEntryBlocked(ctx.Now, raw, c.Side, meta.FundingRate) {
		return queueDeepPreflightResult{RejectReason: "funding_hazard_entry_block", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode {
		if reason := safetyReject(ctx.Safety, c, ctx.Now, time.Time{}, nil, nil, nil, nil, nil); reason != "" {
			return queueDeepPreflightResult{RejectReason: reason, SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	if !ctx.PureMode && inEventLockout(ctx.Now, ctx.EventLockoutMin) {
		return queueDeepPreflightResult{RejectReason: "event_lockout", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.RequireShadowDays > 0 && !shadowReady(ctx.RequireShadowDays, ctx.ShadowEquityFile, ctx.Now) {
		return queueDeepPreflightResult{RejectReason: "shadow_gate_active", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if ctx.ExecMgr == nil && !ctx.PureMode {
		return queueDeepPreflightResult{RejectReason: "exec_manager_unavailable", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if ctx.ExecMgr != nil && ctx.ExecMgr.HasActiveSymbol(c.Entry.Symbol) {
		return queueDeepPreflightResult{RejectReason: "already_active_in_exec_state", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.AvailableUSDT > 0 && ctx.AvailableUSDT < ctx.Safety.minAvailUSDT {
		return queueDeepPreflightResult{RejectReason: "min_available_usdt", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.AvailableUSDT > 0 {
		usable := ctx.AvailableUSDT - ctx.EffectiveReserve
		if usable < ctx.EffectiveMargin {
			return queueDeepPreflightResult{RejectReason: "insufficient_usable", SpreadBps: spreadBps, BookImb: bookImb}
		}
		baseBal := sizingBaseBalance(ctx.AvailableUSDT, ctx.Paper)
		if ctx.ReserveGate != nil && ctx.ReserveGate.block(baseBal) {
			return queueDeepPreflightResult{RejectReason: "reserve_lock_active", SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	openCount := len(ctx.Acct.Positions)
	if ctx.MaxOpenPos > 0 && openCount >= ctx.MaxOpenPos {
		return queueDeepPreflightResult{RejectReason: "max_open_positions", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if ctx.MaxOpenPerSide > 0 {
		openSideCount := countOpenPositionsBySide(ctx.Acct, c.Side)
		if ctx.ExecMgr != nil && openSideCount == 0 {
			openSideCount = ctx.ExecMgr.ActiveCountBySide(c.Side)
		}
		if openSideCount >= ctx.MaxOpenPerSide {
			return queueDeepPreflightResult{RejectReason: "max_open_positions_side", SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	if ctx.ExecMgr != nil && ctx.MaxOpenPos > 0 && ctx.ExecMgr.ActiveCount() >= ctx.MaxOpenPos {
		return queueDeepPreflightResult{RejectReason: "max_tracked_entries", SpreadBps: spreadBps, BookImb: bookImb}
	}
	return queueDeepPreflightResult{SpreadBps: spreadBps, BookImb: bookImb}
}

func sortedMissedReasons(mem map[string]recentRejectMemory) []string {
	if len(mem) == 0 {
		return nil
	}
	keys := make([]string, 0, len(mem))
	for _, v := range mem {
		keys = append(keys, v.Reject)
	}
	sort.Strings(keys)
	return keys
}
