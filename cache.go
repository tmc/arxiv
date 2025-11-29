package arxiv

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Cache manages a local offline cache of arXiv papers.
type Cache struct {
	root string
	db   *sql.DB
}

// Open opens or creates an arXiv cache at the given root directory.
func Open(root string) (*Cache, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	// Create subdirectories
	for _, dir := range []string{"pdf", "src", "meta"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			return nil, fmt.Errorf("create %s dir: %w", dir, err)
		}
	}

	dbPath := filepath.Join(root, "index.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	c := &Cache{root: root, db: db}
	if err := c.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return c, nil
}

// Close closes the cache database.
func (c *Cache) Close() error {
	return c.db.Close()
}

// Root returns the cache root directory.
func (c *Cache) Root() string {
	return c.root
}

func (c *Cache) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS papers (
		id TEXT PRIMARY KEY,
		created TEXT,
		updated TEXT,
		title TEXT,
		abstract TEXT,
		authors TEXT,
		categories TEXT,
		comments TEXT,
		journal_ref TEXT,
		doi TEXT,
		license TEXT,
		pdf_path TEXT,
		src_path TEXT,
		pdf_downloaded INTEGER DEFAULT 0,
		src_downloaded INTEGER DEFAULT 0,
		metadata_updated TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_papers_created ON papers(created);
	CREATE INDEX IF NOT EXISTS idx_papers_updated ON papers(updated);
	CREATE INDEX IF NOT EXISTS idx_papers_categories ON papers(categories);

	CREATE TABLE IF NOT EXISTS sync_state (
		key TEXT PRIMARY KEY,
		value TEXT
	);

	CREATE TABLE IF NOT EXISTS download_queue (
		paper_id TEXT PRIMARY KEY,
		type TEXT,
		priority INTEGER DEFAULT 0,
		added TEXT,
		attempts INTEGER DEFAULT 0,
		last_error TEXT
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS papers_fts USING fts5(
		title,
		abstract,
		content='papers',
		content_rowid='rowid'
	);

	CREATE TRIGGER IF NOT EXISTS papers_ai AFTER INSERT ON papers BEGIN
		INSERT INTO papers_fts(rowid, title, abstract)
		VALUES (NEW.rowid, NEW.title, NEW.abstract);
	END;

	CREATE TRIGGER IF NOT EXISTS papers_ad AFTER DELETE ON papers BEGIN
		INSERT INTO papers_fts(papers_fts, rowid, title, abstract)
		VALUES ('delete', OLD.rowid, OLD.title, OLD.abstract);
	END;

	CREATE TRIGGER IF NOT EXISTS papers_au AFTER UPDATE ON papers BEGIN
		INSERT INTO papers_fts(papers_fts, rowid, title, abstract)
		VALUES ('delete', OLD.rowid, OLD.title, OLD.abstract);
		INSERT INTO papers_fts(rowid, title, abstract)
		VALUES (NEW.rowid, NEW.title, NEW.abstract);
	END;

	CREATE TABLE IF NOT EXISTS citations (
		from_id TEXT NOT NULL,
		to_id TEXT NOT NULL,
		PRIMARY KEY (from_id, to_id)
	);

	CREATE INDEX IF NOT EXISTS idx_citations_to_id ON citations(to_id);
	`
	_, err := c.db.Exec(schema)
	return err
}

// Stats returns cache statistics.
func (c *Cache) Stats(ctx context.Context) (*CacheStats, error) {
	stats := &CacheStats{}

	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM papers").Scan(&stats.TotalPapers)
	if err != nil {
		return nil, err
	}

	err = c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM papers WHERE pdf_downloaded = 1").Scan(&stats.PDFsDownloaded)
	if err != nil {
		return nil, err
	}

	err = c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM papers WHERE src_downloaded = 1").Scan(&stats.SourcesDownloaded)
	if err != nil {
		return nil, err
	}

	err = c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM download_queue").Scan(&stats.QueuedDownloads)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// CacheStats contains statistics about the cache.
type CacheStats struct {
	TotalPapers       int64
	PDFsDownloaded    int64
	SourcesDownloaded int64
	QueuedDownloads   int64
}
