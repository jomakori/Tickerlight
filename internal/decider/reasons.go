package decider

// Hold/guard reason codes this package ever journals. Table-driven tests
// pin these exact strings.
const (
	ReasonNotArmed      = "not_armed"
	ReasonKilled        = "killed"
	ReasonHold          = "hold"
	ReasonLowConfidence = "low_confidence"
	ReasonLowSetupScore = "low_setup_score"
	ReasonHighRisk      = "high_risk"
	ReasonJevError      = "jev_error"
	ReasonBadResponse   = "bad_response"
	ReasonSizeInvalid   = "size_invalid"
)
