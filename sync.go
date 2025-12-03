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
	var state SyncState
	c.db.WithContext(ctx).Where("key = ?", "resumption_token").First(&state)
	resumptionToken := state.Value

	// If no resumption token, check last sync date
	if resumptionToken == "" && opts.From.IsZero() {
		var lastSyncState SyncState
		if c.db.WithContext(ctx).Where("key = ?", "last_sync").First(&lastSyncState).Error == nil && lastSyncState.Value != "" {
			opts.From, _ = time.Parse("2006-01-02", lastSyncState.Value)
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
	c.db.WithContext(ctx).Where("key = ?", "resumption_token").Delete(&SyncState{})
	c.db.WithContext(ctx).Save(&SyncState{
		Key:   "last_sync",
		Value: time.Now().Format("2006-01-02"),
	})

	log.Printf("sync complete: %d papers", totalFetched)
	return nil
}

func (c *Cache) saveResumptionToken(ctx context.Context, token string) {
	c.db.WithContext(ctx).Save(&SyncState{
		Key:   "resumption_token",
		Value: token,
	})
}

func (c *Cache) insertPapers(ctx context.Context, papers []Paper) error {
	now := time.Now()
	for i := range papers {
		papers[i].MetadataUpdated = &now
	}
	return c.db.WithContext(ctx).Save(papers).Error
}
