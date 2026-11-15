package decider

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEngine records how it was called and replays scripted state.
type fakeEngine struct {
	armed, killed bool

	armCalls, killCalls, accountCalls, positionCalls, capsCalls, quoteCalls, orderCalls int
	lastOrder                                                                           PlaceOrderRequest
	orderResult                                                                         PlaceOrderResult
	orderErr                                                                            error
	quote                                                                               Quote
}

func (f *fakeEngine) ArmState(context.Context) (ArmState, error) {
	f.armCalls++
	return ArmState{Armed: f.armed}, nil
}

func (f *fakeEngine) KillState(context.Context) (KillState, error) {
	f.killCalls++
	return KillState{Killed: f.killed}, nil
}

func (f *fakeEngine) Account(context.Context) (Account, error) {
	f.accountCalls++
	return Account{Equity: 500, Cash: 500, BuyingPower: 500}, nil
}

func (f *fakeEngine) Positions(context.Context) ([]Position, error) {
	f.positionCalls++
	return nil, nil
}

func (f *fakeEngine) CapsStatus(context.Context) (CapsStatus, error) {
	f.capsCalls++
	return CapsStatus{PerOrderCapCents: 5000, DailyCapCents: 15000, DailyRemainingCents: 15000, Mode: "live"}, nil
}

func (f *fakeEngine) Quote(context.Context, string) (Quote, error) {
	f.quoteCalls++
	return f.quote, nil
}

func (f *fakeEngine) PlaceOrder(_ context.Context, req PlaceOrderRequest) (PlaceOrderResult, error) {
	f.orderCalls++
	f.lastOrder = req
	return f.orderResult, f.orderErr
}

type fakeJev struct {
	answer Answer
	err    error
	calls  int
}

func (f *fakeJev) Decide(context.Context, any) (Answer, error) {
	f.calls++
	return f.answer, f.err
}

func defaultQuote() Quote {
	return Quote{Symbol: "SPY", Bid: 612.30, Ask: 612.50, Last: 612.40, PrevClose: 610.10}
}

func newDecider(t *testing.T, eng *fakeEngine, jev *fakeJev) *Decider {
	t.Helper()
	cfg := Defaults()
	cfg.JournalPath = filepath.Join(t.TempDir(), "decisions.jsonl")
	cfg.Symbols = []string{"SPY"}
	if eng.quote.Last == 0 {
		eng.quote = defaultQuote()
	}
	return &Decider{Cfg: cfg, Engine: eng, Jev: jev, Now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
}

func journalLines(t *testing.T, d *Decider) []Entry {
	t.Helper()
	raw, err := os.ReadFile(d.Cfg.JournalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var out []Entry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("journal line is not JSON: %v (%s)", err, line)
		}
		out = append(out, e)
	}
	return out
}

func strongAnswer(choice string) Answer {
	return Answer{
		Model:         "jev-1.14.0",
		Choice:        choice,
		Confidence:    0.8,
		Probabilities: map[string]float64{"buy": 0.8, "sell": 0.1, "hold": 0.1},
		Score:         0.75,
		Noul:          0.1,
	}
}

func TestDisarmedMakesNoOutboundCalls(t *testing.T) {
	eng := &fakeEngine{armed: false}
	jev := &fakeJev{answer: strongAnswer("buy")}
	d := newDecider(t, eng, jev)

	e := d.Tick(context.Background())

	if e.Gate.Reason != ReasonNotArmed {
		t.Fatalf("reason = %q, want %q", e.Gate.Reason, ReasonNotArmed)
	}
	if jev.calls != 0 {
		t.Errorf("jev calls = %d, want 0", jev.calls)
	}
	for name, n := range map[string]int{"account": eng.accountCalls, "positions": eng.positionCalls, "caps": eng.capsCalls, "quote": eng.quoteCalls, "order": eng.orderCalls} {
		if n != 0 {
			t.Errorf("%s calls = %d, want 0", name, n)
		}
	}
	if got := journalLines(t, d); len(got) != 1 || got[0].Gate.Reason != ReasonNotArmed {
		t.Fatalf("journal = %+v, want one not_armed line", got)
	}
}

