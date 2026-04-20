package strategies

// DetectStrategy is the explicit setup detector contract used by the new strategy stack.
type DetectStrategy interface {
	ID() StrategyID
	Detect(ctx StrategyContext) (*EntryIntent, bool)
}

type ConfirmationEngine interface {
	Confirm(ctx StrategyContext, intent *EntryIntent) (bool, []string)
}

type IntentRouter interface {
	Route(ctx StrategyContext) []*EntryIntent
}

