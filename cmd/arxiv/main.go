package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/tmc/arxiv"
)

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	// Default cache location
	cacheDir := os.Getenv("ARXIV_CACHE")
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".cache", "arxiv")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "fetch":
		cmdFetch(ctx, cacheDir, args)
	case "sync":
		cmdSync(ctx, cacheDir, args)
	case "stats":
		cmdStats(ctx, cacheDir, args)
	case "search":
		cmdSearch(ctx, cacheDir, args)
	case "get":
		cmdGet(ctx, cacheDir, args)
	case "list":
		cmdList(ctx, cacheDir, args)
	case "reindex":
		cmdReindex(ctx, cacheDir, args)
	case "serve":
		cmdServe(ctx, cacheDir, args)
	case "help":
		usage()
	default:
		log.Fatalf("unknown command: %s", cmd)
	}
}

func usage() {
	fmt.Println(`arxiv - offline arXiv cache manager

Usage: arxiv <command> [options]

Commands:
  fetch      Fetch and download specific papers
  sync       Sync paper metadata from arXiv OAI-PMH (bulk)
  stats      Show cache statistics
  search     Search cached papers
  get        Get a specific paper's info (cached only)
  list       List cached papers
  serve      Start web server to browse cached papers

Environment:
  ARXIV_CACHE  Cache directory (default: ~/.cache/arxiv)

Examples:
  arxiv fetch 2301.00001              # Fetch paper + source
  arxiv fetch -pdf 2301.00001         # Fetch paper + PDF
  arxiv fetch -all 2301.00001         # Fetch paper + source + PDF
  arxiv get 2301.00001                # Show cached paper info
  arxiv search "transformer"          # Search cached papers
  arxiv stats                         # Show cache stats
  arxiv list -limit 10                # List recent papers
  arxiv serve -port 8080             # Start web server`)
}

func cmdFetch(ctx context.Context, cacheDir string, args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	pdf := fs.Bool("pdf", false, "Download PDF")
	source := fs.Bool("source", true, "Download TeX source (default)")
	all := fs.Bool("all", false, "Download both PDF and source")
	fs.Parse(args)

	if fs.NArg() == 0 {
		log.Fatal("usage: arxiv fetch [options] <paper-id> [paper-id...]")
	}

	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	opts := &arxiv.DownloadOptions{
		DownloadPDF:    *pdf || *all,
		DownloadSource: *source || *all,
	}

	for _, id := range fs.Args() {
		fmt.Printf("Fetching %s...\n", id)

		paper, err := cache.FetchAndDownload(ctx, id, opts)
		if err != nil {
			log.Printf("  error: %v", err)
			continue
		}

		fmt.Printf("  Title: %s\n", paper.Title)
		fmt.Printf("  Authors: %s\n", paper.Authors)
		if paper.SourcePath != "" {
			fmt.Printf("  Source: %s\n", paper.SourcePath)
		}
		if paper.PDFPath != "" {
			fmt.Printf("  PDF: %s\n", paper.PDFPath)
		}
		fmt.Println()

		// Rate limit between papers
		if len(fs.Args()) > 1 {
			time.Sleep(3 * time.Second)
		}
	}
}

func cmdSync(ctx context.Context, cacheDir string, args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	set := fs.String("set", "", "arXiv set to sync (e.g., cs, physics)")
	from := fs.String("from", "", "Start date (YYYY-MM-DD)")
	fs.Parse(args)

	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	opts := &arxiv.SyncOptions{
		Set: *set,
		Progress: func(fetched, total int) {
			if total > 0 {
				fmt.Printf("\rSyncing: %d / %d papers (%.1f%%)", fetched, total, float64(fetched)/float64(total)*100)
			} else {
				fmt.Printf("\rSyncing: %d papers", fetched)
			}
		},
	}

	if *from != "" {
		opts.From, err = time.Parse("2006-01-02", *from)
		if err != nil {
			log.Fatalf("invalid date: %v", err)
		}
	}

	fmt.Println("Starting metadata sync (this downloads ~2.4M paper metadata)...")
	fmt.Println("Press Ctrl+C to stop; sync will resume from where it left off.")
	if err := cache.SyncMetadata(ctx, opts); err != nil {
		log.Fatalf("sync: %v", err)
	}
	fmt.Println("\nSync complete!")
}

