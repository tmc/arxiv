package arxiv

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DownloadOptions configures paper downloads.
type DownloadOptions struct {
	// Concurrency is the number of parallel downloads (default 1)
	Concurrency int

	// RateLimit is the delay between downloads (default 3s per arXiv guidelines)
	RateLimit time.Duration

	// DownloadPDF enables PDF downloads
	DownloadPDF bool

	// DownloadSource enables TeX source downloads
	DownloadSource bool

	// Progress callback
	Progress func(paperID string, downloaded, total int)
}

// DownloadPaper downloads PDF and/or source for a single paper.
func (c *Cache) DownloadPaper(ctx context.Context, paperID string, opts *DownloadOptions) error {
	if opts == nil {
		opts = &DownloadOptions{DownloadPDF: true, DownloadSource: true}
	}

	paper, err := c.GetPaper(ctx, paperID)
	if err != nil {
		return fmt.Errorf("get paper: %w", err)
	}

	// Download or repair missing PDF
	if opts.DownloadPDF && (!paper.PDFDownloaded || paper.PDFPath == "") {
		pdfPath, err := c.downloadPDF(ctx, paper)
		if err != nil {
			return fmt.Errorf("download pdf: %w", err)
		}
		c.db.WithContext(ctx).Model(&Paper{}).Where("id = ?", paperID).
			Updates(map[string]interface{}{"pdf_path": pdfPath, "pdf_downloaded": true})
		
		// Extract PDF text in background for search
		go func() {
			bgCtx := context.Background()
			c.EnsurePDFText(bgCtx, paperID)
		}()
	}

	if opts.DownloadSource && !paper.SourceDownloaded {
		srcPath, err := c.downloadSource(ctx, paper)
		if err != nil {
			return fmt.Errorf("download source: %w", err)
		}
		c.db.WithContext(ctx).Model(&Paper{}).Where("id = ?", paperID).
			Updates(map[string]interface{}{"src_path": srcPath, "src_downloaded": true})

		// Extract and store citations
		if err := c.UpdateCitations(ctx, paperID, srcPath); err != nil {
			// Non-fatal: log but don't fail the download
			fmt.Printf("Warning: failed to extract citations for %s: %v\n", paperID, err)
		}
	}

	return nil
}

// GetPaper retrieves a paper by ID, using LRU cache when available.
func (c *Cache) GetPaper(ctx context.Context, id string) (*Paper, error) {
	// Check LRU cache first
	if cached, ok := c.paperLRU.Get(id); ok {
		if paper, ok := cached.(*Paper); ok {
			return paper, nil
		}
	}

	var p Paper
	if err := c.db.WithContext(ctx).Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}

	// Cache the result
	c.paperLRU.Put(id, &p)

	return &p, nil
}

func (c *Cache) downloadPDF(ctx context.Context, paper *Paper) (string, error) {
	// Organize by paper ID prefix for large-scale storage
	// e.g., 2301.00001 -> pdf/2301/2301.00001.pdf
	prefix := paperPrefix(paper.ID)
	dir := filepath.Join(c.root, "pdf", prefix)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, paper.ID+".pdf")
	if _, err := os.Stat(path); err == nil {
		return path, nil // Already exists
	}

	resp, err := httpGetWithContext(ctx, paper.PDFURL())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %s", resp.Status)
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		os.Remove(path)
		return "", err
	}

	return path, nil
}

func (c *Cache) downloadSource(ctx context.Context, paper *Paper) (string, error) {
	prefix := paperPrefix(paper.ID)
	dir := filepath.Join(c.root, "src", prefix)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	// Source files are typically gzipped tar archives
	srcDir := filepath.Join(dir, paper.ID)
	if _, err := os.Stat(srcDir); err == nil {
		return srcDir, nil // Already exists
	}

	resp, err := httpGetWithContext(ctx, paper.SourceURL())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %s", resp.Status)
	}

	// Create temp file to determine content type
	tmpFile, err := os.CreateTemp("", "arxiv-src-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		return "", err
	}

	// Try to extract as gzipped tar
	if err := extractSource(tmpPath, srcDir); err != nil {
		// If extraction fails, it might be a single TeX file
		// Copy it directly
		if err := os.MkdirAll(srcDir, 0755); err != nil {
			return "", err
		}
		data, err := os.ReadFile(tmpPath)
		if err != nil {
			return "", err
		}
		mainTex := filepath.Join(srcDir, "main.tex")
		if err := os.WriteFile(mainTex, data, 0644); err != nil {
			return "", err
		}
	}

	return srcDir, nil
}

func extractSource(srcPath, dstDir string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Try gzip first
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Security: prevent path traversal
		name := filepath.Clean(hdr.Name)
		if strings.HasPrefix(name, "..") {
			continue
		}

		target := filepath.Join(dstDir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}
			// Limit file size to 100MB
			_, err = io.CopyN(outFile, tr, 100*1024*1024)
			outFile.Close()
			if err != nil && err != io.EOF {
				return err
			}
		}
	}

	return nil
}

func paperPrefix(id string) string {
	// Handle both new format (2301.00001) and old format (hep-th/9901001)
	if strings.Contains(id, "/") {
		parts := strings.Split(id, "/")
		return parts[0]
	}
	if len(id) >= 4 {
		return id[:4]
	}
	return id
}

func httpGetWithContext(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}
