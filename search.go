package arxiv

import (
	"context"
	"strings"
	"time"
)

// Search searches papers by title/abstract text using FTS5.
// Note: Uses raw SQL because GORM doesn't support FTS5 MATCH queries.
func (c *Cache) Search(ctx context.Context, query, category string, limit int) ([]Paper, error) {
	if limit <= 0 {
		limit = 20
	}

	sql := `
		SELECT p.id, p.created, p.updated, p.title, p.abstract, p.authors, p.categories,
		       p.comments, p.journal_ref, p.doi, p.license, p.pdf_downloaded, p.src_downloaded
		FROM papers p
		JOIN papers_fts fts ON p.rowid = fts.rowid
		WHERE papers_fts MATCH ?
	`
	args := []any{query}

	if category != "" {
		sql += " AND p.categories LIKE '%' || ? || '%'"
		args = append(args, category)
	}

	sql += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	// Must use raw SQL for FTS5 MATCH queries - GORM doesn't support FTS5
	sqlDB, _ := c.db.DB()
	rows, err := sqlDB.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var papers []Paper
	for rows.Next() {
		var p Paper
		var created, updated string
		var pdfDl, srcDl int

		err := rows.Scan(
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

		papers = append(papers, p)
	}

	return papers, rows.Err()
}

// SearchByAuthor searches papers by author name.
func (c *Cache) SearchByAuthor(ctx context.Context, author string, limit int) ([]Paper, error) {
	if limit <= 0 {
		limit = 100
	}

	var papers []Paper
	err := c.db.WithContext(ctx).
		Where("authors LIKE ?", "%"+author+"%").
		Order("created DESC").
		Limit(limit).
		Find(&papers).Error
	return papers, err
}

// PaperExists checks if a paper exists in the cache.
func (c *Cache) PaperExists(ctx context.Context, id string) bool {
	var count int64
	err := c.db.WithContext(ctx).Model(&Paper{}).Where("id = ?", id).Count(&count).Error
	return err == nil && count > 0
}

// CategoryCount represents a category with its paper count.
type CategoryCount struct {
	Name  string
	Count int
}

// ListCategories returns all categories with their paper counts.
func (c *Cache) ListCategories(ctx context.Context) ([]CategoryCount, error) {
	// Categories are space-separated in the categories column
	// We need to split and count each individual category
	var papers []Paper
	if err := c.db.WithContext(ctx).Select("categories").Where("categories != ?", "").Find(&papers).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, p := range papers {
		for _, cat := range strings.Fields(p.Categories) {
			counts[cat]++
		}
	}

	var result []CategoryCount
	for name, count := range counts {
		result = append(result, CategoryCount{Name: name, Count: count})
	}

	// Sort by count descending
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Count > result[i].Count {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// ListPapers lists papers, optionally filtered by category.
func (c *Cache) ListPapers(ctx context.Context, category string, offset, limit int) ([]Paper, error) {
	if limit <= 0 {
		limit = 100
	}

	query := c.db.WithContext(ctx).Model(&Paper{})
	if category != "" {
		query = query.Where("categories LIKE ?", "%"+category+"%")
	}

	var papers []Paper
	err := query.Order("created DESC").Limit(limit).Offset(offset).Find(&papers).Error
	return papers, err
}

// ListPapersFiltered lists papers with various filter options.
func (c *Cache) ListPapersFiltered(ctx context.Context, category string, srcOnly, all bool, limit int) ([]Paper, error) {
	query := c.db.WithContext(ctx).Model(&Paper{})

	if category != "" {
		query = query.Where("categories LIKE ?", "%"+category+"%")
	}

	if srcOnly {
		query = query.Where("src_downloaded = ?", true)
	} else if !all {
		// Default: show papers with source OR title (exclude metadata-only without useful info)
		query = query.Where("src_downloaded = ? OR title != ?", true, "")
	}

	query = query.Order("id DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	var papers []Paper
	err := query.Find(&papers).Error
	return papers, err
}

// DownloadCategory downloads papers for a category.
func (c *Cache) DownloadCategory(ctx context.Context, category string, limit int, opts *DownloadOptions) error {
	sql := `
		SELECT id FROM papers
		WHERE categories LIKE '%' || ? || '%'
		AND (
			(? = 1 AND pdf_downloaded = 0) OR
			(? = 1 AND src_downloaded = 0)
		)
		ORDER BY created DESC
	`
	args := []any{category}

	dlPDF := 0
	dlSrc := 0
	if opts != nil && opts.DownloadPDF {
		dlPDF = 1
	}
	if opts == nil || opts.DownloadSource {
		dlSrc = 1
	}
	args = append(args, dlPDF, dlSrc)

	if limit > 0 {
		sql += " LIMIT ?"
		args = append(args, limit)
	}

	sqlDB, _ := c.db.DB()
	rows, err := sqlDB.QueryContext(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i, id := range ids {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if opts != nil && opts.Progress != nil {
			opts.Progress(id, i+1, len(ids))
		}

		if err := c.DownloadPaper(ctx, id, opts); err != nil {
			// Log and continue
			continue
		}

		// Rate limit
		time.Sleep(3 * time.Second)
	}

	return nil
}
