package book

import (
	"context"
	"net/url"
	"sort"
	"strings"
)

// LookupISBN resolves an edition by ISBN. Google Books (q=isbn:…) is tried first;
// Open Library's bibkeys endpoint answers when Google is rate-limited, errors, or
// lacks the edition. The ISBN in the result is the normalized 13-digit form when
// derivable.
func (c *Client) LookupISBN(ctx context.Context, isbn string) (Book, error) {
	digits, err := NormalizeISBN(isbn)
	if err != nil {
		return Book{}, err
	}
	b, err := c.googleBooksByISBN(ctx, digits)
	if err == nil && b.Title != "" {
		return b, nil
	}
	return c.openLibraryByISBN(ctx, digits)
}

// SearchTitle resolves a title query to ranked candidates: the Google Books
// results when it answers, Open Library's search index otherwise. Each source
// gets its own query dialect (Google supports intitle:, Open Library's plain
// search does not). Candidates are ranked by how complete they are
// (pages/year/authors/cover make a better reading note), so the best edition is
// [0] and the caller can present the rest on stderr.
func (c *Client) SearchTitle(ctx context.Context, title string) ([]Book, error) {
	q := strings.TrimSpace(title)
	if q == "" {
		return nil, nil
	}
	books, err := c.googleBooksSearch(ctx, "intitle:"+url.QueryEscape(q))
	if err != nil || len(books) == 0 {
		books, err = c.openLibrarySearch(ctx, q)
	}
	if err != nil {
		return nil, err
	}
	rankBooks(books, q)
	return books, nil
}

// --- Google Books ---

// googleBooksVolume mirrors the shape of the Google Books volumes API
// (https://developers.google.com/books/docs/v1/reference/volumes/list) that the
// tool reads; unknown fields are ignored.
type googleBooksVolume struct {
	VolumeInfo struct {
		Title               string `json:"title"`
		Subtitle            string `json:"subtitle"`
		Authors             []string
		Publisher           string            `json:"publisher"`
		PublishedDate       string            `json:"publishedDate"`
		Description         string            `json:"description"`
		PageCount           int               `json:"pageCount"`
		Language            string            `json:"language"`
		ImageLinks          map[string]string `json:"imageLinks"`
		IndustryIdentifiers []struct {
			Type       string `json:"type"`
			Identifier string `json:"identifier"`
		} `json:"industryIdentifiers"`
		CanonicalVolumeLink string `json:"canonicalVolumeLink"`
	} `json:"volumeInfo"`
}

type googleBooksResponse struct {
	TotalItems int                 `json:"totalItems"`
	Items      []googleBooksVolume `json:"items"`
}

// googleBooksByISBN queries the volumes endpoint with q=isbn:<digits>.
func (c *Client) googleBooksByISBN(ctx context.Context, digits string) (Book, error) {
	resp, err := c.googleBooks(ctx, "isbn:"+digits, 1)
	if err != nil {
		return Book{}, err
	}
	if resp.TotalItems == 0 || len(resp.Items) == 0 {
		return Book{}, nil
	}
	b, ok := fromGoogleBooks(resp.Items[0])
	if !ok {
		return Book{}, nil
	}
	return b, nil
}

// googleBooksSearch returns every candidate for a query from the volumes endpoint.
func (c *Client) googleBooksSearch(ctx context.Context, query string) ([]Book, error) {
	resp, err := c.googleBooks(ctx, query, 8)
	if err != nil {
		return nil, err
	}
	books := make([]Book, 0, len(resp.Items))
	for _, v := range resp.Items {
		if b, ok := fromGoogleBooks(v); ok {
			books = append(books, b)
		}
	}
	return books, nil
}

func (c *Client) googleBooks(ctx context.Context, query string, max int) (googleBooksResponse, error) {
	u := c.gb + "/volumes?q=" + url.QueryEscape(query) + "&maxResults=" + itoa(max) + "&country=US"
	var resp googleBooksResponse
	err := c.getJSON(ctx, u, &resp)
	return resp, err
}

// fromGoogleBooks maps one volume onto a Book. It returns ok=false for volumes
// without a title, so a junk item never becomes a candidate.
func fromGoogleBooks(v googleBooksVolume) (Book, bool) {
	info := v.VolumeInfo
	if info.Title == "" {
		return Book{}, false
	}
	b := Book{
		Title:         info.Title,
		Subtitle:      info.Subtitle,
		Authors:       append([]string(nil), info.Authors...),
		Publisher:     info.Publisher,
		PublishedYear: year(info.PublishedDate),
		PageCount:     info.PageCount,
		Language:      info.Language,
		Description:   strings.TrimSpace(info.Description),
		Source:        "googlebooks",
		SourceURL:     info.CanonicalVolumeLink,
	}
	for _, id := range info.IndustryIdentifiers {
		switch id.Type {
		case "ISBN_13":
			b.ISBN = id.Identifier
		case "ISBN_10":
			if b.ISBN == "" {
				b.ISBN = ISBN10to13(id.Identifier)
			}
		}
	}
	// imageLinks: prefer the largest image present, in the order the API itself
	// documents (extraLarge > large > medium > small > thumbnail).
	for _, size := range []string{"extraLarge", "large", "medium", "small", "thumbnail"} {
		if u, ok := info.ImageLinks[size]; ok && isPublicURL(u) {
			b.CoverURL = u
			break
		}
	}
	return b, true
}

// rankBooks orders candidates so the best edition for a reading note is [0]: an
// exact (case-insensitive) title match wins, then completeness (pages/year/
// authors/cover), then the source's own relevance order preserved by the stable
// sort.
func rankBooks(books []Book, query string) {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	q := norm(query)
	sort.SliceStable(books, func(i, j int) bool {
		ei, ej := norm(books[i].Title) == q, norm(books[j].Title) == q
		if ei != ej {
			return ei
		}
		si, sj := completeness(books[i]), completeness(books[j])
		if si != sj {
			return si > sj
		}
		return false // keep the source's relevance order
	})
}

func completeness(b Book) int {
	n := 0
	if len(b.Authors) > 0 {
		n++
	}
	if b.PublishedYear > 0 {
		n++
	}
	if b.PageCount > 0 {
		n++
	}
	if b.CoverURL != "" {
		n++
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
