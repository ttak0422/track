package site

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"time"
)

const sitemapNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

type sitemapDocument struct {
	XMLName xml.Name       `xml:"urlset"`
	XMLNS   string         `xml:"xmlns,attr"`
	URLs    []sitemapEntry `xml:"url"`
}

type sitemapEntry struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod,omitempty"`
}

// writeSitemap publishes crawler metadata beside the HTML export. A base URL is required because
// sitemap locators must be absolute; local exports without one deliberately remain free of a sitemap
// and robots file rather than publishing invalid relative locators.
func writeSitemap(outDir, baseURL string, routes []pageRoute) error {
	sitemapPath := filepath.Join(outDir, "sitemap.xml")
	robotsPath := filepath.Join(outDir, "robots.txt")
	if baseURL == "" {
		for _, path := range []string{sitemapPath, robotsPath} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}

	doc := sitemapDocument{XMLNS: sitemapNamespace, URLs: make([]sitemapEntry, 0, len(routes))}
	for _, route := range routes {
		entry := sitemapEntry{Loc: baseURL + routePath(route.route)}
		if route.doc != nil && route.doc.mtime > 0 {
			entry.Lastmod = time.Unix(route.doc.mtime, 0).UTC().Format(time.RFC3339)
		}
		doc.URLs = append(doc.URLs, entry)
	}
	data, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	if err := os.WriteFile(sitemapPath, data, 0o644); err != nil {
		return err
	}
	robots := "User-agent: *\nSitemap: " + baseURL + "/sitemap.xml\n"
	if err := os.WriteFile(robotsPath, []byte(robots), 0o644); err != nil {
		return err
	}
	return nil
}
