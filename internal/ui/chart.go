package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// EquityChart renders the cumulative realized P&L chart as inline SVG.
//
// rows must be the already range-filtered attempts in ledger order. allLen is
// the unfiltered row count, used only to pick the right empty-state copy.
func EquityChart(rows []tickerdata.Attempt, allLen int) string {
	const (
		w   = 900
		h   = 320
		l   = 56
		r   = 24
		top = 16
		bot = h - 36
	)
	if len(rows) == 0 {
		title, body := "Waiting for first fill", "The curve starts at $0.00 when the first order fills, and every gate that fires is marked on it. The journal is empty — nothing is broken."
		if allLen > 0 {
			title, body = "No attempts in this range", "Pick a wider range to see the tape."
		}
		return `<div class="empty">` + iconSVG("hourglass", 24) + `<div><strong>` + escText(title) + `</strong>` + escText(body) + `</div></div>`
	}

	tMin, tMax := rows[0].TS.UnixMilli(), rows[len(rows)-1].TS.UnixMilli()
	span := float64(tMax - tMin)
	if span == 0 {
		span = 1
	}
	x := func(ms int64) float64 {
		return l + float64(ms-tMin)/span*float64(w-l-r-24)
	}

	cum := 0.0
	type pt struct {
		r    tickerdata.Attempt
		x, v float64
		kind tickerdata.Kind
	}
	pts := make([]pt, 0, len(rows))
	vals := make([]float64, 0, len(rows))
	for _, row := range rows {
		if row.Outcome == tickerdata.Filled {
			cum += row.PnL
		}
		pts = append(pts, pt{r: row, x: x(row.TS.UnixMilli()), v: cum, kind: row.Outcome.Meta().Kind})
		vals = append(vals, cum)
	}

	lo, hi := 0.0, 10.0
	for _, v := range vals {
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	step := math.Max(10, math.Ceil((hi-lo)/4/10)*10)
	lo = math.Floor(lo/step) * step
	hi = lo + step*4
	for _, v := range vals {
		if v > hi-step {
			hi += step
			break
		}
	}
	y := func(v float64) float64 { return top + (bot-top)*(1-(v-lo)/(hi-lo)) }

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg viewBox="0 0 %d %d" role="img" aria-label="Cumulative realized P&amp;L over time with every attempt marked; use the recent gate events list for the details">`, w, h))
	b.WriteString(`<defs><linearGradient id="eqfill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#D2F23C" stop-opacity=".28"/><stop offset="1" stop-color="#D2F23C" stop-opacity="0"/></linearGradient></defs>`)

	for v := lo; v <= hi+1e-9; v += step {
		stroke, dash := "#ffffff0d", ""
		label := fmt.Sprintf("$%d", int64(v))
		if math.Abs(v) < 1e-9 {
			stroke, dash, label = "#ffffff33", ` stroke-dasharray="4 4"`, "$0"
		}
		b.WriteString(fmt.Sprintf(`<line x1="%d" x2="%d" y1="%.1f" y2="%.1f" stroke="%s"%s/><text x="%d" y="%.1f" text-anchor="end" fill="#7F877C" font-size="11" font-family="ui-monospace,Menlo,monospace">%s</text>`,
			l, w-r, y(v), y(v), stroke, dash, l-10, y(v)+4, escText(label)))
	}

	maxN := 0.0
	for _, row := range rows {
		maxN = math.Max(maxN, row.Notional)
	}
	colours := map[tickerdata.Kind]struct{ c, op, dash string }{
		tickerdata.KindOK:  {"#D2F23C", ".42", ""},
		tickerdata.KindBlk: {"#E8B33C", ".7", `2 3`},
		tickerdata.KindBad: {"#FF5A5F", ".85", ""},
	}
	for _, p := range pts {
		col := colours[p.kind]
		height := (bot - top) * 0.62 * math.Max(.12, p.r.Notional/maxN)
		for k := -2; k <= 2; k++ {
			dash := ""
			if col.dash != "" {
				dash = fmt.Sprintf(` stroke-dasharray="%s"`, col.dash)
			}
			b.WriteString(fmt.Sprintf(`<line x1="%.1f" x2="%.1f" y1="%d" y2="%.1f" stroke="%s" stroke-opacity="%s" stroke-width="1"%s/>`,
				p.x+float64(k)*3, p.x+float64(k)*3, bot, bot-height, col.c, col.op, dash))
		}
	}

	line := strings.Builder{}
	for i, p := range pts {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		line.WriteString(fmt.Sprintf("%s%.1f %.1f ", cmd, p.x, y(p.v)))
	}
	d := strings.TrimSpace(line.String())
	b.WriteString(fmt.Sprintf(`<path d="%s L%.1f %d L%.1f %dZ" fill="url(#eqfill)"/>`, d, pts[len(pts)-1].x, bot, pts[0].x, bot))
	b.WriteString(fmt.Sprintf(`<path d="%s" fill="none" stroke="#D2F23C" stroke-width="2" stroke-linejoin="round"/>`, d))

	for _, p := range pts {
		cx, cy := p.x, y(p.v)
		label1 := fmt.Sprintf("#%s · %s %s", Pad4(p.r.ID), p.r.Symbol, strings.ToUpper(p.r.Side))
		label2, colour := "", "#D2F23C"
		switch p.kind {
		case tickerdata.KindOK:
			label2 = fmt.Sprintf("%s · total %s", USDSigned(p.r.PnL), USDSigned(p.v))
		case tickerdata.KindBlk:
			label2, colour = strings.Split(ReasonText(p.r), " — ")[0], "#E8B33C"
		default:
			label2, colour = "Venue rejected", "#FF5A5F"
		}
		b.WriteString(fmt.Sprintf(`<a class="pt" href="%s" hx-get="%s" hx-target="#view" hx-select="#view" hx-swap="outerHTML" hx-push-url="true" data-x="%.1f" data-y="%.1f" data-l1="%s" data-l2="%s" data-c="%s" aria-label="%s">`,
			escText(JournalURL(tickerdata.Filter{}, p.r.ID)), escText(JournalURL(tickerdata.Filter{}, p.r.ID)),
			cx, cy, escText(label1), escText(label2), colour, escText(label1+": "+label2)))
		switch p.kind {
		case tickerdata.KindBlk:
			b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="5" fill="#060807" stroke="#E8B33C" stroke-width="1.5" stroke-dasharray="2 2"/>`, cx, cy))
		case tickerdata.KindBad:
			b.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="9" height="9" fill="#FF5A5F" stroke="#060807" stroke-width="1.5"/>`, cx-4.5, cy-4.5))
		default:
			b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="3" fill="#D2F23C"/>`, cx, cy))
		}
		b.WriteString(`<circle cx="` + fmt.Sprintf("%.1f", cx) + `" cy="` + fmt.Sprintf("%.1f", cy) + `" r="9" fill="transparent"/></a>`)
	}

	b.WriteString(`<g class="xh" pointer-events="none"></g>`)
	for i := 0; i <= 4; i++ {
		ms := tMin + (tMax-tMin)*int64(i)/4
		anchor := []string{"start", "middle", "middle", "middle", "end"}[i]
		t := rows[0].TS.Add(time.Millisecond * time.Duration(ms-tMin))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%d" text-anchor="%s" fill="#7F877C" font-size="11" font-family="ui-monospace,Menlo,monospace">%s</text>`,
			x(ms), h-10, anchor, escText(MD(t)+" "+HMS(t)[:5])))
	}
	b.WriteString(`<rect x="0" y="0" width="900" height="320" fill="transparent" class="hit"/></svg>`)
	return b.String()
}

