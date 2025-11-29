package arxiv

import (
	"strings"
	"time"
)

// Paper represents an arXiv paper's metadata.
type Paper struct {
	// ID is the arXiv identifier (e.g., "2301.00001" or "hep-th/9901001")
	ID string

	// Created is when the paper was first submitted
	Created time.Time

	// Updated is when the paper was last updated
	Updated time.Time

	// Title of the paper
	Title string

	// Abstract of the paper
	Abstract string

	// Authors as a single string (arXiv format)
	Authors string

	// Categories is a space-separated list of arXiv categories
	Categories string

	// Comments from the submitter (e.g., "10 pages, 3 figures")
	Comments string

	// JournalRef is the journal reference if published
	JournalRef string

	// DOI is the Digital Object Identifier if available
	DOI string

	// License URL
	License string

	// PDFPath is the local path to the PDF (if downloaded)
	PDFPath string

	// SourcePath is the local path to the TeX source (if downloaded)
	SourcePath string

	// PDFDownloaded indicates if the PDF has been downloaded
	PDFDownloaded bool

	// SourceDownloaded indicates if the source has been downloaded
	SourceDownloaded bool
}

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
