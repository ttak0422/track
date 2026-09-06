package book

import (
	"context"
	"fmt"
	"net/url"
)

// openLibraryByISBN resolves an edition from Open Library's bibkeys endpoint
// (https://openlibrary.org/dev/docs/api/books): the data format carries title,
// authors, page count, publish date, publisher, and cover URLs directly.
func (c *Client) openLibraryByISBN(ctx context.Context, digits string) (Book, error) {
	u := c.ol + "/api/books?bibkeys=ISBN:" + digits + "&format=json&jscmd=data"
	var resp map[string]openLibraryData
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return Book{}, err
	}
	data, ok := resp["ISBN:"+digits]
	if !ok || data.Title == "" {
		return Book{}, nil
	}
	b := fromOpenLibrary(data)
	if b.ISBN == "" {
		b.ISBN = digits
	}
	b.Source = "openlibrary"
	return b, nil
}

// openLibrarySearch resolves a title query from Open Library's search index
// (https://openlibrary.org/dev/docs/api/search). The search response aggregates
// editions, so page count is the median and the cover is the first edition's;
// the ISBN is the first 13-digit one in the hit.
func (c *Client) openLibrarySearch(ctx context.Context, query string) ([]Book, error) {
	u := c.ol + "/search.json?q=" + url.QueryEscape(query) +
		"&fields=title,author_name,first_publish_year,number_of_pages_median,isbn,cover_i,edition_key&limit=8"
	var resp openLibrarySearchResponse
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	books := make([]Book, 0, len(resp.Docs))
	for _, d := range resp.Docs {
		if d.Title == "" {
			continue
		}
		b := Book{
			Title:         d.Title,
			Authors:       append([]string(nil), d.AuthorName...),
			PublishedYear: d.FirstPublishYear,
			PageCount:     d.NumberOfPagesMedian,
			Source:        "openlibrary",
		}
		if d.CoverI > 0 {
			b.CoverURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", d.CoverI)
		}
		for _, isbn := range d.ISBN {
			if n := isbnRe.ReplaceAllString(isbn, ""); len(n) == 13 {
				b.ISBN = n
				break
			}
		}
		books = append(books, b)
	}
	return books, nil
}

// openLibraryData mirrors the `jscmd=data` record of the bibkeys endpoint.
type openLibraryData struct {
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	NumberPages int    `json:"number_of_pages"`
	PublishDate string `json:"publish_date"`
	URL         string `json:"url"`
	Publishers  []struct {
		Name string `json:"name"`
	} `json:"publishers"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
	Cover       map[string]string `json:"cover"`
	Identifiers struct {
		ISBN13 []string `json:"isbn_13"`
		ISBN10 []string `json:"isbn_10"`
	} `json:"identifiers"`
}

func fromOpenLibrary(d openLibraryData) Book {
	b := Book{
		Title:         d.Title,
		Subtitle:      d.Subtitle,
		PageCount:     d.NumberPages,
		PublishedYear: year(d.PublishDate),
		SourceURL:     d.URL,
	}
	for _, a := range d.Authors {
		b.Authors = append(b.Authors, a.Name)
	}
	for _, p := range d.Publishers {
		if p.Name != "" {
			b.Publisher = p.Name
			break
		}
	}
	for _, size := range []string{"large", "medium", "small"} {
		if u, ok := d.Cover[size]; ok && isPublicURL(u) {
			b.CoverURL = u
			break
		}
	}
	if len(d.Identifiers.ISBN13) > 0 {
		b.ISBN = d.Identifiers.ISBN13[0]
	} else if len(d.Identifiers.ISBN10) > 0 {
		b.ISBN = ISBN10to13(d.Identifiers.ISBN10[0])
	}
	return b
}

// openLibrarySearchResponse mirrors the search.json response envelope.
type openLibrarySearchResponse struct {
	NumFound int                    `json:"numFound"`
	Docs     []openLibrarySearchDoc `json:"docs"`
}

type openLibrarySearchDoc struct {
	Title               string   `json:"title"`
	AuthorName          []string `json:"author_name"`
	FirstPublishYear    int      `json:"first_publish_year"`
	NumberOfPagesMedian int      `json:"number_of_pages_median"`
	CoverI              int      `json:"cover_i"`
	ISBN                []string `json:"isbn"`
	EditionKey          []string `json:"edition_key"`
}
