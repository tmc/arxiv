package arxiv

import (
	"fmt"
	"strings"
)

// BibTeXEntry represents a BibTeX entry for a paper
type BibTeXEntry struct {
	Type      string            // @article, @misc, etc.
	Key       string            // Citation key
	Fields    map[string]string // BibTeX fields
}

// ToBibTeX converts a Paper to BibTeX format
func (p *Paper) ToBibTeX() string {
	entry := BibTeXEntry{
		Type:   "article",
		Key:    p.BibTeXKey(),
		Fields: make(map[string]string),
	}

	// Determine entry type
	if p.JournalRef != "" {
		entry.Type = "article"
	} else {
		entry.Type = "misc"
	}

	// Title
	if p.Title != "" {
		entry.Fields["title"] = p.Title
	}

	// Authors
	if p.Authors != "" {
		entry.Fields["author"] = p.formatAuthorsBibTeX()
	}

	// Year
	if !p.Created.IsZero() {
		entry.Fields["year"] = fmt.Sprintf("%d", p.Created.Year())
		entry.Fields["month"] = p.Created.Format("January")
	}

	// arXiv ID
	entry.Fields["eprint"] = p.ID
	entry.Fields["archivePrefix"] = "arXiv"
	entry.Fields["primaryClass"] = p.PrimaryCategory()

	// Abstract
	if p.Abstract != "" {
		entry.Fields["abstract"] = p.Abstract
	}

	// DOI
	if p.DOI != "" {
		entry.Fields["doi"] = p.DOI
	}

	// Journal reference
	if p.JournalRef != "" {
		entry.Fields["journal"] = p.JournalRef
	}

	// Comments
	if p.Comments != "" {
		entry.Fields["note"] = p.Comments
	}

	// URL
	entry.Fields["url"] = p.AbstractURL()

	// Build BibTeX string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("@%s{%s,\n", entry.Type, entry.Key))

	fieldOrder := []string{"title", "author", "year", "month", "journal", "eprint", "archivePrefix", "primaryClass", "doi", "url", "abstract", "note"}
	written := make(map[string]bool)

	for _, field := range fieldOrder {
		if value, ok := entry.Fields[field]; ok && value != "" {
			sb.WriteString(fmt.Sprintf("  %s = {%s},\n", field, escapeBibTeX(value)))
			written[field] = true
		}
	}

	// Write any remaining fields
	for field, value := range entry.Fields {
		if !written[field] && value != "" {
			sb.WriteString(fmt.Sprintf("  %s = {%s},\n", field, escapeBibTeX(value)))
		}
	}

	sb.WriteString("}\n")
	return sb.String()
}

// BibTeXKey generates a citation key for BibTeX
func (p *Paper) BibTeXKey() string {
	// Use first author's last name + year + first word of title
	key := "arxiv"
	if p.Authors != "" {
		parts := strings.Fields(p.Authors)
		if len(parts) > 0 {
			// Try to extract last name (usually first part before comma or last part)
			lastName := parts[0]
			if strings.Contains(lastName, ",") {
				lastName = strings.Split(lastName, ",")[0]
			}
			key = strings.ToLower(strings.Trim(lastName, ".,"))
		}
	}

	if !p.Created.IsZero() {
		key += fmt.Sprintf("%d", p.Created.Year())
	}

	if p.Title != "" {
		words := strings.Fields(p.Title)
		if len(words) > 0 {
			firstWord := strings.ToLower(words[0])
			firstWord = strings.Trim(firstWord, ".,!?;:")
			if len(firstWord) > 0 {
				key += firstWord[:min(len(firstWord), 5)]
			}
		}
	}

	// Fallback to ID
	if key == "arxiv" {
		key = strings.ReplaceAll(p.ID, ".", "")
		key = strings.ReplaceAll(key, "/", "")
	}

	return key
}

// formatAuthorsBibTeX formats authors for BibTeX (Last, First and Last, First)
func (p *Paper) formatAuthorsBibTeX() string {
	if p.Authors == "" {
		return ""
	}

	// Split by "and" or comma
	parts := strings.Split(p.Authors, " and ")
	if len(parts) == 1 {
		parts = strings.Split(p.Authors, ",")
	}

	var formatted []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// If already in "Last, First" format, use as-is
		if strings.Contains(part, ",") {
			formatted = append(formatted, part)
		} else {
			// Assume "First Last" format
			words := strings.Fields(part)
			if len(words) >= 2 {
				lastName := words[len(words)-1]
				firstName := strings.Join(words[:len(words)-1], " ")
				formatted = append(formatted, fmt.Sprintf("%s, %s", lastName, firstName))
			} else {
				formatted = append(formatted, part)
			}
		}
	}

	return strings.Join(formatted, " and ")
}

// escapeBibTeX escapes special characters in BibTeX strings
func escapeBibTeX(s string) string {
	s = strings.ReplaceAll(s, "{", "\\{")
	s = strings.ReplaceAll(s, "}", "\\}")
	s = strings.ReplaceAll(s, "&", "\\&")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "$", "\\$")
	s = strings.ReplaceAll(s, "#", "\\#")
	s = strings.ReplaceAll(s, "^", "\\textasciicircum{}")
	s = strings.ReplaceAll(s, "_", "\\_")
	s = strings.ReplaceAll(s, "~", "\\textasciitilde{}")
	s = strings.ReplaceAll(s, "\\", "\\textbackslash{}")
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ToRIS converts a Paper to RIS format
func (p *Paper) ToRIS() string {
	var sb strings.Builder
	sb.WriteString("TY  - JOUR\n")

	if p.Title != "" {
		sb.WriteString(fmt.Sprintf("TI  - %s\n", p.Title))
	}

	if p.Authors != "" {
		for _, author := range strings.Split(p.Authors, " and ") {
			author = strings.TrimSpace(author)
			if author != "" {
				sb.WriteString(fmt.Sprintf("AU  - %s\n", author))
			}
		}
	}

	if !p.Created.IsZero() {
		sb.WriteString(fmt.Sprintf("PY  - %d\n", p.Created.Year()))
		sb.WriteString(fmt.Sprintf("DA  - %s\n", p.Created.Format("2006/01/02")))
	}

	if p.Abstract != "" {
		sb.WriteString(fmt.Sprintf("AB  - %s\n", p.Abstract))
	}

	if p.DOI != "" {
		sb.WriteString(fmt.Sprintf("DO  - %s\n", p.DOI))
	}

	sb.WriteString(fmt.Sprintf("UR  - %s\n", p.AbstractURL()))
	sb.WriteString(fmt.Sprintf("M3  - arXiv:%s\n", p.ID))

	if p.JournalRef != "" {
		sb.WriteString(fmt.Sprintf("JO  - %s\n", p.JournalRef))
	}

	if p.PrimaryCategory() != "" {
		sb.WriteString(fmt.Sprintf("KW  - %s\n", p.PrimaryCategory()))
	}

	sb.WriteString("ER  - \n")
	return sb.String()
}