func cmdStats(ctx context.Context, cacheDir string, args []string) {
	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	stats, err := cache.Stats(ctx)
	if err != nil {
		log.Fatalf("stats: %v", err)
	}

	fmt.Printf("Cache: %s\n", cacheDir)
	fmt.Printf("Total papers:       %d\n", stats.TotalPapers)
	fmt.Printf("PDFs downloaded:    %d\n", stats.PDFsDownloaded)
	fmt.Printf("Sources downloaded: %d\n", stats.SourcesDownloaded)
	fmt.Printf("Queued downloads:   %d\n", stats.QueuedDownloads)
}

func cmdSearch(ctx context.Context, cacheDir string, args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	category := fs.String("category", "", "Filter by category")
	limit := fs.Int("limit", 20, "Max results")
	fs.Parse(args)

	if fs.NArg() == 0 {
		log.Fatal("usage: arxiv search <query>")
	}

	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	query := fs.Arg(0)
	results, err := cache.Search(ctx, query, *category, *limit)
	if err != nil {
		log.Fatalf("search: %v", err)
	}

	if len(results) == 0 {
		fmt.Println("No results found.")
		return
	}

	for _, p := range results {
		fmt.Printf("[%s] %s\n", p.ID, p.Title)
		fmt.Printf("  %s\n", p.Authors)
		fmt.Printf("  Categories: %s\n", p.Categories)
		if p.SourceDownloaded {
			fmt.Printf("  [source cached]")
		}
		if p.PDFDownloaded {
			fmt.Printf(" [pdf cached]")
		}
		fmt.Println("\n")
	}
}

func cmdGet(ctx context.Context, cacheDir string, args []string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	fetch := fs.Bool("fetch", false, "Fetch from arXiv if not cached")
	fs.Parse(args)

	if fs.NArg() == 0 {
		log.Fatal("usage: arxiv get [-fetch] <paper-id>")
	}

	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	var paper *arxiv.Paper
	if *fetch {
		paper, err = cache.Fetch(ctx, fs.Arg(0))
	} else {
		paper, err = cache.GetPaper(ctx, fs.Arg(0))
	}
	if err != nil {
		log.Fatalf("get paper: %v", err)
	}

	fmt.Printf("ID:         %s\n", paper.ID)
	fmt.Printf("Title:      %s\n", paper.Title)
	fmt.Printf("Authors:    %s\n", paper.Authors)
	fmt.Printf("Categories: %s\n", paper.Categories)
	fmt.Printf("Created:    %s\n", paper.Created.Format("2006-01-02"))
	fmt.Printf("Updated:    %s\n", paper.Updated.Format("2006-01-02"))
	fmt.Printf("PDF:        %v\n", paper.PDFDownloaded)
	fmt.Printf("Source:     %v\n", paper.SourceDownloaded)
	if paper.PDFPath != "" {
		fmt.Printf("PDF Path:   %s\n", paper.PDFPath)
	}
	if paper.SourcePath != "" {
		fmt.Printf("Source Path: %s\n", paper.SourcePath)
	}
	fmt.Printf("\nAbstract:\n%s\n", paper.Abstract)
}

func cmdList(ctx context.Context, cacheDir string, args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	category := fs.String("category", "", "Filter by category")
	limit := fs.Int("limit", 20, "Max results")
	offset := fs.Int("offset", 0, "Offset for pagination")
	fs.Parse(args)

	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	papers, err := cache.ListPapers(ctx, *category, *offset, *limit)
	if err != nil {
		log.Fatalf("list: %v", err)
	}

	if len(papers) == 0 {
		fmt.Println("No papers cached.")
		return
	}

	for _, p := range papers {
		status := ""
		if p.SourceDownloaded {
			status += "[src]"
		}
		if p.PDFDownloaded {
			status += "[pdf]"
		}
		fmt.Printf("[%s] %s %s\n", p.ID, p.Title, status)
	}
}

func cmdReindex(ctx context.Context, cacheDir string, args []string) {
	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	fmt.Println("Rebuilding FTS index...")
	if err := cache.RebuildFTSIndex(ctx); err != nil {
		log.Fatalf("reindex fts: %v", err)
	}

	fmt.Println("Rebuilding citations...")
	if err := cache.RebuildAllCitations(ctx); err != nil {
		log.Fatalf("reindex citations: %v", err)
	}
	fmt.Println("Done.")
}
