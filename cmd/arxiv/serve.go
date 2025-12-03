package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tmc/arxiv"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "..."
	},
	"parseAuthors":    parseAuthors,
	"parseCategories": parseCategories,
	"arxivIDToDate":   arxivIDToDate,
}).ParseFS(templateFS, "templates/*.html"))

func cmdServe(ctx context.Context, cacheDir string, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "Port to listen on")
	fs.Parse(args)

	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}

	srv := &server{cache: cache, cacheDir: cacheDir}
	mux := http.NewServeMux()

	// API routes (before other routes for proper matching)
	mux.HandleFunc("/api/v1/papers/", srv.handleAPIPaper)
	mux.HandleFunc("/api/v1/search", srv.handleAPISearch)
	mux.HandleFunc("/api/v1/search/pdf", srv.handleAPISearchPDF)
	mux.HandleFunc("/api/v1/categories", srv.handleAPICategories)
	mux.HandleFunc("/api/v1/stats", srv.handleAPIStats)

	// Web routes
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/search", srv.handleSearch)
	mux.HandleFunc("/paper/", srv.handlePaper)
	mux.HandleFunc("/abs/", srv.handleAbs)
	mux.HandleFunc("/author/", srv.handleAuthor)
	mux.HandleFunc("/category/", srv.handleCategory)
	mux.HandleFunc("/categories", srv.handleCategories)
	mux.HandleFunc("/src/", srv.handleSource)
	mux.HandleFunc("/pdf/", srv.handlePDF)
	mux.HandleFunc("/robots.txt", srv.handleRobots)
	mux.HandleFunc("/sitemap.xml", srv.handleSitemap)

	// Setup middleware
	cacheMW := newCacheMiddleware(5 * time.Minute) // Cache for 5 minutes
	rateLimiter := newRateLimiter(100, time.Minute) // 100 requests per minute

	// Apply middleware: rate limiting first, then caching
	handler := rateLimiter.Handler(cacheMW.Handler(mux))

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting server at http://localhost%s", addr)
	log.Printf("API available at http://localhost%s/api/v1/", addr)

	httpServer := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		httpServer.Shutdown(context.Background())
	}()

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

type server struct {
	cache    *arxiv.Cache
	cacheDir string
}

// sitemapURLs collects the public, crawlable URLs for the sitemap.
// It currently includes:
//   - Home page
//   - Categories index
//   - Individual categories
//   - Recently updated papers for each category
func (s *server) sitemapURLs(ctx context.Context) (arxiv.SitemapURLSet, error) {
	base := arxiv.SiteBaseURL()

	var urls arxiv.SitemapURLSet

	// Static top-level pages
	now := time.Now()
	urls = append(urls,
		arxiv.SitemapURL{
			Loc:        base + "/",
			LastMod:    &now,
			ChangeFreq: "daily",
			Priority:   1.0,
		},
		arxiv.SitemapURL{
			Loc:        base + "/categories",
			LastMod:    &now,
			ChangeFreq: "daily",
			Priority:   0.8,
		},
	)

	// Categories and a slice of recent papers per category
	categories, err := s.cache.ListCategories(ctx)
	if err != nil {
		return nil, err
	}

	for _, c := range categories {
		// Category listing page
		urls = append(urls, arxiv.SitemapURL{
			Loc:        fmt.Sprintf("%s/category/%s", base, c.Name),
			ChangeFreq: "daily",
			Priority:   0.7,
		})

		// A capped number of recent papers per category
		papers, err := s.cache.ListPapers(ctx, c.Name, 0, 50)
		if err != nil {
			continue
		}
		for _, p := range papers {
			lastMod := p.Updated
			urls = append(urls, arxiv.SitemapURL{
				Loc:        fmt.Sprintf("%s/abs/%s", base, p.ID),
				LastMod:    &lastMod,
				ChangeFreq: "weekly",
				Priority:   0.6,
			})
		}
	}

	return urls, nil
}

