/*
arxiv is an offline arXiv paper cache manager.

It provides tools to fetch, cache, search, and browse arXiv papers locally,
including TeX source files, PDFs, and extracted citation graphs.

# Usage

	arxiv <command> [options]

# Commands

	fetch      Fetch and download specific papers
	sync       Sync paper metadata from arXiv OAI-PMH (bulk)
	stats      Show cache statistics
	search     Search cached papers (full-text search)
	get        Get a specific paper's info
	ls         List cached papers (alias: list)
	reindex    Rebuild search index and citations
	serve      Start web server to browse cached papers

# Environment

	ARXIV_CACHE    Cache directory (default: ~/.cache/arxiv)

# Fetching Papers

Fetch downloads paper metadata, TeX source, and optionally PDF:

	arxiv fetch 2301.00001              # Fetch paper + TeX source
	arxiv fetch -pdf 2301.00001         # Fetch paper + PDF only
	arxiv fetch -all 2301.00001         # Fetch paper + source + PDF
	arxiv fetch 2301.00001 2302.12345   # Fetch multiple papers

The fetch command also extracts citation references from TeX source files
and stores them in the local database for graph visualization.

# Listing Papers

List cached papers with various filters:

	arxiv ls                            # List all cached papers
	arxiv ls cs.AI                      # List papers in category cs.AI
	arxiv ls -src                       # Only papers with source downloaded
	arxiv ls -n 50                      # Limit to 50 results
	arxiv ls -a                         # Include metadata-only papers

# Searching

Full-text search across titles and abstracts:

	arxiv search "transformer attention"
	arxiv search -category cs.CL "language model"
	arxiv search -limit 50 "neural network"

# Web Interface

Start a local web server to browse papers with a citation graph visualization:

	arxiv serve                         # Start on default port 8080
	arxiv serve -port 3000              # Start on custom port

The web interface provides:
  - Full-text search with real-time results
  - Paper detail pages with abstracts and metadata
  - Interactive D3.js citation graph visualization
  - Category and author browsing
  - Direct arXiv ID/URL input for fetching new papers

# Syncing Metadata

Bulk sync paper metadata from arXiv's OAI-PMH API:

	arxiv sync                          # Sync all papers (slow, ~2.4M records)
	arxiv sync -set cs                  # Sync only computer science papers
	arxiv sync -from 2024-01-01         # Sync papers from date

Note: This downloads metadata only, not source files or PDFs.
Use 'arxiv fetch' to download individual papers with full content.

# Cache Structure

The cache is stored in ARXIV_CACHE (default ~/.cache/arxiv):

	~/.cache/arxiv/
	├── index.db          # SQLite database with metadata and FTS index
	├── pdf/              # Downloaded PDF files
	├── src/              # Extracted TeX source directories
	└── meta/             # Raw metadata files

# Examples

	# Fetch a paper and view it in the web UI
	arxiv fetch 2301.00001
	arxiv serve

	# Search for papers and fetch interesting ones
	arxiv search "attention mechanism"
	arxiv fetch 1706.03762

	# List all AI papers with source code
	arxiv ls -src cs.AI

	# Rebuild the citation graph after manual edits
	arxiv reindex
*/
package main

//go:generate go run github.com/tmc/misc/gocmddoc@master -output ../../README.md

const usageText = `arxiv - offline arXiv paper cache manager

Usage: arxiv <command> [options]

Commands:
  fetch      Fetch and download specific papers
  sync       Sync paper metadata from arXiv OAI-PMH
  stats      Show cache statistics
  search     Search cached papers (full-text)
  get        Get a specific paper's info
  ls         List cached papers
  reindex    Rebuild search index and citations
  serve      Start web server

Environment:
  ARXIV_CACHE  Cache directory (default: ~/.cache/arxiv)

Examples:
  arxiv fetch 2301.00001       Fetch paper + TeX source
  arxiv fetch -all 2301.00001  Fetch paper + source + PDF
  arxiv search "transformer"   Search cached papers
  arxiv ls cs.AI               List papers in category
  arxiv ls -src -n 50          List 50 papers with source
  arxiv serve                  Start web UI on :8080

Run 'go doc github.com/tmc/arxiv/cmd/arxiv' for full documentation.`
