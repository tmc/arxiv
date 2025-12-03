package arxiv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sajari/fuzzy"
)

// ExtractPDFText extracts text from a PDF file using pdftotext.
// Returns the extracted text and any error.
func ExtractPDFText(pdfPath string) (string, error) {
	if _, err := os.Stat(pdfPath); err != nil {
		return "", fmt.Errorf("PDF not found: %w", err)
	}

	cmd := exec.Command("pdftotext", pdfPath, "-")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext failed: %w: %s", err, stderr.String())
	}

	return out.String(), nil
}

// EnsurePDFText extracts and stores PDF text if not already cached.
func (c *Cache) EnsurePDFText(ctx context.Context, paperID string) (string, error) {
	paper, err := c.GetPaper(ctx, paperID)
	if err != nil {
		return "", err
	}

	if !paper.PDFDownloaded || paper.PDFPath == "" {
		return "", fmt.Errorf("PDF not downloaded for paper %s", paperID)
	}

	// Check if text is already cached
	if paper.PDFText != "" {
		return paper.PDFText, nil
	}

	// Extract text from PDF
	text, err := ExtractPDFText(paper.PDFPath)
	if err != nil {
		return "", fmt.Errorf("extract PDF text: %w", err)
	}

	// Store in database
	paper.PDFText = text
	if err := c.db.WithContext(ctx).Model(paper).Update("pdf_text", text).Error; err != nil {
		return "", fmt.Errorf("store PDF text: %w", err)
	}

	return text, nil
}

// SearchPDFs searches through PDF text content using fuzzy matching.
// Supports both exact and fuzzy search (typo-tolerant).
// Returns paper IDs that match the query.
func (c *Cache) SearchPDFs(ctx context.Context, query string, limit int, fuzzyMode bool) ([]PDFSearchResult, error) {
	if limit <= 0 {
		limit = 50
	}

	// Get all papers with PDFs downloaded
	var papers []Paper
	if err := c.db.WithContext(ctx).
		Select("id", "pdf_path", "pdf_text").
		Where("pdf_downloaded = ? AND pdf_path IS NOT NULL", true).
		Find(&papers).Error; err != nil {
		return nil, err
	}

	var results []PDFSearchResult
	lowerQuery := strings.ToLower(query)

	// Build fuzzy model if fuzzy mode enabled
	var model *fuzzy.Model
	if fuzzyMode {
		model = fuzzy.NewModel()
		model.SetThreshold(1) // Allow 1 character difference
		model.SetDepth(5)    // Search depth
	}

	// Search through cached text
	type scoredResult struct {
		result PDFSearchResult
		score  float64
	}
	var scoredResults []scoredResult

	for _, p := range papers {
		if p.PDFText == "" {
			continue
		}

		text := p.PDFText
		lowerText := strings.ToLower(text)

		var match bool
		var score float64
		var matchPos int = -1

		if fuzzyMode && model != nil {
			// Fuzzy search: find best match in text
			words := strings.Fields(lowerText)
			bestScore := 0.0
			bestWord := ""
			for _, word := range words {
				if len(word) < 3 {
					continue
				}
				s := model.SpellCheck(word)
				if s != word {
					// Check similarity
					sim := similarity(word, lowerQuery)
					if sim > bestScore {
						bestScore = sim
						bestWord = word
					}
				}
			}
			// Also check direct fuzzy match
			directSim := fuzzyMatch(lowerText, lowerQuery)
			if directSim > bestScore {
				bestScore = directSim
			}
			if bestScore > 0.6 { // 60% similarity threshold
				match = true
				score = bestScore
				matchPos = strings.Index(lowerText, bestWord)
				if matchPos == -1 {
					matchPos = strings.Index(lowerText, lowerQuery)
				}
			}
		} else {
			// Exact search (case-insensitive)
			matchPos = strings.Index(lowerText, lowerQuery)
			if matchPos != -1 {
				match = true
				score = 1.0
			}
		}

		if match {
			context := extractContextAt(text, matchPos, query, 200)
			scoredResults = append(scoredResults, scoredResult{
				result: PDFSearchResult{
					PaperID: p.ID,
					Context: context,
					Match:   true,
					Score:   score,
				},
				score: score,
			})
		}
	}

	// Sort by score (best matches first)
	for i := 0; i < len(scoredResults)-1; i++ {
		for j := i + 1; j < len(scoredResults); j++ {
			if scoredResults[j].score > scoredResults[i].score {
				scoredResults[i], scoredResults[j] = scoredResults[j], scoredResults[i]
			}
		}
	}

	// Take top results
	for i, sr := range scoredResults {
		if i >= limit {
			break
		}
		results = append(results, sr.result)
	}

	return results, nil
}

// fuzzyMatch calculates similarity between two strings (simple Levenshtein-like)
func fuzzyMatch(text, query string) float64 {
	if len(query) == 0 {
		return 0
	}
	if len(text) < len(query) {
		return 0
	}

	// Check if query appears in text (allowing for small differences)
	maxDist := len(query) / 3 // Allow 1/3 of query length as difference
	if maxDist < 1 {
		maxDist = 1
	}

	bestMatch := 0.0
	for i := 0; i <= len(text)-len(query); i++ {
		substr := text[i : i+len(query)]
		sim := similarity(substr, query)
		if sim > bestMatch {
			bestMatch = sim
		}
	}

	return bestMatch
}

// similarity calculates simple similarity between two strings (0.0 to 1.0)
func similarity(s1, s2 string) float64 {
	if len(s1) == 0 && len(s2) == 0 {
		return 1.0
	}
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Simple character overlap
	matches := 0
	minLen := len(s1)
	if len(s2) < minLen {
		minLen = len(s2)
	}

	for i := 0; i < minLen; i++ {
		if s1[i] == s2[i] {
			matches++
		}
	}

	return float64(matches) / float64(max(len(s1), len(s2)))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}


// extractContext extracts text around a match for display.
func extractContext(text, query string, contextLen int) string {
	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)
	idx := strings.Index(lowerText, lowerQuery)
	return extractContextAt(text, idx, query, contextLen)
}

// extractContextAt extracts text around a specific position.
func extractContextAt(text string, pos int, query string, contextLen int) string {
	if pos == -1 {
		// Return first part of text
		if len(text) > contextLen {
			return text[:contextLen] + "..."
		}
		return text
	}

	start := pos - contextLen/2
	if start < 0 {
		start = 0
	}
	end := pos + len(query) + contextLen/2
	if end > len(text) {
		end = len(text)
	}

	context := text[start:end]
	if start > 0 {
		context = "..." + context
	}
	if end < len(text) {
		context = context + "..."
	}

	return context
}

// PDFSearchResult represents a PDF search result.
type PDFSearchResult struct {
	PaperID string  `json:"paperId"`
	Context string  `json:"context"`
	Match   bool    `json:"match"`
	Score   float64 `json:"score,omitempty"` // Match quality score (0.0 to 1.0)
}

