// Package arxiv provides tools for managing a complete offline cache of arXiv papers.
//
// This package implements:
//   - OAI-PMH client for harvesting paper metadata
//   - PDF and TeX source download functionality
//   - Local SQLite-based indexing for fast search
//   - Incremental sync to keep the cache up to date
//
// arXiv contains ~2.4 million papers (as of 2024). A full cache requires:
//   - Metadata: ~10GB
//   - PDFs: ~10TB
//   - TeX sources: ~2TB
//
// The cache supports incremental updates via OAI-PMH resumption tokens
// and tracks download state to resume interrupted syncs.
//
// Basic usage:
//
//	cache, err := arxiv.Open("/path/to/cache")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer cache.Close()
//
//	// Sync metadata from arXiv
//	if err := cache.SyncMetadata(ctx); err != nil {
//		log.Fatal(err)
//	}
//
//	// Download papers for a specific category
//	if err := cache.DownloadCategory(ctx, "cs.AI"); err != nil {
//		log.Fatal(err)
//	}
package arxiv
