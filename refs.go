package arxiv

import (
	"bufio"
	"os"
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
}

// ExtractReferences extracts arXiv paper IDs from .bbl and .bib files in the source directory.
func ExtractReferences(srcPath string) []string {
	if srcPath == "" {
		return nil
	}

	seen := make(map[string]bool)
	var refs []string

	filepath.WalkDir(srcPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".bbl" && ext != ".bib" {
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
