package arxiv

import (
	"context"
	"time"
)

// UpdateCitations extracts references from a paper's source and stores citation edges.
// This should be called after downloading source files.
func (c *Cache) UpdateCitations(ctx context.Context, paperID, srcPath string) error {
	if srcPath == "" {
		return nil
	}

	refs := ExtractReferences(srcPath)
	if len(refs) == 0 {
		return nil
	}

	// Delete existing citations from this paper (in case of re-index)
	_, err := c.db.ExecContext(ctx, "DELETE FROM citations WHERE from_id = ?", paperID)
	if err != nil {
		return err
	}

	// Insert new citations
	for _, refID := range refs {
		if refID == paperID {
			continue // Skip self-citations
		}
		_, err := c.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO citations (from_id, to_id) VALUES (?, ?)",
			paperID, refID)
		if err != nil {
			return err
		}
	}

	return nil
}

// CitedByCount returns the number of cached papers that cite this paper.
func (c *Cache) CitedByCount(ctx context.Context, paperID string) (int, error) {
	var count int
	err := c.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM citations WHERE to_id = ?", paperID).Scan(&count)
	return count, err
}

// CitedBy returns papers that cite this paper (only cached papers with metadata).
type CitingPaper struct {
	ID    string
	Title string
}

func (c *Cache) CitedBy(ctx context.Context, paperID string, limit int) ([]CitingPaper, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := c.db.QueryContext(ctx, `
		SELECT p.id, p.title
		FROM citations c
		JOIN papers p ON c.from_id = p.id
		WHERE c.to_id = ?
		ORDER BY p.created DESC
		LIMIT ?
	`, paperID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var papers []CitingPaper
	for rows.Next() {
		var p CitingPaper
		if err := rows.Scan(&p.ID, &p.Title); err != nil {
			return nil, err
		}
		papers = append(papers, p)
	}

	return papers, rows.Err()
}

// Reference represents a paper that is cited.
type Reference struct {
	ID        string
	Title     string
	HasTitle  bool // True if we have metadata (title available)
	HasSource bool // True if we have source downloaded
}

func (c *Cache) References(ctx context.Context, paperID string) ([]Reference, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT c.to_id, COALESCE(p.title, ''),
		       CASE WHEN p.id IS NOT NULL AND p.title != '' THEN 1 ELSE 0 END,
		       CASE WHEN p.src_downloaded = 1 THEN 1 ELSE 0 END
		FROM citations c
		LEFT JOIN papers p ON c.to_id = p.id
		WHERE c.from_id = ?
		ORDER BY c.to_id DESC
	`, paperID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Reference
	for rows.Next() {
		var r Reference
		var hasTitle, hasSource int
		if err := rows.Scan(&r.ID, &r.Title, &hasTitle, &hasSource); err != nil {
			return nil, err
		}
		r.HasTitle = hasTitle == 1
		r.HasSource = hasSource == 1
		refs = append(refs, r)
	}

	return refs, rows.Err()
}

// UncachedReferenceCount returns the number of references without metadata.
func (c *Cache) UncachedReferenceCount(ctx context.Context, paperID string) (int, error) {
	var count int
	err := c.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM citations c
		LEFT JOIN papers p ON c.to_id = p.id
		WHERE c.from_id = ? AND (p.id IS NULL OR p.title = '')
	`, paperID).Scan(&count)
	return count, err
}

