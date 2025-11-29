package arxiv

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const apiBaseURL = "https://export.arxiv.org/api/query"

// Fetch retrieves a paper's metadata directly from arXiv API and stores it.
// This is for fetching individual papers without a full OAI-PMH sync.
func (c *Cache) Fetch(ctx context.Context, id string) (*Paper, error) {
	// Check if already in cache
	paper, err := c.GetPaper(ctx, id)
	if err == nil {
		return paper, nil
	}

	// Fetch from arXiv API
	paper, err = fetchPaperMetadata(ctx, id)
	if err != nil {
		return nil, err
	}

	// Store in database
	_, err = c.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO papers
		(id, created, updated, title, abstract, authors, categories, comments, journal_ref, doi, license, metadata_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		paper.ID,
		paper.Created.Format("2006-01-02"),
		paper.Updated.Format("2006-01-02"),
		paper.Title,
		paper.Abstract,
		paper.Authors,
		paper.Categories,
		paper.Comments,
		paper.JournalRef,
		paper.DOI,
		paper.License,
		time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("store paper: %w", err)
	}

	return paper, nil
}

// FetchMetadataOnly fetches just the metadata (title, authors, abstract) without downloading source.
// This is cheap and fast - good for populating citation titles.
func (c *Cache) FetchMetadataOnly(ctx context.Context, id string) (*Paper, error) {
	return c.Fetch(ctx, id) // Fetch already does metadata-only
}

// FetchBatch fetches metadata for multiple papers in a single API call.
// arXiv API supports up to ~100 IDs per request.
func (c *Cache) FetchBatch(ctx context.Context, ids []string) ([]*Paper, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Filter out papers we already have
	var missing []string
	var existing []*Paper
	for _, id := range ids {
		if paper, err := c.GetPaper(ctx, id); err == nil {
			existing = append(existing, paper)
		} else {
			missing = append(missing, id)
		}
	}

	if len(missing) == 0 {
		return existing, nil
	}

	// Batch fetch from arXiv API (comma-separated IDs)
	url := fmt.Sprintf("%s?id_list=%s&max_results=%d", apiBaseURL, strings.Join(missing, ","), len(missing))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return existing, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return existing, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return existing, fmt.Errorf("http %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return existing, err
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return existing, fmt.Errorf("parse xml: %w", err)
	}

	// Store each paper
	for _, entry := range feed.Entries {
		paper := parseAtomEntry(entry)
		if paper.ID == "" {
			continue
		}

		_, err = c.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO papers
			(id, created, updated, title, abstract, authors, categories, comments, journal_ref, doi, license, metadata_updated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			paper.ID,
			paper.Created.Format("2006-01-02"),
			paper.Updated.Format("2006-01-02"),
			paper.Title,
			paper.Abstract,
			paper.Authors,
			paper.Categories,
			paper.Comments,
			paper.JournalRef,
			paper.DOI,
			paper.License,
			time.Now().Format(time.RFC3339),
		)
		if err == nil {
			existing = append(existing, paper)
		}
	}

	return existing, nil
}

// PrefetchReferenceTitles fetches metadata for all uncached references of a paper.
// This populates titles without downloading full sources.
func (c *Cache) PrefetchReferenceTitles(ctx context.Context, paperID string) error {
	refs, err := c.References(ctx, paperID)
	if err != nil {
		return err
	}

	var uncached []string
	for _, r := range refs {
		if !r.HasTitle {
			uncached = append(uncached, r.ID)
		}
	}

	if len(uncached) == 0 {
		return nil
	}

	// Batch fetch in chunks of 50 (arXiv limit is ~100)
	for i := 0; i < len(uncached); i += 50 {
		end := i + 50
		if end > len(uncached) {
			end = len(uncached)
		}
		chunk := uncached[i:end]

		if _, err := c.FetchBatch(ctx, chunk); err != nil {
			// Non-fatal, continue with next chunk
			continue
		}

		// Rate limit between batches
		if end < len(uncached) {
			time.Sleep(1 * time.Second)
		}
	}

	return nil
}

