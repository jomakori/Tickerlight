package decider

// Client for Jev (TypeSafe System One): one POST per tick, no retry, no
// loop. A non-200 status, transport failure, or timeout is classified
// jev_error; a 200 whose body does not parse into the expected answer
// shape is classified bad_response — the two hold reasons the gate
// reports for a failed call (see decider.go).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// jevURL is Jev's fixed endpoint; this package calls no other host.
const jevURL = "https://api.typesafe.ai/v1/systemone"

// jevModel is the model this package asks Jev to use.
const jevModel = "jev-latest"

// jevQuestions is the fixed question set MAK-51 sends on every call.
// "setup_quality" is a score question, whose "criteria" is an ordered
// list of level descriptions (NOT "levels" — that shape returns HTTP
// 422). "action" and "risk" each take "criteria" as a choice/boolean map.
var jevQuestions = map[string]any{
	"action": map[string]any{
		"type":         "choice",
		"instructions": "Given the account state and quote, what is the best action for this symbol now?",
		"criteria": map[string]string{
			"buy":  "Open or add to a long position in the symbol",
			"sell": "Reduce or close an existing long position in the symbol",
			"hold": "Take no action this tick",
		},
	},
	"setup_quality": map[string]any{
		"type":         "score",
		"instructions": "Rate the quality of this trading setup as a signal for an immediate order.",
		"criteria":     []string{"no edge, noise", "weak, ambiguous", "clear, well-supported"},
	},
	"risk": map[string]any{
		"type":         "noul",
		"instructions": "Does placing this order risk an outcome that cannot be undone or is disproportionate to the account?",
		"criteria": map[string]string{
			"true": "The order could cause loss beyond the stated per-order cap or is otherwise disproportionate",
		},
	},
}

// jevHoldReason classifies why a Decide call could not be used, so
// decider.go's Tick can journal the exact reason code without reaching
// into HTTP/JSON details itself.
type jevHoldReason struct {
	reason string
	err    error
}

func (e *jevHoldReason) Error() string { return e.err.Error() }
func (e *jevHoldReason) Unwrap() error { return e.err }

// Answer is this package's flattened read of Jev's response — just the
// fields the gate and the journal need.
type Answer struct {
	Model         string
	Choice        string
	Confidence    float64
	Probabilities map[string]float64
	Score         float64
	Noul          float64
}

// Caller is implemented by Client (production) and by a fake in every
// decider_test.go case — no test in this package performs network IO.
type Caller interface {
	Decide(ctx context.Context, state any) (Answer, error)
}

// Client calls Jev over HTTPS. APIKey comes from TYPESAFE_AI_API_KEY and
// is never logged or journalled.
type Client struct {
	APIKey string
	HTTP   httpDoer
}

func (c *Client) http() httpDoer {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type jevRequest struct {
	Model     string         `json:"model"`
	State     any            `json:"state"`
	Questions map[string]any `json:"questions"`
}

type jevResponse struct {
	Model   string `json:"model"`
	Answers struct {
		Action struct {
			Choice        string             `json:"choice"`
			Confidence    float64            `json:"confidence"`
			Probabilities map[string]float64 `json:"probabilities"`
		} `json:"action"`
		SetupQuality struct {
			Score float64 `json:"score"`
		} `json:"setup_quality"`
		Risk struct {
			Noul float64 `json:"noul"`
		} `json:"risk"`
	} `json:"answers"`
}

// Decide makes exactly one call for state and returns Jev's decision.
// Any failure is returned as a *jevHoldReason; callers use errors.As to
// read which hold reason (jev_error or bad_response) applies.
func (c *Client) Decide(ctx context.Context, state any) (Answer, error) {
	body, err := json.Marshal(jevRequest{Model: jevModel, State: state, Questions: jevQuestions})
	if err != nil {
		return Answer{}, &jevHoldReason{reason: ReasonJevError, err: fmt.Errorf("decider: encode jev request: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, jevURL, bytes.NewReader(body))
	if err != nil {
		return Answer{}, &jevHoldReason{reason: ReasonJevError, err: fmt.Errorf("decider: build jev request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.http().Do(req)
	if err != nil {
		return Answer{}, &jevHoldReason{reason: ReasonJevError, err: fmt.Errorf("decider: jev unreachable: %w", err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Answer{}, &jevHoldReason{reason: ReasonJevError, err: fmt.Errorf("decider: read jev response: %w", err)}
	}
	if resp.StatusCode != http.StatusOK {
		return Answer{}, &jevHoldReason{reason: ReasonJevError, err: fmt.Errorf("decider: jev returned HTTP %d", resp.StatusCode)}
	}

	var out jevResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Answer{}, &jevHoldReason{reason: ReasonBadResponse, err: fmt.Errorf("decider: decode jev response: %w", err)}
	}
	if out.Answers.Action.Choice == "" || out.Answers.Action.Probabilities == nil {
		return Answer{}, &jevHoldReason{reason: ReasonBadResponse, err: fmt.Errorf("decider: jev response missing action/probabilities")}
	}

	return Answer{
		Model:         out.Model,
		Choice:        out.Answers.Action.Choice,
		Confidence:    out.Answers.Action.Confidence,
		Probabilities: out.Answers.Action.Probabilities,
		Score:         out.Answers.SetupQuality.Score,
		Noul:          out.Answers.Risk.Noul,
	}, nil
}
