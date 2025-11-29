package arxiv

import (
	"context"
	"fmt"
	"log"
	"time"
)

// SyncOptions configures metadata synchronization.
type SyncOptions struct {
	// Set filters to a specific arXiv set (e.g., "cs" for computer science)
	Set string

	// From is the start date for incremental sync
	From time.Time

	// Until is the end date for sync
	Until time.Time

	// Progress callback for reporting sync progress
	Progress func(fetched, total int)

	// BatchSize is how often to commit (default 1000)
	BatchSize int
}

// SyncMetadata synchronizes paper metadata from arXiv via OAI-PMH.
func (c *Cache) SyncMetadata(ctx context.Context, opts *SyncOptions) error {
	if opts == nil {
		opts = &SyncOptions{}
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = 1000
	}

	client := NewOAIClient()

	// Check for existing resumption token
	var resumptionToken string
	row := c.db.QueryRowContext(ctx, "SELECT value FROM sync_state WHERE key = 'resumption_token'")
	row.Scan(&resumptionToken)

	// If no resumption token, check last sync date
	if resumptionToken == "" && opts.From.IsZero() {
		var lastSync string
		row := c.db.QueryRowContext(ctx, "SELECT value FROM sync_state WHERE key = 'last_sync'")
		if row.Scan(&lastSync) == nil && lastSync != "" {
			opts.From, _ = time.Parse("2006-01-02", lastSync)
		}
	}

	totalFetched := 0
	batch := make([]Paper, 0, opts.BatchSize)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.ListRecords(ctx, opts.Set, opts.From, opts.Until, resumptionToken)
		if err != nil {
			// Save state for resume on rate limit
			if resumptionToken != "" {
				c.saveResumptionToken(ctx, resumptionToken)
			}
			return fmt.Errorf("list records: %w", err)
		}

		batch = append(batch, resp.Papers...)
		totalFetched += len(resp.Papers)

		if opts.Progress != nil {
			opts.Progress(totalFetched, resp.CompleteListSize)
		}

		// Commit batch if full
		if len(batch) >= opts.BatchSize {
			if err := c.insertPapers(ctx, batch); err != nil {
				return fmt.Errorf("insert papers: %w", err)
			}
			batch = batch[:0]
		}

		// Save resumption token
		if resp.ResumptionToken != "" {
			c.saveResumptionToken(ctx, resp.ResumptionToken)
			resumptionToken = resp.ResumptionToken

			// arXiv rate limit: wait between requests
			time.Sleep(3 * time.Second)
		} else {
			// No more records
			break
		}
	}

	// Insert remaining papers
	if len(batch) > 0 {
		if err := c.insertPapers(ctx, batch); err != nil {
			return fmt.Errorf("insert papers: %w", err)
		}
	}

	// Clear resumption token and save last sync date
	c.db.ExecContext(ctx, "DELETE FROM sync_state WHERE key = 'resumption_token'")
	c.db.ExecContext(ctx, "INSERT OR REPLACE INTO sync_state (key, value) VALUES ('last_sync', ?)",
		time.Now().Format("2006-01-02"))

	log.Printf("sync complete: %d papers", totalFetched)
	return nil
}

func (c *Cache) saveResumptionToken(ctx context.Context, token string) {
	c.db.ExecContext(ctx, "INSERT OR REPLACE INTO sync_state (key, value) VALUES ('resumption_token', ?)", token)
}

func (c *Cache) insertPapers(ctx context.Context, papers []Paper) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO papers
		(id, created, updated, title, abstract, authors, categories, comments, journal_ref, doi, license, metadata_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Format(time.RFC3339)
	for _, p := range papers {
		_, err := stmt.ExecContext(ctx,
			p.ID,
			p.Created.Format("2006-01-02"),
			p.Updated.Format("2006-01-02"),
			p.Title,
			p.Abstract,
			p.Authors,
			p.Categories,
			p.Comments,
			p.JournalRef,
			p.DOI,
			p.License,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