// CalibrationChart renders the win-rate-vs-stated-probability scatter.
func CalibrationChart(buckets []tickerdata.Bucket, limits tickerdata.Limits) string {
	const (
		w   = 600
		h   = 500
		l   = 44
		r   = 12
		top = 20
		bot = h - 40
		lo  = 0.4
		hi  = 1.0
	)
	x := func(v float64) float64 { return l + float64(w-l-r)*(v-lo)/(hi-lo) }
	y := func(v float64) float64 { return top + float64(bot-top)*(1-(v-lo)/(hi-lo)) }
	const mono = `font-family="ui-monospace,Menlo,monospace"`

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg viewBox="0 0 %d %d" role="img" aria-label="Calibration plot: realized win rate against stated judge probability">`, w, h))
	b.WriteString(`<defs><pattern id="hatch" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="6" height="6" fill="#141816"/><line x1="0" y1="0" x2="0" y2="6" stroke="#A9B0A6" stroke-width="2"/></pattern></defs>`)
	for i := 0; i <= 6; i++ {
		v := lo + float64(i)/10
		b.WriteString(fmt.Sprintf(`<line x1="%d" x2="%d" y1="%.1f" y2="%.1f" stroke="#ffffff0d"/><line y1="%d" y2="%d" x1="%.1f" x2="%.1f" stroke="#ffffff0d"/><text x="%d" y="%.1f" text-anchor="end" fill="#7F877C" font-size="11" %s>%.1f</text><text x="%.1f" y="%d" text-anchor="middle" fill="#7F877C" font-size="11" %s>%.1f</text>`,
			l, w-r, y(v), y(v), top, bot, x(v), x(v), l-8, y(v)+4, mono, v, x(v), bot+16, mono, v))
	}
	b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#7F877C" stroke-width="1.5" stroke-dasharray="6 5"/><text x="%.1f" y="%.1f" fill="#7F877C" font-size="11" transform="rotate(-39 %.1f %.1f)">perfectly calibrated</text>`,
		x(.4), y(.4), x(1), y(1), x(.62), y(.62)-8, x(.62), y(.62)-8))

	var good [][2]float64
	var marks strings.Builder
	for i, bk := range buckets {
		if bk.N == 0 {
			continue
		}
		mid := bk.Midpoint(i)
		wr := float64(bk.Wins) / float64(bk.N)
		cx, cy := x(mid), y(wr)
		if bk.N >= limits.MinN {
			good = append(good, [2]float64{cx, cy})
			marks.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="7" fill="#D2F23C" stroke="#0D100F" stroke-width="2"><title>%s: n=%d, win rate %s</title></circle>`,
				cx, cy, escText(bk.Label), bk.N, Pct(wr, 1)))
			continue
		}
		marks.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="18" height="18" rx="4" fill="url(#hatch)" stroke="#A9B0A6" stroke-width="1.5" stroke-dasharray="3 2"><title>%s: n=%d — insufficient (n&lt;%d)</title></rect><text x="%.1f" y="%.1f" text-anchor="middle" fill="#A9B0A6" font-size="11" %s>n=%d · insufficient</text>`,
			cx-9, cy-9, escText(bk.Label), bk.N, limits.MinN, math.Min(cx, float64(w-r-64)), cy+28, mono, bk.N))
	}
	if len(good) > 1 {
		path := strings.Builder{}
		for i, p := range good {
			cmd := "L"
			if i == 0 {
				cmd = "M"
			}
			path.WriteString(fmt.Sprintf("%s%.1f %.1f ", cmd, p[0], p[1]))
		}
		b.WriteString(fmt.Sprintf(`<path d="%s" fill="none" stroke="#D2F23C" stroke-width="2"/>`, strings.TrimSpace(path.String())))
	}
	b.WriteString(marks.String())
	b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%d" text-anchor="middle" fill="#7F877C" font-size="11">Stated judge probability (bucket midpoint)</text>`, float64(l+w-r)/2, h-4))
	b.WriteString(fmt.Sprintf(`<text transform="translate(12 %.1f) rotate(-90)" text-anchor="middle" fill="#7F877C" font-size="11">Realized win rate</text>`, float64(top+bot)/2))
	b.WriteString(`</svg>`)
	return b.String()
}

