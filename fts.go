package arxiv

import "context"

// RebuildFTSIndex rebuilds the FTS5 index from all papers.
// Use this after migrating an existing database to FTS5.
func (c *Cache) RebuildFTSIndex(ctx context.Context) error {
	// Delete existing FTS data
	_, err := c.db.ExecContext(ctx, "DELETE FROM papers_fts")
	if err != nil {
		return err
	}

	// Rebuild from papers table
	_, err = c.db.ExecContext(ctx, `
		INSERT INTO papers_fts(rowid, title, abstract)
		SELECT rowid, title, abstract FROM papers
	`)
	return err
}