// handleSitemap serves the XML sitemap at /sitemap.xml.
func (s *server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	urls, err := s.sitemapURLs(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := arxiv.BuildSitemapXML(urls)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	stats, err := s.cache.Stats(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	papers, err := s.cache.ListPapers(ctx, "", 0, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":  "Home",
		"Stats":  stats,
		"Papers": papers,
		"Query":  "",
	}
	templates.ExecuteTemplate(w, "index", data)
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Check if query looks like an arXiv ID or URL - redirect to abs page
	if arxivID := extractArxivID(query); arxivID != "" {
		http.Redirect(w, r, "/abs/"+arxivID, http.StatusFound)
		return
	}

	ctx := r.Context()
	papers, err := s.cache.Search(ctx, query, "", 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// JSON format for live search
	if r.URL.Query().Get("format") == "json" {
		type searchResult struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Authors    string `json:"authors"`
			Categories string `json:"categories"`
			Src        bool   `json:"src"`
			PDF        bool   `json:"pdf"`
		}
		results := make([]searchResult, len(papers))
		for i, p := range papers {
			results[i] = searchResult{
				ID:         p.ID,
				Title:      p.Title,
				Authors:    p.Authors,
				Categories: p.Categories,
				Src:        p.SourceDownloaded,
				PDF:        p.PDFDownloaded,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
		return
	}

	data := map[string]any{
		"Title":  "Search",
		"Query":  query,
		"Papers": papers,
	}
	templates.ExecuteTemplate(w, "search", data)
}

type refInfo struct {
	ID        string
	Title     string
	HasTitle  bool // Has metadata (title available)
	HasSource bool // Has full source downloaded
	CitedBy   int
}

func (s *server) handlePaper(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/paper/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	// Handle /paper/:id/fetch - fetch paper on demand
	if strings.HasSuffix(path, "/fetch") {
		paperID := strings.TrimSuffix(path, "/fetch")

		// Fetch metadata and source
		opts := &arxiv.DownloadOptions{DownloadPDF: false, DownloadSource: true}
		_, err := s.cache.FetchAndDownload(ctx, paperID, opts)
		if err != nil {
			http.Error(w, "failed to fetch paper: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Redirect to paper page
		http.Redirect(w, r, "/paper/"+paperID, http.StatusSeeOther)
		return
	}

	// Handle /paper/:id/graph - return citation graph JSON
	if strings.HasSuffix(path, "/graph") {
		paperID := strings.TrimSuffix(path, "/graph")
		graph, err := s.cache.GetCitationGraph(ctx, paperID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(graph)
		return
	}

	// Handle /paper/:id/prefetch-refs - prefetch reference titles
	if strings.HasSuffix(path, "/prefetch-refs") {
		paperID := strings.TrimSuffix(path, "/prefetch-refs")
		if r.Method == http.MethodPost {
			// Synchronous prefetch - blocks until all titles are fetched
			err := s.cache.PrefetchReferenceTitles(ctx, paperID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			// Return JSON for AJAX requests
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		// GET returns status of uncached references
		uncached, _ := s.cache.UncachedReferenceCount(ctx, paperID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"uncached": uncached})
		return
	}

	// Handle /paper/:id/fetch-source - fetch source and extract citations
	if strings.HasSuffix(path, "/fetch-source") {
		paperID := strings.TrimSuffix(path, "/fetch-source")
		if r.Method == http.MethodPost {
			// Download source only (not PDF)
			opts := &arxiv.DownloadOptions{DownloadPDF: false, DownloadSource: true}
			if err := s.cache.DownloadPaper(ctx, paperID, opts); err != nil {
				http.Error(w, "failed to fetch source: "+err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/paper/"+paperID, http.StatusSeeOther)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle /paper/:id/status - return paper status JSON (for polling)
	if strings.HasSuffix(path, "/status") {
		paperID := strings.TrimSuffix(path, "/status")
		paper, err := s.cache.GetPaper(ctx, paperID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		refs, _ := s.cache.References(ctx, paperID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sourceDownloaded": paper.SourceDownloaded,
			"hasReferences":    len(refs) > 0,
			"refCount":         len(refs),
		})
		return
	}

	// Handle /paper/:id/refs - return references JSON (for live updates)
	if strings.HasSuffix(path, "/refs") {
		paperID := strings.TrimSuffix(path, "/refs")
		dbRefs, err := s.cache.References(ctx, paperID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type refJSON struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			HasTitle  bool   `json:"hasTitle"`
			HasSource bool   `json:"hasSource"`
			CitedBy   int    `json:"citedBy"`
		}
		refs := make([]refJSON, len(dbRefs))
		for i, r := range dbRefs {
			refs[i] = refJSON{
				ID:        r.ID,
				Title:     r.Title,
				HasTitle:  r.HasTitle,
				HasSource: r.HasSource,
			}
			if r.HasTitle {
				refs[i].CitedBy, _ = s.cache.CitedByCount(ctx, r.ID)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(refs)
		return
	}

	// Handle /paper/:id/export/:format - export paper (bibtex, ris, json)
	if strings.Contains(path, "/export/") {
		parts := strings.Split(path, "/export/")
		if len(parts) == 2 {
			paperID := parts[0]
			format := parts[1]
			paper, err := s.cache.GetPaper(ctx, paperID)
			if err != nil {
				http.Error(w, "paper not found", http.StatusNotFound)
				return
			}

			switch format {
			case "bibtex":
				w.Header().Set("Content-Type", "application/x-bibtex; charset=utf-8")
				w.Header().Set("Content-Disposition", `attachment; filename="`+paperID+`.bib"`)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(paper.ToBibTeX()))
				return
			case "ris":
				w.Header().Set("Content-Type", "application/x-research-info-systems; charset=utf-8")
				w.Header().Set("Content-Disposition", `attachment; filename="`+paperID+`.ris"`)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(paper.ToRIS()))
				return
			case "json":
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Content-Disposition", `attachment; filename="`+paperID+`.json"`)
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(paper)
				return
			}
		}
	}

	id := path

	// For canonical viewing, redirect plain /paper/{id} to /abs/{id}
	// while keeping the /paper/ namespace for auxiliary actions
	// like /paper/{id}/fetch, /graph, etc.
	if !strings.Contains(id, "/") {
		http.Redirect(w, r, "/abs/"+id, http.StatusMovedPermanently)
		return
	}

	// Fallback: if we somehow reach here with a nested path that is not
	// handled above, just 404 to avoid serving ambiguous routes.
	http.NotFound(w, r)
}

// handleAbs serves arXiv-style abstract URLs at /abs/{id}, mirroring arxiv.org.
// This allows users to switch between arxiv.org and arxiv.gg by only changing
// the domain part of the URL.
func (s *server) handleAbs(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/abs/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	s.renderPaper(w, r, id)
}

// renderPaper contains the core logic for rendering a paper page given an ID.
func (s *server) renderPaper(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	paper, err := s.cache.GetPaper(ctx, id)
	if err != nil {
		// Paper not in cache - check if it looks like a valid arXiv ID and auto-fetch
		if isArxivID(id) {
			// Fetch metadata and source
			opts := &arxiv.DownloadOptions{DownloadPDF: false, DownloadSource: true}
			paper, err = s.cache.FetchAndDownload(ctx, id, opts)
			if err != nil {
				http.Error(w, "Paper not found: "+err.Error(), http.StatusNotFound)
				return
			}
		} else {
			http.NotFound(w, r)
			return
		}
	}

	// Get citation count for this paper
	citedByCount, _ := s.cache.CitedByCount(ctx, id)

	var files []string
	if paper.SourceDownloaded && paper.SourcePath != "" {
		filepath.WalkDir(paper.SourcePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(paper.SourcePath, path)
			files = append(files, rel)
			return nil
		})
	}

	// Get paper list for sidebar
	paperList, _ := s.cache.GetPaperList(ctx, id)

	// Count uncached references
	uncachedCount := 0
	for _, p := range paperList {
		if !p.Cached && p.IsRef {
			uncachedCount++
		}
	}

	// Auto-fetch source in background if not downloaded
	fetchingSource := false
	if !paper.SourceDownloaded {
		fetchingSource = true
		go func() {
			bgCtx := context.Background()
			opts := &arxiv.DownloadOptions{DownloadPDF: false, DownloadSource: true}
			s.cache.DownloadPaper(bgCtx, id, opts)
		}()
	}
	// Note: Client handles prefetch via /prefetch-refs endpoint

	data := map[string]any{
		"Title":          paper.Title,
		"Paper":          paper,
		"Files":          files,
		"PaperList":      paperList,
		"UncachedCount":  uncachedCount,
		"CitedByCount":   citedByCount,
		"FetchingSource": fetchingSource,
	}
	templates.ExecuteTemplate(w, "paper", data)
}

func (s *server) handleAuthor(w http.ResponseWriter, r *http.Request) {
	author := strings.TrimPrefix(r.URL.Path, "/author/")
	if author == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	papers, err := s.cache.SearchByAuthor(ctx, author, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":  "Author: " + author,
		"Author": author,
		"Papers": papers,
	}
	templates.ExecuteTemplate(w, "author", data)
}

func (s *server) handlePDF(w http.ResponseWriter, r *http.Request) {
	// Routes: GET /pdf/{id} - serve PDF, POST /pdf/{id}/fetch - fetch PDF
	path := strings.TrimPrefix(r.URL.Path, "/pdf/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	// Check if this is a fetch request
	if strings.HasSuffix(path, "/fetch") {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		paperID := strings.TrimSuffix(path, "/fetch")
		returnTo := r.URL.Query().Get("return")

		// First ensure paper metadata exists (fetch if needed)
		paper, err := s.cache.Fetch(ctx, paperID)
		if err != nil {
			http.Error(w, "failed to fetch paper: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Download PDF
		opts := &arxiv.DownloadOptions{DownloadPDF: true, DownloadSource: false}
		if err := s.cache.DownloadPaper(ctx, paper.ID, opts); err != nil {
			http.Error(w, "failed to download PDF: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Redirect back
		if returnTo != "" {
			http.Redirect(w, r, "/paper/"+returnTo, http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/pdf/"+paperID, http.StatusSeeOther)
		}
		return
	}

	// Serve PDF: GET /pdf/{id}
	paperID := path
	paper, err := s.cache.GetPaper(ctx, paperID)
	if err != nil {
		// Ensure metadata exists (fetch from arxiv.org if needed)
		paper, err = s.cache.Fetch(ctx, paperID)
		if err != nil {
			http.Error(w, "failed to fetch paper: "+err.Error(), http.StatusNotFound)
			return
		}
	}

	// Download PDF if we don't have it yet or path is missing
	if !paper.PDFDownloaded || paper.PDFPath == "" {
		opts := &arxiv.DownloadOptions{DownloadPDF: true, DownloadSource: false}
		if err := s.cache.DownloadPaper(ctx, paper.ID, opts); err != nil {
			http.Error(w, "failed to download PDF: "+err.Error(), http.StatusInternalServerError)
			return
		}
		// Reload paper metadata to get updated PDFPath
		if p2, err := s.cache.GetPaper(ctx, paperID); err == nil {
			paper = p2
		}
	}

	if paper.PDFPath == "" {
		http.Error(w, "PDF path missing", http.StatusInternalServerError)
		return
	}

	// Verify file exists
	if _, err := os.Stat(paper.PDFPath); err != nil {
		http.Error(w, "PDF file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", paperID+".pdf"))
	http.ServeFile(w, r, paper.PDFPath)
}

func (s *server) handleSource(w http.ResponseWriter, r *http.Request) {
	// Path format: /src/{paperID}/{filepath...}
	path := strings.TrimPrefix(r.URL.Path, "/src/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	paperID := parts[0]
	filePath := parts[1]

	ctx := r.Context()
	paper, err := s.cache.GetPaper(ctx, paperID)
	if err != nil || !paper.SourceDownloaded || paper.SourcePath == "" {
		http.NotFound(w, r)
		return
	}

	// Security: ensure the requested path is within the source directory
	fullPath := filepath.Join(paper.SourcePath, filePath)
	fullPath = filepath.Clean(fullPath)
	if !strings.HasPrefix(fullPath, paper.SourcePath) {
		http.NotFound(w, r)
		return
	}

	// Check file exists
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, fullPath)
}

// handleRobots serves a static robots.txt file from the project root
// if it exists, otherwise returns a minimal default that points to
// the sitemap.
func (s *server) handleRobots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Try to serve robots.txt from the working directory
	if _, err := os.Stat("robots.txt"); err == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.ServeFile(w, r, "robots.txt")
		return
	}

	// Fallback minimal robots.txt
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nDisallow:\n\nSitemap: %s/sitemap.xml\n", arxiv.SiteBaseURL())
}

// parseAuthors splits an author string into individual author names.
// arXiv author format varies but is typically comma-separated or "and"-separated.
func parseAuthors(authors string) []string {
	// Replace " and " with comma for consistent splitting
	authors = strings.ReplaceAll(authors, " and ", ", ")

	var result []string
	for _, a := range strings.Split(authors, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			result = append(result, a)
		}
	}
	return result
}

// parseCategories splits a space-separated category string.
func parseCategories(categories string) []string {
	return strings.Fields(categories)
}

// arxivIDToDate parses an arXiv ID and returns a date string like "Feb 2023".
// New format: YYMM.NNNNN (e.g., 2302.13971 -> Feb 2023)
// Old format: category/YYMMNNN (e.g., hep-th/9901001 -> Jan 1999)
func arxivIDToDate(id string) string {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

	// Check for old format: category/YYMMNNN
	if idx := strings.Index(id, "/"); idx >= 0 {
		numPart := id[idx+1:]
		if len(numPart) >= 4 {
			yy := numPart[0:2]
			mm := numPart[2:4]
			year, month := parseYYMM(yy, mm)
			if month >= 1 && month <= 12 {
				return months[month-1] + " " + fmt.Sprintf("%d", year)
			}
		}
		return ""
	}

	// New format: YYMM.NNNNN or YYMM.NNNNNN
	if idx := strings.Index(id, "."); idx >= 0 {
		yymm := id[:idx]
		if len(yymm) == 4 {
			yy := yymm[0:2]
			mm := yymm[2:4]
			year, month := parseYYMM(yy, mm)
			if month >= 1 && month <= 12 {
				return months[month-1] + " " + fmt.Sprintf("%d", year)
			}
		}
	}

	return ""
}

func parseYYMM(yy, mm string) (year, month int) {
	// Parse year: 91-99 -> 1991-1999, 00-90 -> 2000-2090
	if len(yy) == 2 && yy[0] >= '0' && yy[0] <= '9' && yy[1] >= '0' && yy[1] <= '9' {
		y := int(yy[0]-'0')*10 + int(yy[1]-'0')
		if y >= 91 {
			year = 1900 + y
		} else {
			year = 2000 + y
		}
	}
	// Parse month
	if len(mm) == 2 && mm[0] >= '0' && mm[0] <= '1' && mm[1] >= '0' && mm[1] <= '9' {
		month = int(mm[0]-'0')*10 + int(mm[1]-'0')
	}
	return
}

// isArxivID checks if a string looks like a valid arXiv ID.
// Matches: YYMM.NNNNN, YYMM.NNNNNN, or category/NNNNNNN (e.g., hep-th/9901001)
func isArxivID(s string) bool {
	s = strings.TrimSpace(s)
	// New format: YYMM.NNNNN or YYMM.NNNNNN
	if idx := strings.Index(s, "."); idx == 4 {
		yymm := s[:4]
		rest := s[5:]
		// Check YYMM is numeric
		for _, c := range yymm {
			if c < '0' || c > '9' {
				return false
			}
		}
		// Check rest is numeric and reasonable length (5-6 digits)
		if len(rest) < 4 || len(rest) > 6 {
			return false
		}
		for _, c := range rest {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	// Old format: category/NNNNNNN (e.g., hep-th/9901001)
	if idx := strings.Index(s, "/"); idx > 0 {
		rest := s[idx+1:]
		if len(rest) >= 7 {
			for _, c := range rest {
				if c < '0' || c > '9' {
					return false
				}
			}
			return true
		}
	}
	return false
}

// extractArxivID extracts an arXiv ID from various input formats:
// - Plain ID: "2301.00001" -> "2301.00001"
// - URL: "https://arxiv.org/abs/2301.00001" -> "2301.00001"
// - URL with version: "https://arxiv.org/abs/2301.00001v2" -> "2301.00001"
// - PDF URL: "https://arxiv.org/pdf/2301.00001.pdf" -> "2301.00001"
// Returns empty string if no valid ID found.
func extractArxivID(input string) string {
	input = strings.TrimSpace(input)

	// Check if it's already a valid ID
	if isArxivID(input) {
		return input
	}

	// Try to extract from URL
	// Patterns: arxiv.org/abs/ID, arxiv.org/pdf/ID, export.arxiv.org/abs/ID
	for _, pattern := range []string{"/abs/", "/pdf/"} {
		if idx := strings.Index(input, pattern); idx >= 0 {
			id := input[idx+len(pattern):]
			// Remove trailing .pdf if present
			id = strings.TrimSuffix(id, ".pdf")
			// Remove version suffix (v1, v2, etc.)
			if vIdx := strings.LastIndex(id, "v"); vIdx > 0 {
				// Check if everything after 'v' is numeric
				allDigits := true
				for _, c := range id[vIdx+1:] {
					if c < '0' || c > '9' {
						allDigits = false
						break
					}
				}
				if allDigits && len(id[vIdx+1:]) > 0 {
					id = id[:vIdx]
				}
			}
			// Remove any query params or fragments
			if qIdx := strings.IndexAny(id, "?#"); qIdx >= 0 {
				id = id[:qIdx]
			}
			if isArxivID(id) {
				return id
			}
		}
	}

	return ""
}

func (s *server) handleCategory(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimPrefix(r.URL.Path, "/category/")
	if category == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	papers, err := s.cache.ListPapers(ctx, category, 0, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":    "Category: " + category,
		"Category": category,
		"Papers":   papers,
	}
	templates.ExecuteTemplate(w, "category", data)
}

func (s *server) handleCategories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	categories, err := s.cache.ListCategories(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":      "Categories",
		"Categories": categories,
	}
	templates.ExecuteTemplate(w, "categories", data)
}