func TestKilledMakesNoJevCall(t *testing.T) {
	eng := &fakeEngine{armed: true, killed: true}
	jev := &fakeJev{answer: strongAnswer("buy")}
	d := newDecider(t, eng, jev)

	if e := d.Tick(context.Background()); e.Gate.Reason != ReasonKilled {
		t.Fatalf("reason = %q, want %q", e.Gate.Reason, ReasonKilled)
	}
	if jev.calls != 0 || eng.orderCalls != 0 {
		t.Fatalf("jev=%d order=%d calls, want 0/0", jev.calls, eng.orderCalls)
	}
}

func TestGateBoundaries(t *testing.T) {
	base := func() Answer {
		return Answer{
			Choice:        "buy",
			Probabilities: map[string]float64{"buy": 0.70, "sell": 0.05, "hold": 0.25},
			Score:         0.60,
			Noul:          0.10,
		}
	}
	cfg := Defaults()
	cases := []struct {
		name   string
		mutate func(*Answer)
		passed bool
		reason string
	}{
		{"passes", func(*Answer) {}, true, ""},
		{"hold choice", func(a *Answer) { a.Choice = "hold" }, false, ReasonHold},
		{"unknown choice", func(a *Answer) { a.Choice = "short" }, false, ReasonHold},
		{"prob below", func(a *Answer) { a.Probabilities["buy"] = 0.64 }, false, ReasonLowConfidence},
		{"prob at", func(a *Answer) { a.Probabilities["buy"] = 0.65 }, true, ""},
		{"score below", func(a *Answer) { a.Score = 0.49 }, false, ReasonLowSetupScore},
		{"score at", func(a *Answer) { a.Score = 0.50 }, true, ""},
		{"risk below", func(a *Answer) { a.Noul = 0.34 }, true, ""},
		{"risk at", func(a *Answer) { a.Noul = 0.35 }, false, ReasonHighRisk},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := base()
			tc.mutate(&a)
			g := Evaluate(a, cfg)
			if g.Passed != tc.passed || g.Reason != tc.reason {
				t.Fatalf("gate = %+v, want passed=%v reason=%q", g, tc.passed, tc.reason)
			}
		})
	}
}

func TestSizing(t *testing.T) {
	cfg := Defaults()
	cases := []struct {
		name    string
		price   float64
		maxQty  float64
		wantQty float64
		ok      bool
	}{
		{"spy", 612.40, 25, 0.08164598, true},
		{"zero price", 0, 25, 0, false},
		{"above max qty", 1.50, 25, 0, false},
		{"cheap symbol sizes exactly", 25, 25, 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cfg
			c.MaxQtyPerSymbol = tc.maxQty
			qty, ok := Size(tc.price, c)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && (qty != tc.wantQty || qty != 0 && qty != tc.wantQty) {
				t.Fatalf("qty = %v, want %v", qty, tc.wantQty)
			}
		})
	}
}

func TestJevFailuresHold(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		reason string
	}{
		{"transport", &jevHoldReason{reason: ReasonJevError, err: errors.New("dial tcp: timeout")}, ReasonJevError},
		{"bad body", &jevHoldReason{reason: ReasonBadResponse, err: errors.New("missing action")}, ReasonBadResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := &fakeEngine{armed: true}
			jev := &fakeJev{err: tc.err}
			d := newDecider(t, eng, jev)

			e := d.Tick(context.Background())
			if e.Gate.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", e.Gate.Reason, tc.reason)
			}
			if eng.orderCalls != 0 {
				t.Fatalf("order calls = %d, want 0", eng.orderCalls)
			}
			if jev.calls != 1 {
				t.Fatalf("jev calls = %d, want exactly 1 (no retry)", jev.calls)
			}
		})
	}
}

