package book

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// decode parses a fixture into the response type it models.
func decode[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return v
}

func TestNormalizeISBN(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"9780132350884", "9780132350884", true},
		{"978-0-13-235088-4", "9780132350884", true},
		{" 978 0132 3508 84 ", "9780132350884", true},
		{"0132350882", "0132350882", true},
		{"", "", false},
		{"hello", "", false},
		{"978013235088", "", false}, // 12 digits
	}
	for _, c := range cases {
		got, err := NormalizeISBN(c.in)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("NormalizeISBN(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("NormalizeISBN(%q) = %q, nil; want error", c.in, got)
		}
	}
}

func TestISBN10to13(t *testing.T) {
	if got := ISBN10to13("0132350882"); got != "9780132350884" {
		t.Errorf("ISBN10to13 = %q, want 9780132350884", got)
	}
	if got := ISBN10to13("9780132350884"); got != "9780132350884" { // not 10 digits: passthrough
		t.Errorf("ISBN10to13 passthrough = %q", got)
	}
}

func TestYear(t *testing.T) {
	cases := map[string]int{
		"2008":           2008,
		"2008-07-14":     2008,
		"July 2008":      2008,
		"2008 (Reprint)": 2008,
		"no year here":   0,
		"":               0,
	}
	for in, want := range cases {
		if got := year(in); got != want {
			t.Errorf("year(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFromGoogleBooks(t *testing.T) {
	resp := decode[googleBooksResponse](t, fixture(t, "gb-volume.json"))
	b, ok := fromGoogleBooks(resp.Items[0])
	if !ok {
		t.Fatal("fromGoogleBooks rejected a valid volume")
	}
	if b.Title != "Clean Code" || b.Subtitle != "A Handbook of Agile Software Craftsmanship" {
		t.Errorf("title/subtitle = %q / %q", b.Title, b.Subtitle)
	}
	if len(b.Authors) != 1 || b.Authors[0] != "Robert C. Martin" {
		t.Errorf("authors = %v", b.Authors)
	}
	if b.PublishedYear != 2008 || b.PageCount != 431 {
		t.Errorf("year/pages = %d/%d", b.PublishedYear, b.PageCount)
	}
	if b.ISBN != "9780132350884" {
		t.Errorf("ISBN = %q", b.ISBN)
	}
	// The largest present image (large) wins.
	if !strings.HasSuffix(b.CoverURL, "zoom=4") {
		t.Errorf("cover = %q, want the large image", b.CoverURL)
	}
	if b.Source != "googlebooks" || b.SourceURL == "" {
		t.Errorf("source = %q, url = %q", b.Source, b.SourceURL)
	}
}

func TestFromOpenLibrary(t *testing.T) {
	resp := decode[map[string]openLibraryData](t, fixture(t, "ol-data.json"))
	b := fromOpenLibrary(resp["ISBN:9780132350884"])
	if b.Title != "Clean Code" || b.PageCount != 431 {
		t.Errorf("title/pages = %q/%d", b.Title, b.PageCount)
	}
	if b.PublishedYear != 2008 { // "July 2008"
		t.Errorf("year = %d, want 2008", b.PublishedYear)
	}
	if b.ISBN != "9780132350884" {
		t.Errorf("ISBN = %q", b.ISBN)
	}
	if b.Publisher != "Prentice Hall" || len(b.Authors) != 1 || b.Authors[0] != "Robert C. Martin" {
		t.Errorf("publisher/authors = %q/%v", b.Publisher, b.Authors)
	}
	// The largest cover size present wins.
	if b.CoverURL != "https://covers.openlibrary.org/b/id/15126503-L.jpg" {
		t.Errorf("cover = %q, want the large image", b.CoverURL)
	}
	if b.SourceURL == "" {
		t.Error("SourceURL is empty")
	}
}

func TestOpenLibrarySearch(t *testing.T) {
	resp := decode[openLibrarySearchResponse](t, fixture(t, "ol-search.json"))
	books := make([]Book, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		b := Book{Title: d.Title, Authors: d.AuthorName, PublishedYear: d.FirstPublishYear, PageCount: d.NumberOfPagesMedian}
		if d.CoverI > 0 {
			b.CoverURL = "https://covers.openlibrary.org/b/id/" + itoa(d.CoverI) + "-L.jpg"
		}
		books = append(books, b)
	}
	if len(books) != 2 || books[0].Title != "Clean Code" {
		t.Fatalf("books = %v", books)
	}
	if books[0].PageCount != 431 || books[0].PublishedYear != 2008 {
		t.Errorf("first hit fields = %d/%d", books[0].PageCount, books[0].PublishedYear)
	}
	if books[1].CoverURL != "https://covers.openlibrary.org/b/id/11857731-L.jpg" {
		t.Errorf("cover URL = %q", books[1].CoverURL)
	}
}

// mockAPI serves canned responses on the Google Books and Open Library paths a
// Client hits, letting the fallback logic be tested without the network.
func mockAPI(t *testing.T, gbStatus int, gbBody, olData, olSearch string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/gb/volumes", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(gbStatus)
		ioWrite(w, gbBody)
	})
	mux.HandleFunc("/ol/api/books", func(w http.ResponseWriter, r *http.Request) {
		ioWrite(w, olData)
	})
	mux.HandleFunc("/ol/search.json", func(w http.ResponseWriter, r *http.Request) {
		ioWrite(w, olSearch)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newTestClient(srv.Client(), srv.URL+"/gb", srv.URL+"/ol")
}

func ioWrite(w http.ResponseWriter, s string) {
	if _, err := w.Write([]byte(s)); err != nil {
		panic(err)
	}
}

func TestLookupISBNGooglePrimary(t *testing.T) {
	c := mockAPI(t, http.StatusOK, fixture(t, "gb-volume.json"), olDataEmpty, olSearchEmpty)
	b, err := c.LookupISBN(context.Background(), "9780132350884")
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != "googlebooks" || b.Title != "Clean Code" || b.PageCount != 431 {
		t.Errorf("got source=%q title=%q pages=%d", b.Source, b.Title, b.PageCount)
	}
}

func TestLookupISBNFallsBackOnQuota(t *testing.T) {
	// Google 429s (the shared-quota case); Open Library answers.
	c := mockAPI(t, http.StatusTooManyRequests, fixture(t, "gb-quota.json"), fixture(t, "ol-data.json"), olSearchEmpty)
	b, err := c.LookupISBN(context.Background(), "9780132350884")
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != "openlibrary" || b.Title != "Clean Code" || b.ISBN != "9780132350884" {
		t.Errorf("got source=%q title=%q isbn=%q", b.Source, b.Title, b.ISBN)
	}
	if b.PublishedYear != 2008 || b.PageCount != 431 {
		t.Errorf("year/pages = %d/%d", b.PublishedYear, b.PageCount)
	}
}

func TestLookupISBNFallsBackOnEmpty(t *testing.T) {
	// Google answers 200 but has no volume; Open Library does.
	c := mockAPI(t, http.StatusOK, fixture(t, "gb-empty.json"), fixture(t, "ol-data.json"), olSearchEmpty)
	b, err := c.LookupISBN(context.Background(), "9780132350884")
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != "openlibrary" {
		t.Errorf("source = %q, want openlibrary", b.Source)
	}
}

func TestLookupISBNNotFound(t *testing.T) {
	c := mockAPI(t, http.StatusOK, fixture(t, "gb-empty.json"), olDataEmpty, olSearchEmpty)
	b, err := c.LookupISBN(context.Background(), "9780000000000")
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "" {
		t.Errorf("expected empty book, got %+v", b)
	}
}

func TestSearchTitleFallsBackToOpenLibrary(t *testing.T) {
	c := mockAPI(t, http.StatusOK, fixture(t, "gb-empty.json"), olDataEmpty, fixture(t, "ol-search.json"))
	books, err := c.SearchTitle(context.Background(), "Clean Code")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("got %d candidates", len(books))
	}
	if books[0].Source != "openlibrary" || books[0].Title != "Clean Code" {
		t.Errorf("best candidate = %+v", books[0])
	}
	if books[0].PageCount != 431 || books[0].PublishedYear != 2008 {
		t.Errorf("best candidate fields = %d/%d", books[0].PageCount, books[0].PublishedYear)
	}
}

func TestSearchTitleNoResults(t *testing.T) {
	c := mockAPI(t, http.StatusOK, fixture(t, "gb-empty.json"), olDataEmpty, olSearchEmpty)
	books, err := c.SearchTitle(context.Background(), "zzz nonexistent book zzz")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 0 {
		t.Errorf("expected no candidates, got %d", len(books))
	}
}

func TestNoteBodyComplete(t *testing.T) {
	b := Book{
		Title:         "Clean Code",
		Authors:       []string{"Robert C. Martin"},
		Publisher:     "Prentice Hall",
		PublishedYear: 2008,
		PageCount:     431,
		ISBN:          "9780132350884",
		Source:        "openlibrary",
		SourceURL:     "http://openlibrary.org/books/OL26222911M/Clean_Code",
		Description:   "Even bad code can function.",
	}
	got := NoteBody(b, "assets/cover-9780132350884.jpg", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"# Clean Code",
		"![cover](assets/cover-9780132350884.jpg)",
		"- Author: Robert C. Martin",
		"- Publisher: Prentice Hall",
		"- Published: 2008",
		"- Pages: 431",
		"- ISBN: 9780132350884",
		"- Source: [openlibrary](http://openlibrary.org/books/OL26222911M/Clean_Code)",
		"## About",
		"Even bad code can function.",
		"## Notes",
		"## Quotes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("NoteBody missing %q\n%s", want, got)
		}
	}
}

func TestNoteBodySparse(t *testing.T) {
	// A lookup with no authors, pages, year, or cover must not emit stub lines or
	// an empty image block.
	b := Book{Title: "Untitled", ISBN: "9780000000000"}
	got := NoteBody(b, "", time.Now())
	for _, forbidden := range []string{"![cover]", "- Author:", "- Pages:", "- Published:", "## About"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("NoteBody should not contain %q\n%s", forbidden, got)
		}
	}
	if !strings.HasPrefix(got, "# Untitled\n") {
		t.Errorf("NoteBody should start with the H1 title\n%s", got)
	}
}