// Ticks renders the segmented utilisation meter.
func Ticks(p float64, n int) string {
	if n <= 0 {
		n = 36
	}
	var b strings.Builder
	b.WriteString(`<div class="ticks" aria-hidden="true">`)
	for i := 1; i <= n; i++ {
		frac := float64(i) / float64(n)
		class := ""
		if frac <= p+1e-9 {
			class = "on"
			if frac > 0.7 {
				class = "warn"
			}
		}
		b.WriteString(`<i class="` + class + `"></i>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// iconSVG renders one icon from the shared stroke set.
func iconSVG(name string, size int) string {
	path, ok := icons[name]
	if !ok {
		path = icons["minus"]
	}
	style := ""
	if size > 0 {
		style = fmt.Sprintf(` style="width:%dpx;height:%dpx"`, size, size)
	}
	return `<svg class="od-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"` + style + `>` + path + `</svg>`
}

var icons = map[string]string{
	"check":     `<path d="M5 12.5l4.5 4.5L19 7.5"/>`,
	"slash":     `<circle cx="12" cy="12" r="8.5"/><path d="M6 6l12 12"/>`,
	"bang":      `<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5v5.5M12 16.5v.01"/>`,
	"hourglass": `<path d="M6 3h12M6 21h12M7 3c0 5 5 6 5 9s-5 4-5 9M17 3c0 5-5 6-5 9s5 4 5 9"/>`,
	"dot":       `<circle cx="12" cy="12" r="5" fill="currentColor"/>`,
	"ring":      `<circle cx="12" cy="12" r="5"/>`,
	"minus":     `<path d="M6 12h12"/>`,
	"x":         `<path d="M6 6l12 12M18 6L6 18"/>`,
	"up":        `<path d="M12 5l7 8H5z" fill="currentColor"/>`,
	"down":      `<path d="M12 19l-7-8h14z" fill="currentColor"/>`,
	"lock":      `<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>`,
}

// escText escapes text for the raw-SVG helpers above; templ escapes component
// output, but these strings are assembled by hand.
func escText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}