// FetchAndDownload fetches metadata and downloads source/PDF for a paper.
func (c *Cache) FetchAndDownload(ctx context.Context, id string, opts *DownloadOptions) (*Paper, error) {
	paper, err := c.Fetch(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := c.DownloadPaper(ctx, id, opts); err != nil {
		return paper, fmt.Errorf("download: %w", err)
	}

	// Refresh to get updated paths
	return c.GetPaper(ctx, id)
}

func fetchPaperMetadata(ctx context.Context, id string) (*Paper, error) {
	url := fmt.Sprintf("%s?id_list=%s", apiBaseURL, id)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse xml: %w", err)
	}

	if len(feed.Entries) == 0 {
		return nil, fmt.Errorf("paper not found: %s", id)
	}

	entry := feed.Entries[0]

	// Extract ID from the URL (e.g., http://arxiv.org/abs/2301.00001v1 -> 2301.00001)
	paperID := id
	if idx := strings.LastIndex(entry.ID, "/abs/"); idx >= 0 {
		paperID = entry.ID[idx+5:]
		// Remove version suffix
		if vIdx := strings.LastIndex(paperID, "v"); vIdx > 0 {
			paperID = paperID[:vIdx]
		}
	}

	var authors []string
	for _, a := range entry.Authors {
		authors = append(authors, a.Name)
	}

	var categories []string
	for _, c := range entry.Categories {
		categories = append(categories, c.Term)
	}

	paper := &Paper{
		ID:         paperID,
		Title:      strings.TrimSpace(entry.Title),
		Abstract:   strings.TrimSpace(entry.Summary),
		Authors:    strings.Join(authors, ", "),
		Categories: strings.Join(categories, " "),
		Comments:   entry.Comment,
		JournalRef: entry.JournalRef,
		DOI:        entry.DOI,
	}

	paper.Created, _ = time.Parse(time.RFC3339, entry.Published)
	paper.Updated, _ = time.Parse(time.RFC3339, entry.Updated)

	return paper, nil
}

// Atom feed structures for arXiv API

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Summary    string         `xml:"summary"`
	Authors    []atomAuthor   `xml:"author"`
	Categories []atomCategory `xml:"category"`
	Published  string         `xml:"published"`
	Updated    string         `xml:"updated"`
	Comment    string         `xml:"comment"`
	JournalRef string         `xml:"journal_ref"`
	DOI        string         `xml:"doi"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

// parseAtomEntry converts an atom entry to a Paper.
func parseAtomEntry(entry atomEntry) *Paper {
	// Extract ID from the URL (e.g., http://arxiv.org/abs/2301.00001v1 -> 2301.00001)
	paperID := ""
	if idx := strings.LastIndex(entry.ID, "/abs/"); idx >= 0 {
		paperID = entry.ID[idx+5:]
		// Remove version suffix
		if vIdx := strings.LastIndex(paperID, "v"); vIdx > 0 {
			paperID = paperID[:vIdx]
		}
	}

	var authors []string
	for _, a := range entry.Authors {
		authors = append(authors, a.Name)
	}

	var categories []string
	for _, c := range entry.Categories {
		categories = append(categories, c.Term)
	}

	paper := &Paper{
		ID:         paperID,
		Title:      strings.TrimSpace(entry.Title),
		Abstract:   strings.TrimSpace(entry.Summary),
		Authors:    strings.Join(authors, ", "),
		Categories: strings.Join(categories, " "),
		Comments:   entry.Comment,
		JournalRef: entry.JournalRef,
		DOI:        entry.DOI,
	}

	paper.Created, _ = time.Parse(time.RFC3339, entry.Published)
	paper.Updated, _ = time.Parse(time.RFC3339, entry.Updated)

	return paper
}
