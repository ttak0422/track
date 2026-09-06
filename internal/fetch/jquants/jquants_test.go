package jquants

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// server fakes the two endpoints the client touches: the token exchange and a paginated
// daily_quotes. Rows are synthetic — code 99990 does not exist.
func server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token/auth_refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Get("refreshtoken") != "refresh-1" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message": "The incoming token is invalid or expired."}`)
			return
		}
		fmt.Fprint(w, `{"idToken": "id-1"}`)
	})
	mux.HandleFunc("/prices/daily_quotes", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer id-1" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message": "Missing Authentication Token."}`)
			return
		}
		if code := r.URL.Query().Get("code"); code != "99990" {
			t.Errorf("code = %q, want 99990", code)
		}
		if r.URL.Query().Get("pagination_key") == "" {
			// Page 1: an adjusted row (out of order) and a halted day full of nulls.
			fmt.Fprint(w, `{"daily_quotes": [
				{"Date": "2026-01-06", "Open": 200, "High": 220, "Low": 190, "Close": 210, "Volume": 5000,
				 "AdjustmentOpen": 100, "AdjustmentHigh": 110, "AdjustmentLow": 95, "AdjustmentClose": 105, "AdjustmentVolume": 10000},
				{"Date": "2026-01-07", "Open": null, "High": null, "Low": null, "Close": null, "Volume": null}
			], "pagination_key": "page-2"}`)
			return
		}
		// Page 2: a plain raw row in the older compact date format, volume omitted.
		fmt.Fprint(w, `{"daily_quotes": [
			{"Date": "20260105", "Open": 98, "High": 102, "Low": 97, "Close": 100}
		]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func client(srv *httptest.Server) *Client {
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestIDToken(t *testing.T) {
	c := client(server(t))
	tok, err := c.IDToken("refresh-1")
	if err != nil {
		t.Fatalf("IDToken: %v", err)
	}
	if tok != "id-1" {
		t.Errorf("token = %q, want id-1", tok)
	}
	if _, err := c.IDToken("stale"); err == nil {
		t.Fatal("stale refresh token: want error")
	} else if want := "invalid or expired"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not carry the server message %q", err, want)
	}
}

func TestDailyQuotes(t *testing.T) {
	c := client(server(t))
	bars, skipped, err := c.DailyQuotes("id-1", "99990", "2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatalf("DailyQuotes: %v", err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (the all-null day)", skipped)
	}
	if len(bars) != 2 {
		t.Fatalf("bars = %d, want 2", len(bars))
	}
	// Adjusted prices win over raw ones when the full set is present.
	adj := bars[0]
	if adj.Open != 100 || adj.High != 110 || adj.Low != 95 || adj.Close != 105 {
		t.Errorf("adjusted bar = %+v, want 100/110/95/105", adj)
	}
	if adj.Volume == nil || *adj.Volume != 10000 {
		t.Errorf("adjusted volume = %v, want 10000", adj.Volume)
	}
	if bars[1].Volume != nil {
		t.Errorf("raw bar volume = %v, want nil", bars[1].Volume)
	}
	if got := bars[1].Time.Format("2006-01-02"); got != "2026-01-05" {
		t.Errorf("compact date parsed as %s, want 2026-01-05", got)
	}
}

func TestPrices(t *testing.T) {
	c := client(server(t))
	bars, _, err := c.DailyQuotes("id-1", "99990", "", "")
	if err != nil {
		t.Fatalf("DailyQuotes: %v", err)
	}
	records, err := Prices(bars, "TEST")
	if err != nil {
		t.Fatalf("Prices: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	// Ascending by time even though page 2 carried the earlier day.
	if t0, _ := records[0].String("time"); t0 != "2026-01-05" {
		t.Errorf("records[0].time = %s, want 2026-01-05", t0)
	}
	for i, rec := range records {
		if err := dataset.Validate(dataset.KindPrice, rec); err != nil {
			t.Errorf("record %d invalid: %v", i, err)
		}
		if ent, _ := rec.String("entity"); ent != "TEST" {
			t.Errorf("record %d entity = %q, want TEST", i, ent)
		}
	}
	if _, ok := records[0]["volume"]; ok {
		t.Error("volume-less bar must omit the field, not write a zero")
	}
	// A record line must round-trip as one JSONL object.
	if _, err := json.Marshal(records[0]); err != nil {
		t.Errorf("marshal: %v", err)
	}
}

func TestParseDateRejectsGarbage(t *testing.T) {
	if _, err := parseDate("Jan 5"); err == nil {
		t.Fatal("want error for unrecognized date")
	}
	if _, err := parseDate(""); err == nil {
		t.Fatal("want error for empty date")
	}
}

func TestBarTimes(t *testing.T) {
	got, err := parseDate("2026-01-06")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("parseDate = %v, want %v", got, want)
	}
}
