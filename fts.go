package arxiv

import "context"

// RebuildFTSIndex rebuilds the FTS5 index from all papers.
// Use this after migrating an existing database to FTS5.
// Note: Uses raw SQL because GORM doesn't support FTS5 virtual tables.
func (c *Cache) RebuildFTSIndex(ctx context.Context) error {
	// Delete existing FTS data
	if err := c.db.WithContext(ctx).Exec("DELETE FROM papers_fts").Error; err != nil {
		return err
	}

	// Rebuild from papers table
	return c.db.WithContext(ctx).Exec(`
		INSERT INTO papers_fts(rowid, title, abstract)
		SELECT rowid, title, abstract FROM papers
	`).Error
}
