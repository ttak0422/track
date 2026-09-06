// Package jquants converts J-Quants daily quotes into Canonical Data Model price records — the
// engine behind the track-fetch-jquants tool (see docs/spec/fetch.md). It depends only on the dataset
// contract, not on the track CLI or store.
package jquants

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ttak0422/track/internal/track/dataset"
)

// DefaultBaseURL is the production J-Quants API root.
const DefaultBaseURL = "https://api.jquants.com/v1"

// Client calls the J-Quants API. BaseURL is overridable so tests (and a future proxy) can point it
// at another server.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// IDToken exchanges a long-lived refresh token for the short-lived ID token every data endpoint
// requires as its bearer credential.
func (c *Client) IDToken(refreshToken string) (string, error) {
	u := c.BaseURL + "/token/auth_refresh?refreshtoken=" + url.QueryEscape(refreshToken)
	resp, err := c.HTTP.Post(u, "application/json", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", apiError("auth_refresh", resp)
	}
	var body struct {
		IDToken string `json:"idToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("auth_refresh: %w", err)
	}
	if body.IDToken == "" {
		return "", fmt.Errorf("auth_refresh: response carries no idToken")
	}
	return body.IDToken, nil
}

// Bar is one daily OHLCV bar.
type Bar struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume *float64 // nil when the source reports no volume
}

// row mirrors one daily_quotes entry. Prices are pointers because the API returns null for days an
// issue did not trade; Adjustment* fields carry split-adjusted values.
type row struct {
	Date             string   `json:"Date"`
	Open             *float64 `json:"Open"`
	High             *float64 `json:"High"`
	Low              *float64 `json:"Low"`
	Close            *float64 `json:"Close"`
	Volume           *float64 `json:"Volume"`
	AdjustmentOpen   *float64 `json:"AdjustmentOpen"`
	AdjustmentHigh   *float64 `json:"AdjustmentHigh"`
	AdjustmentLow    *float64 `json:"AdjustmentLow"`
	AdjustmentClose  *float64 `json:"AdjustmentClose"`
	AdjustmentVolume *float64 `json:"AdjustmentVolume"`
}

// DailyQuotes fetches the daily bars for one issue code, following pagination until the server stops
// returning a pagination key. from/to bound the range when non-empty (YYYY-MM-DD). Rows without a
// full set of prices (halted or non-trading days) are counted and skipped, never emitted
// half-formed. Split-adjusted prices are preferred over raw ones so a chart survives a stock split.
func (c *Client) DailyQuotes(idToken, code, from, to string) ([]Bar, int, error) {
	var bars []Bar
	skipped := 0
	paginationKey := ""
	for {
		q := url.Values{"code": {code}}
		if from != "" {
			q.Set("from", from)
		}
		if to != "" {
			q.Set("to", to)
		}
		if paginationKey != "" {
			q.Set("pagination_key", paginationKey)
		}
		page, next, err := c.dailyQuotesPage(idToken, q)
		if err != nil {
			return nil, 0, err
		}
		for _, r := range page {
			bar, ok, err := r.bar()
			if err != nil {
				return nil, 0, err
			}
			if !ok {
				skipped++
				continue
			}
			bars = append(bars, bar)
		}
		if next == "" {
			return bars, skipped, nil
		}
		paginationKey = next
	}
}

// dailyQuotesPage fetches one page of daily quotes, returning its rows and the pagination key of the
// next page ("" on the last one).
func (c *Client) dailyQuotesPage(idToken string, q url.Values) ([]row, string, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/prices/daily_quotes?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+idToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", apiError("daily_quotes", resp)
	}
	var body struct {
		DailyQuotes   []row  `json:"daily_quotes"`
		PaginationKey string `json:"pagination_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", fmt.Errorf("daily_quotes: %w", err)
	}
	return body.DailyQuotes, body.PaginationKey, nil
}

// bar converts one API row into a Bar. ok is false for rows without a complete OHLC set (halted
// days), which callers count rather than emit — the dataset contract requires every price field.
func (r row) bar() (Bar, bool, error) {
	t, err := parseDate(r.Date)
	if err != nil {
		return Bar{}, false, err
	}
	open, high, low, close_ := r.Open, r.High, r.Low, r.Close
	volume := r.Volume
	// A row that carries adjusted prices carries all four; mixing raw and adjusted would skew a bar.
	if r.AdjustmentOpen != nil && r.AdjustmentHigh != nil && r.AdjustmentLow != nil && r.AdjustmentClose != nil {
		open, high, low, close_ = r.AdjustmentOpen, r.AdjustmentHigh, r.AdjustmentLow, r.AdjustmentClose
		if r.AdjustmentVolume != nil {
			volume = r.AdjustmentVolume
		}
	}
	if open == nil || high == nil || low == nil || close_ == nil {
		return Bar{}, false, nil
	}
	return Bar{Time: t, Open: *open, High: *high, Low: *low, Close: *close_, Volume: volume}, true, nil
}

// dateFormats are the layouts daily_quotes has used across API revisions.
var dateFormats = []string{"2006-01-02", "20060102"}

// parseDate normalizes a daily_quotes date.
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range dateFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q", s)
}

// apiError renders a non-200 response as an error, keeping the server's message (J-Quants reports
// failures as {"message": ...}) without dumping a whole body.
func apiError(endpoint string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(body))
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Message != "" {
		msg = payload.Message
	}
	if msg == "" {
		return fmt.Errorf("%s: HTTP %s", endpoint, resp.Status)
	}
	return fmt.Errorf("%s: HTTP %s: %s", endpoint, resp.Status, msg)
}

// Prices maps bars onto canonical price records (docs/spec/fetch.md): ordered ascending, each
// validated against the price kind so the tool can never emit a non-conformant line. Daily bars
// carry a date-only time — the day is the bar's whole identity. entity names the series on every
// record.
func Prices(bars []Bar, entity string) ([]dataset.Record, error) {
	sorted := make([]Bar, len(bars))
	copy(sorted, bars)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	records := make([]dataset.Record, 0, len(sorted))
	for _, b := range sorted {
		rec := dataset.Record{
			"version": dataset.SchemaVersion,
			"entity":  entity,
			"time":    b.Time.Format("2006-01-02"),
			"open":    b.Open,
			"high":    b.High,
			"low":     b.Low,
			"close":   b.Close,
		}
		if b.Volume != nil {
			rec["volume"] = *b.Volume
		}
		records = append(records, rec)
	}
	if err := dataset.ValidateRecords(dataset.KindPrice, records); err != nil {
		return nil, err
	}
	return records, nil
}
