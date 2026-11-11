// Handler wires the rule-based scorer to HTTP. It is deliberately a single
// route on its own mux: this is meant to run on a private, loopback-only
// listener (see ValidateBindAddr) and must never be mounted on the console's
// public server.New mux.
package judge

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// NewHandler returns the judge scorer's handler: POST /score only. Every
// other method or path falls through to the mux's own 404/405 handling.
func NewHandler(cfg Config, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /score", scoreHandler(cfg, log))
	return mux
}

// scoreHandler decodes one Order, scores it against cfg, and writes the
// Result as JSON. A body that doesn't decode is answered with the same
// {score,reason} shape (score 0, reason "invalid request body") rather than
// a bare error, so callers can always read score+reason without branching
// on status code.
func scoreHandler(cfg Config, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var o Order
		if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
			log.Warn("judge: invalid request body", "err", err)
			writeResult(w, http.StatusBadRequest, Result{Score: scoreReject, Reason: "invalid request body"})
			return
		}
		writeResult(w, http.StatusOK, Score(cfg, o))
	}
}

func writeResult(w http.ResponseWriter, status int, res Result) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(res)
}
