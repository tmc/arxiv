package arxiv

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"time"
)

// SitemapURL represents a single URL entry in the sitemap.
type SitemapURL struct {
	Loc        string     // Absolute URL, e.g. https://arxiv.gg/paper/1234
	LastMod    *time.Time // Optional last modification time
	ChangeFreq string     // Optional change frequency hint (e.g. daily, weekly)
	Priority   float32    // Optional priority hint between 0.0 and 1.0
}

// SitemapURLSet represents a collection of URLs for a sitemap.
type SitemapURLSet []SitemapURL

// SiteBaseURL returns the base URL for the site, using the SITE_URL
// environment variable when available, falling back to https://arxiv.gg.
func SiteBaseURL() string {
	if v := os.Getenv("SITE_URL"); v != "" {
		return v
	}
	return "https://arxiv.gg"
}

// BuildSitemapXML generates a sitemap XML document for the given URLs.
// It conforms to the sitemaps.org protocol.
func BuildSitemapXML(urls SitemapURLSet) ([]byte, error) {
	type xmlURL struct {
		Loc        string  `xml:"loc"`
		LastMod    *string `xml:"lastmod,omitempty"`
		ChangeFreq string  `xml:"changefreq,omitempty"`
		Priority   *string `xml:"priority,omitempty"`
	}

	type urlSet struct {
		XMLName xml.Name `xml:"urlset"`
		Xmlns   string   `xml:"xmlns,attr"`
		URLs    []xmlURL `xml:"url"`
	}

	const ns = "http://www.sitemaps.org/schemas/sitemap/0.9"

	var out urlSet
	out.Xmlns = ns
	out.URLs = make([]xmlURL, 0, len(urls))

	// Deduplicate by location to avoid multiple entries for the same URL
	seen := make(map[string]bool, len(urls))

	for _, u := range urls {
		if seen[u.Loc] {
			continue
		}
		seen[u.Loc] = true

		xu := xmlURL{
			Loc:        u.Loc,
			ChangeFreq: u.ChangeFreq,
		}
		if u.LastMod != nil {
			s := u.LastMod.UTC().Format(time.RFC3339)
			xu.LastMod = &s
		}
		if u.Priority > 0 {
			s := fmt.Sprintf("%.1f", u.Priority)
			xu.Priority = &s
		}
		out.URLs = append(out.URLs, xu)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}


