package arxiv

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// arXiv ID patterns:
// - New format: YYMM.NNNNN (e.g., 2301.00001, 2301.12345v2)
// - Old format: archive/YYMMNNN (e.g., hep-th/9901001)
var arxivIDPatterns = []*regexp.Regexp{
	// arXiv:YYMM.NNNNN or arXiv:YYMM.NNNNNvN (case insensitive)
	regexp.MustCompile(`(?i)arXiv[:\s]+(\d{4}\.\d{4,5}(?:v\d+)?)`),
	// arXiv preprint arXiv:YYMM.NNNNN (common in bib files)
	regexp.MustCompile(`(?i)arXiv\s+preprint\s+arXiv[:\s]+(\d{4}\.\d{4,5}(?:v\d+)?)`),
	// arxiv.org/abs/YYMM.NNNNN
	regexp.MustCompile(`(?i)arxiv\.org/abs/(\d{4}\.\d{4,5}(?:v\d+)?)`),
	// arxiv.org/pdf/YYMM.NNNNN
	regexp.MustCompile(`(?i)arxiv\.org/pdf/(\d{4}\.\d{4,5}(?:v\d+)?)`),
	// Old format: arXiv:hep-th/9901001
	regexp.MustCompile(`(?i)arXiv[:\s]+([a-z-]+/\d{7}(?:v\d+)?)`),
	// arxiv.org/abs/hep-th/9901001
	regexp.MustCompile(`(?i)arxiv\.org/abs/([a-z-]+/\d{7}(?:v\d+)?)`),
	// LaTeX escaped: ar{X}iv:{\tt 1308.0850 or ar{X}iv: 1308.0850
	regexp.MustCompile(`ar.{0,5}iv.{0,20}(\d{4}\.\d{4,5})`),
	// Bare arXiv IDs in certain contexts (eprint field)
	regexp.MustCompile(`eprint\s*=\s*[{"']?(\d{4}\.\d{4,5}(?:v\d+)?)`),
	// Old format: hep-th/9901001, hep-ph/9905221, cond-mat/0001234, quant-ph/9901001
	regexp.MustCompile(`\b([a-z]+-[a-z]+/\d{7})\b`),
	// Old format single category: cs/0001001, math/0001001
	regexp.MustCompile(`\b((?:cs|math|astro-ph|gr-qc|nlin|nucl-ex|nucl-th|physics|q-bio|q-fin|stat)/\d{7})\b`),
	// Old format with subcategory: cs.LG/0001001, math.CO/0001001
	regexp.MustCompile(`(?i)\b([a-z]+\.[A-Z]{2}/\d{7})\b`),
}

// ExtractReferences extracts arXiv paper IDs from .bbl, .bib, and .tex files in the source directory.
// Falls back to PDF extraction using pdftotext if no refs found in text files.
func ExtractReferences(srcPath string) []string {
	if srcPath == "" {
		return nil
	}

	seen := make(map[string]bool)
	var refs []string
	var pdfFiles []string

	filepath.WalkDir(srcPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".pdf" {
			pdfFiles = append(pdfFiles, path)
			return nil
		}
		if ext != ".bbl" && ext != ".bib" && ext != ".tex" {
			return nil
		}

		ids := extractFromFile(path)
		for _, id := range ids {
			// Normalize: strip version suffix for deduplication
			normalized := normalizeArxivID(id)
			if !seen[normalized] {
				seen[normalized] = true
				refs = append(refs, normalized)
			}
		}
		return nil
	})

	// Fallback: if no refs found in text files, try PDFs
	if len(refs) == 0 && len(pdfFiles) > 0 {
		refs = extractFromPDFs(pdfFiles, seen)
	}

	return refs
}

func extractFromFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var ids []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, pat := range arxivIDPatterns {
			matches := pat.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				if len(m) > 1 {
					ids = append(ids, m[1])
				}
			}
		}
	}
	return ids
}

// extractFromPDFs uses pdftotext to extract text from PDF files and find arXiv IDs.
func extractFromPDFs(pdfFiles []string, seen map[string]bool) []string {
	var refs []string

	for _, pdfPath := range pdfFiles {
		cmd := exec.Command("pdftotext", pdfPath, "-")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			continue
		}

		text := out.String()
		for _, pat := range arxivIDPatterns {
			matches := pat.FindAllStringSubmatch(text, -1)
			for _, m := range matches {
				if len(m) > 1 {
					normalized := normalizeArxivID(m[1])
					if !seen[normalized] {
						seen[normalized] = true
						refs = append(refs, normalized)
					}
				}
			}
		}
	}

	return refs
}

// normalizeArxivID strips version suffixes (e.g., "2301.00001v2" -> "2301.00001").
func normalizeArxivID(id string) string {
	// Remove version suffix
	if idx := strings.LastIndex(id, "v"); idx > 0 {
		// Check if everything after 'v' is digits
		suffix := id[idx+1:]
		allDigits := true
		for _, c := range suffix {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && len(suffix) > 0 {
			return id[:idx]
		}
	}
	return id
}