func TestExecutesOnStrongAnswer(t *testing.T) {
	eng := &fakeEngine{armed: true, orderResult: PlaceOrderResult{Executed: true, BrokerOrderID: "abc-1", Outcome: "executed"}}
	jev := &fakeJev{answer: strongAnswer("buy")}
	d := newDecider(t, eng, jev)

	e := d.Tick(context.Background())

	if !e.Gate.Passed || e.Order == nil || !e.Order.Placed {
		t.Fatalf("entry = %+v, want executed order", e)
	}
	if eng.lastOrder.Symbol != "SPY" || eng.lastOrder.Side != "buy" {
		t.Fatalf("order = %+v, want SPY buy", eng.lastOrder)
	}
	if want := 0.08164598; eng.lastOrder.Qty != want {
		t.Fatalf("qty = %v, want %v", eng.lastOrder.Qty, want)
	}
	if eng.lastOrder.OrderType != "market" || eng.lastOrder.Price != 612.40 {
		t.Fatalf("order = %+v, want market at midpoint", eng.lastOrder)
	}
}

func TestEngineBlockIsJournaled(t *testing.T) {
	eng := &fakeEngine{armed: true, orderResult: PlaceOrderResult{Executed: false, Outcome: "blocked:qty_disagrees_with_sizing"}}
	a := strongAnswer("sell")
	a.Probabilities = map[string]float64{"buy": 0.1, "sell": 0.8, "hold": 0.1}
	jev := &fakeJev{answer: a}
	d := newDecider(t, eng, jev)

	e := d.Tick(context.Background())

	if e.Gate.Passed {
		t.Fatal("gate passed on a blocked order")
	}
	if e.Gate.Reason != "blocked:qty_disagrees_with_sizing" {
		t.Fatalf("reason = %q", e.Gate.Reason)
	}
	if got := journalLines(t, d); len(got) != 1 || got[0].Order == nil || got[0].Order.BrokerReached {
		t.Fatalf("journal = %+v, want blocked order with broker_reached=false", got)
	}
}

func TestJournalIsAppendOnly(t *testing.T) {
	eng := &fakeEngine{armed: false}
	d := newDecider(t, eng, &fakeJev{answer: strongAnswer("buy")})

	d.Tick(context.Background())
	eng.armed = true
	eng.quote = defaultQuote()
	d.Tick(context.Background())

	if got := journalLines(t, d); len(got) != 2 {
		t.Fatalf("journal lines = %d, want 2", len(got))
	}
	info, err := os.Stat(d.Cfg.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Fatalf("journal mode = %o, want 640", perm)
	}
}

func TestMarketOpen(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := func(h, m int) time.Time {
		return time.Date(2026, time.November, 13, h, m, 0, 0, loc) // a Friday
	}
	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"before open", at(9, 29), false},
		{"at open", at(9, 30), true},
		{"midday", at(12, 0), true},
		{"last minute", at(15, 59), true},
		{"at close", at(16, 0), false},
		{"saturday", time.Date(2026, time.November, 14, 12, 0, 0, 0, loc), false},
		{"utc converted", time.Date(2026, time.November, 13, 15, 0, 0, 0, time.UTC), true}, // 10:00 ET
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MarketOpen(tc.t); got != tc.want {
				t.Fatalf("MarketOpen(%s) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func TestRotationOneSymbolPerTick(t *testing.T) {
	cfg := Defaults()
	cfg.Symbols = []string{"AAPL", "MSFT", "SPY", "QQQ"}
	d := &Decider{Cfg: cfg}
	var got []string
	for i := 0; i < 5; i++ {
		got = append(got, d.NextSymbol())
	}
	want := []string{"AAPL", "MSFT", "SPY", "QQQ", "AAPL"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rotation = %v, want %v", got, want)
		}
	}
}
