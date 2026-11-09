package server

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jomakori/tickerlight/internal/tickerdata"
)

// The header must name the source the rows actually came from. A console
// showing the real attempt journal while the corner badge says "Sample data"
// would be lying about its own evidence, which is the one thing an operator
// reads it for.
func TestHeaderNamesTheJournalSource(t *testing.T) {
	h := New(Options{
		Rows:   tickerdata.Sample(),
		Now:    func() time.Time { return tickerdata.SampleNow },
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Source: "attempt journal",
	})

	for _, path := range []string{"/", "/journal", "/calibration", "/risk"} {
		body := fetch(t, h, http.MethodGet, path, nil).body
		if !strings.Contains(body, "Attempt journal · paper only") {
			t.Fatalf("%s: badge does not name the journal source", path)
		}
		if strings.Contains(body, banner) {
			t.Fatalf("%s: badge still claims sample data while reading a journal", path)
		}
		if !strings.Contains(body, "paper only") {
			t.Fatalf("%s: paper-only reminder dropped", path)
		}
	}
}

// Without a source the console keeps its original wording.
func TestHeaderDefaultsToSampleData(t *testing.T) {
	body := fetch(t, fixture(t), http.MethodGet, "/", nil).body
	if !strings.Contains(body, banner) {
		t.Fatal("default fixture no longer identifies itself as sample data")
	}
	if strings.Contains(body, "Attempt journal") {
		t.Fatal("sample fixture claims to be reading a journal")
	}
}
