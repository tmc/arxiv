package arxiv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Cache manages a local offline cache of arXiv papers.
type Cache struct {
	root     string
	db       *gorm.DB
	paperLRU *LRUCache // In-memory cache for papers
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
	// Use sqlite3 driver (not modernc) for FTS5 support
	// Add connection string parameters to ensure FTS5 is available
	dsn := dbPath + "?_pragma=foreign_keys(1)"
	db, err := gorm.Open(sqlite.Dialector{
		DriverName: "sqlite3",
		DSN:        dsn,
	}, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// LRU cache size: with 15GB+ RAM, we can cache hundreds of thousands of papers
	// Each Paper struct is ~1-2KB, so 500k entries = ~500MB-1GB memory (still <10% of RAM)
	lruSize := 500000
	c := &Cache{
		root:     root,
		db:       db,
		paperLRU: NewLRUCache(lruSize), // Cache 500k most recent papers (~500MB-1GB)
	}
	if err := c.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return c, nil
}

// Close closes the cache database.
func (c *Cache) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Root returns the cache root directory.
func (c *Cache) Root() string {
	return c.root
}

func (c *Cache) initSchema() error {
	// GORM AutoMigrate handles all regular tables (Paper, Citation, SyncState, DownloadQueueItem)
	if err := c.db.AutoMigrate(&Paper{}, &Citation{}, &SyncState{}, &DownloadQueueItem{}); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}

	// FTS5 virtual tables and triggers MUST use raw SQL - GORM doesn't support FTS5
	// We use GORM's Exec() method to stay consistent with GORM patterns
	ftsSchema := `
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
	`
	if err := c.db.Exec(ftsSchema).Error; err != nil {
		// FTS5 not available - log but don't fail (search will fall back to LIKE queries)
		fmt.Printf("Warning: FTS5 not available (%v), full-text search will use fallback methods\n", err)
	}
	return nil
}

// Stats returns cache statistics.
func (c *Cache) Stats(ctx context.Context) (*CacheStats, error) {
	stats := &CacheStats{}

	if err := c.db.WithContext(ctx).Model(&Paper{}).Count(&stats.TotalPapers).Error; err != nil {
		return nil, err
	}

	if err := c.db.WithContext(ctx).Model(&Paper{}).Where("pdf_downloaded = ?", true).Count(&stats.PDFsDownloaded).Error; err != nil {
		return nil, err
	}

	if err := c.db.WithContext(ctx).Model(&Paper{}).Where("src_downloaded = ?", true).Count(&stats.SourcesDownloaded).Error; err != nil {
		return nil, err
	}

	if err := c.db.WithContext(ctx).Model(&DownloadQueueItem{}).Count(&stats.QueuedDownloads).Error; err != nil {
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