// RebuildAllCitations rebuilds the citations table by re-extracting references from all papers.
func (c *Cache) RebuildAllCitations(ctx context.Context) error {
	// Clear existing citations
	_, err := c.db.ExecContext(ctx, "DELETE FROM citations")
	if err != nil {
		return err
	}

	// Get all papers with source downloaded
	rows, err := c.db.QueryContext(ctx, `
		SELECT id, src_path FROM papers WHERE src_downloaded = 1 AND src_path IS NOT NULL
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type paper struct {
		id      string
		srcPath string
	}
	var papers []paper
	for rows.Next() {
		var p paper
		if err := rows.Scan(&p.id, &p.srcPath); err != nil {
			return err
		}
		papers = append(papers, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Extract and store citations for each paper
	for _, p := range papers {
		if err := c.UpdateCitations(ctx, p.id, p.srcPath); err != nil {
			return err
		}
	}

	return nil
}

// GetPaperWithCitations returns a paper along with its citation count.
func (c *Cache) GetPaperWithCitations(ctx context.Context, id string) (*Paper, int, error) {
	paper, err := c.GetPaper(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	count, err := c.CitedByCount(ctx, id)
	if err != nil {
		return paper, 0, err
	}

	return paper, count, nil
}

// scanPaper is a helper to scan paper rows (used internally).
func scanPaperRow(row interface {
	Scan(dest ...any) error
}) (*Paper, error) {
	var p Paper
	var created, updated string
	var pdfDl, srcDl int

	err := row.Scan(
		&p.ID, &created, &updated, &p.Title, &p.Abstract, &p.Authors,
		&p.Categories, &p.Comments, &p.JournalRef, &p.DOI, &p.License,
		&pdfDl, &srcDl,
	)
	if err != nil {
		return nil, err
	}

	p.Created, _ = time.Parse("2006-01-02", created)
	p.Updated, _ = time.Parse("2006-01-02", updated)
	p.PDFDownloaded = pdfDl == 1
	p.SourceDownloaded = srcDl == 1

	return &p, nil
}

// GraphNode represents a node in the citation graph.
type GraphNode struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Authors   string `json:"authors"`
	Year      int    `json:"year"`
	Citations int    `json:"citations"` // How many papers cite this one
	Cached    bool   `json:"cached"`
}

// GraphEdge represents an edge in the citation graph.
type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// CitationGraph represents a citation graph for visualization.
type CitationGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// PaperListItem represents a paper in the sidebar list.
type PaperListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Authors   string `json:"authors"`
	Year      int    `json:"year"`
	Citations int    `json:"citations"`
	Cached    bool   `json:"cached"`
	IsRef     bool   `json:"isRef"`    // True if this paper is a reference
	IsCiting  bool   `json:"isCiting"` // True if this paper cites the main paper
}

// GetCitationGraph returns a citation graph centered on the given paper.
// Includes: the paper itself, its references, papers that cite it,
// and edges between references if they cite each other.
func (c *Cache) GetCitationGraph(ctx context.Context, paperID string) (*CitationGraph, error) {
	graph := &CitationGraph{
		Nodes: []GraphNode{},
		Edges: []GraphEdge{},
	}

	nodeSet := make(map[string]GraphNode)
	edgeSet := make(map[string]bool)

	addNode := func(id string, node GraphNode) {
		if _, exists := nodeSet[id]; !exists {
			nodeSet[id] = node
		}
	}

	addEdge := func(from, to string) {
		key := from + "->" + to
		if !edgeSet[key] {
			edgeSet[key] = true
			graph.Edges = append(graph.Edges, GraphEdge{Source: from, Target: to})
		}
	}

	// Helper to extract year from arXiv ID (YYMM.NNNNN -> 20YY)
	yearFromID := func(id string) int {
		if len(id) >= 2 {
			yy := id[:2]
			if yy >= "00" && yy <= "99" {
				year := 2000 + int(yy[0]-'0')*10 + int(yy[1]-'0')
				if year > 2090 {
					year -= 100 // 91-99 -> 1991-1999
				}
				return year
			}
		}
		return 0
	}

	// Get the central paper
	paper, err := c.GetPaper(ctx, paperID)
	if err != nil {
		return nil, err
	}
	citedByCount, _ := c.CitedByCount(ctx, paperID)
	addNode(paper.ID, GraphNode{
		ID:        paper.ID,
		Title:     paper.Title,
		Authors:   paper.Authors,
		Year:      paper.Created.Year(),
		Citations: citedByCount,
		Cached:    true,
	})

	// Get references (papers this one cites)
	refs, err := c.References(ctx, paperID)
	if err != nil {
		return nil, err
	}
	refIDs := make(map[string]bool)
	for _, ref := range refs {
		title := ref.Title
		if title == "" {
			title = ref.ID
		}
		year := yearFromID(ref.ID)
		citations, _ := c.CitedByCount(ctx, ref.ID)
		// Get authors if we have metadata
		var authors string
		if ref.HasTitle {
			if p, err := c.GetPaper(ctx, ref.ID); err == nil {
				authors = p.Authors
				year = p.Created.Year()
			}
		}
		addNode(ref.ID, GraphNode{
			ID:        ref.ID,
			Title:     title,
			Authors:   authors,
			Year:      year,
			Citations: citations,
			Cached:    ref.HasTitle,
		})
		addEdge(paperID, ref.ID)
		refIDs[ref.ID] = true
	}

	// Get citing papers (papers that cite this one)
	citedBy, err := c.CitedBy(ctx, paperID, 100)
	if err != nil {
		return nil, err
	}
	for _, citing := range citedBy {
		citations, _ := c.CitedByCount(ctx, citing.ID)
		var authors string
		var year int
		if p, err := c.GetPaper(ctx, citing.ID); err == nil {
			authors = p.Authors
			year = p.Created.Year()
		}
		addNode(citing.ID, GraphNode{
			ID:        citing.ID,
			Title:     citing.Title,
			Authors:   authors,
			Year:      year,
			Citations: citations,
			Cached:    true,
		})
		addEdge(citing.ID, paperID)
	}

	// Find edges between references (if they cite each other)
	if len(refIDs) > 0 {
		rows, err := c.db.QueryContext(ctx, `
			SELECT from_id, to_id FROM citations
			WHERE from_id IN (SELECT to_id FROM citations WHERE from_id = ?)
			  AND to_id IN (SELECT to_id FROM citations WHERE from_id = ?)
		`, paperID, paperID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var from, to string
				if err := rows.Scan(&from, &to); err == nil {
					addEdge(from, to)
				}
			}
		}
	}

	// Convert node map to slice
	for _, node := range nodeSet {
		graph.Nodes = append(graph.Nodes, node)
	}

	return graph, nil
}

// GetPaperList returns a combined list of references and citing papers for the sidebar.
func (c *Cache) GetPaperList(ctx context.Context, paperID string) ([]PaperListItem, error) {
	var items []PaperListItem

	// Helper to extract year from arXiv ID
	yearFromID := func(id string) int {
		if len(id) >= 2 {
			yy := id[:2]
			if yy >= "00" && yy <= "99" {
				year := 2000 + int(yy[0]-'0')*10 + int(yy[1]-'0')
				if year > 2090 {
					year -= 100
				}
				return year
			}
		}
		return 0
	}

	// Get references
	refs, err := c.References(ctx, paperID)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		title := ref.Title
		if title == "" {
			title = ref.ID
		}
		year := yearFromID(ref.ID)
		citations, _ := c.CitedByCount(ctx, ref.ID)
		var authors string
		if ref.HasTitle {
			if p, err := c.GetPaper(ctx, ref.ID); err == nil {
				authors = p.Authors
				year = p.Created.Year()
			}
		}
		items = append(items, PaperListItem{
			ID:        ref.ID,
			Title:     title,
			Authors:   authors,
			Year:      year,
			Citations: citations,
			Cached:    ref.HasTitle,
			IsRef:     true,
		})
	}

	// Get citing papers
	citedBy, err := c.CitedBy(ctx, paperID, 100)
	if err != nil {
		return nil, err
	}
	for _, citing := range citedBy {
		citations, _ := c.CitedByCount(ctx, citing.ID)
		var authors string
		var year int
		if p, err := c.GetPaper(ctx, citing.ID); err == nil {
			authors = p.Authors
			year = p.Created.Year()
		}
		items = append(items, PaperListItem{
			ID:        citing.ID,
			Title:     citing.Title,
			Authors:   authors,
			Year:      year,
			Citations: citations,
			Cached:    true,
			IsCiting:  true,
		})
	}

	return items, nil
}
