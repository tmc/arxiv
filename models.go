package arxiv

import (
	"time"
)

// Paper represents an arXiv paper's metadata.
type Paper struct {
	// ID is the arXiv identifier (e.g., "2301.00001" or "hep-th/9901001")
	ID string `gorm:"primaryKey"`

	// Created is when the paper was first submitted
	Created time.Time `gorm:"index"`

	// Updated is when the paper was last updated
	Updated time.Time `gorm:"index"`

	// Title of the paper
	Title string

	// Abstract of the paper
	Abstract string

	// Authors as a single string (arXiv format)
	Authors string

	// Categories is a space-separated list of arXiv categories
	Categories string `gorm:"index"`

	// Comments from the submitter (e.g., "10 pages, 3 figures")
	Comments string

	// JournalRef is the journal reference if published
	JournalRef string

	// DOI is the Digital Object Identifier if available
	DOI string

	// License URL
	License string

	// PDFPath is the local path to the PDF (if downloaded)
	PDFPath string `gorm:"column:pdf_path"`

	// SourcePath is the local path to the TeX source (if downloaded)
	SourcePath string `gorm:"column:src_path"`

	// PDFText is extracted text from PDF for search
	PDFText string `gorm:"type:text;column:pdf_text"`

	// PDFDownloaded indicates if the PDF has been downloaded
	PDFDownloaded bool `gorm:"column:pdf_downloaded"`

	// SourceDownloaded indicates if the source has been downloaded
	SourceDownloaded bool `gorm:"column:src_downloaded"`

	// MetadataUpdated timestamp
	MetadataUpdated *time.Time `gorm:"column:metadata_updated"`
}

func (Paper) TableName() string {
	return "papers"
}

// Citation represents a citation relationship between papers.
type Citation struct {
	FromID string `gorm:"primaryKey;column:from_id"`
	ToID   string `gorm:"primaryKey;column:to_id;index"`
}

func (Citation) TableName() string {
	return "citations"
}

// SyncState stores sync metadata.
type SyncState struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

func (SyncState) TableName() string {
	return "sync_state"
}

// DownloadQueueItem represents a queued download.
type DownloadQueueItem struct {
	PaperID   string `gorm:"primaryKey;column:paper_id"`
	Type      string
	Priority  int
	Added     *time.Time
	Attempts  int
	LastError string `gorm:"column:last_error"`
}

func (DownloadQueueItem) TableName() string {
	return "download_queue"
}

