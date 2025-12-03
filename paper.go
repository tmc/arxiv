package arxiv

import (
	"strings"
)

// Paper methods are defined here, struct is in models.go

// PrimaryCategory returns the primary (first) category.
func (p *Paper) PrimaryCategory() string {
	cats := strings.Fields(p.Categories)
	if len(cats) == 0 {
		return ""
	}
	return cats[0]
}

// CategoryList returns all categories as a slice.
func (p *Paper) CategoryList() []string {
	return strings.Fields(p.Categories)
}

// PDFURL returns the arXiv PDF download URL.
func (p *Paper) PDFURL() string {
	return "https://arxiv.org/pdf/" + p.ID + ".pdf"
}

// SourceURL returns the arXiv source download URL.
func (p *Paper) SourceURL() string {
	return "https://arxiv.org/e-print/" + p.ID
}

// AbstractURL returns the arXiv abstract page URL.
func (p *Paper) AbstractURL() string {
	return "https://arxiv.org/abs/" + p.ID
}