func TestFetchCover(t *testing.T) {
	img := []byte("fake-jpeg-bytes")
	mux := http.NewServeMux()
	mux.HandleFunc("/cover-L.jpg", func(w http.ResponseWriter, r *http.Request) { ioWrite(w, string(img)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(srv.Client(), srv.URL, srv.URL)
	b := Book{Title: "Clean Code", ISBN: "9780132350884", CoverURL: srv.URL + "/cover-L.jpg"}
	dir := t.TempDir()
	ref, err := c.FetchCover(context.Background(), dir, b)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "assets/cover-9780132350884.jpg" {
		t.Errorf("ref = %q", ref)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cover-9780132350884.jpg"))
	if err != nil || string(data) != string(img) {
		t.Errorf("downloaded file = %q, %v", data, err)
	}
}

func TestFetchCoverNoCover(t *testing.T) {
	c := newTestClient(http.DefaultClient, "", "")
	ref, err := c.FetchCover(context.Background(), t.TempDir(), Book{Title: "X"})
	if err != nil || ref != "" {
		t.Errorf("ref = %q, err = %v; want empty ref and nil error", ref, err)
	}
}

func TestFetchCoverFailureFallsBackToRemote(t *testing.T) {
	// A 404 cover must be reported as an error (the CLI then keeps the remote URL).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.Client(), srv.URL, srv.URL)
	_, err := c.FetchCover(context.Background(), t.TempDir(), Book{CoverURL: srv.URL + "/missing.jpg"})
	if err == nil {
		t.Fatal("expected an error for a 404 cover")
	}
}

func TestCoverFileName(t *testing.T) {
	if got := coverFileName(Book{ISBN: "9780132350884", CoverURL: "https://x/cover-L.jpg"}); got != "cover-9780132350884.jpg" {
		t.Errorf("got %q", got)
	}
	if got := coverFileName(Book{Title: "Clean Code in Python", CoverURL: "https://x/cover.png"}); got != "cover-Clean-Code-in-Python.png" {
		t.Errorf("got %q", got)
	}
	if got := coverFileName(Book{Title: "？？？", CoverURL: "https://x/cover"}); got != "cover-？？？.jpg" {
		t.Errorf("got %q", got)
	}
}

const (
	olDataEmpty   = `{}`
	olSearchEmpty = `{"numFound": 0, "docs": []}`
)
