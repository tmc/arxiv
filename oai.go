package arxiv

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const oaiBaseURL = "https://export.arxiv.org/oai2"

// OAIClient is an OAI-PMH client for arXiv.
type OAIClient struct {
	client  *http.Client
	baseURL string
}

// NewOAIClient creates a new OAI-PMH client.
func NewOAIClient() *OAIClient {
	return &OAIClient{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		baseURL: oaiBaseURL,
	}
}

// ListRecords fetches records from arXiv via OAI-PMH.
// If resumptionToken is empty, starts from the beginning with the given params.
// If resumptionToken is non-empty, continues from that point.
func (c *OAIClient) ListRecords(ctx context.Context, set string, from, until time.Time, resumptionToken string) (*OAIResponse, error) {
	params := url.Values{}
	params.Set("verb", "ListRecords")

	if resumptionToken != "" {
		params.Set("resumptionToken", resumptionToken)
	} else {
		params.Set("metadataPrefix", "arXiv")
		if set != "" {
			params.Set("set", set)
		}
		if !from.IsZero() {
			params.Set("from", from.Format("2006-01-02"))
		}
		if !until.IsZero() {
			params.Set("until", until.Format("2006-01-02"))
		}
	}

	reqURL := c.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		// arXiv rate limiting - should retry after delay
		return nil, fmt.Errorf("rate limited (503)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var oaiResp oaiPMHResponse
	if err := xml.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse xml: %w", err)
	}

	if oaiResp.Error.Code != "" {
		return nil, fmt.Errorf("oai error %s: %s", oaiResp.Error.Code, oaiResp.Error.Value)
	}

	result := &OAIResponse{
		ResumptionToken: oaiResp.ListRecords.ResumptionToken.Value,
		CompleteListSize: oaiResp.ListRecords.ResumptionToken.CompleteListSize,
		Cursor:          oaiResp.ListRecords.ResumptionToken.Cursor,
	}

	for _, rec := range oaiResp.ListRecords.Records {
		paper := Paper{
			ID:         rec.Metadata.ArXiv.ID,
			Title:      strings.TrimSpace(rec.Metadata.ArXiv.Title),
			Abstract:   strings.TrimSpace(rec.Metadata.ArXiv.Abstract),
			Authors:    formatAuthors(rec.Metadata.ArXiv.Authors),
			Categories: rec.Metadata.ArXiv.Categories,
			Comments:   rec.Metadata.ArXiv.Comments,
			JournalRef: rec.Metadata.ArXiv.JournalRef,
			DOI:        rec.Metadata.ArXiv.DOI,
			License:    rec.Metadata.ArXiv.License,
		}

		if rec.Metadata.ArXiv.Created != "" {
			paper.Created, _ = time.Parse("2006-01-02", rec.Metadata.ArXiv.Created)
		}
		if rec.Metadata.ArXiv.Updated != "" {
			paper.Updated, _ = time.Parse("2006-01-02", rec.Metadata.ArXiv.Updated)
		} else {
			paper.Updated = paper.Created
		}

		result.Papers = append(result.Papers, paper)
	}

	return result, nil
}

// OAIResponse contains the parsed response from an OAI-PMH ListRecords request.
type OAIResponse struct {
	Papers           []Paper
	ResumptionToken  string
	CompleteListSize int
	Cursor           int
}

func formatAuthors(authors []oaiAuthor) string {
	var parts []string
	for _, a := range authors {
		name := a.Forenames + " " + a.Keyname
		if a.Suffix != "" {
			name += " " + a.Suffix
		}
		parts = append(parts, strings.TrimSpace(name))
	}
	return strings.Join(parts, ", ")
}

// XML structures for OAI-PMH parsing

type oaiPMHResponse struct {
	XMLName     xml.Name       `xml:"OAI-PMH"`
	Error       oaiError       `xml:"error"`
	ListRecords oaiListRecords `xml:"ListRecords"`
}

type oaiError struct {
	Code  string `xml:"code,attr"`
	Value string `xml:",chardata"`
}

type oaiListRecords struct {
	Records         []oaiRecord         `xml:"record"`
	ResumptionToken oaiResumptionToken `xml:"resumptionToken"`
}

type oaiResumptionToken struct {
	Value            string `xml:",chardata"`
	CompleteListSize int    `xml:"completeListSize,attr"`
	Cursor           int    `xml:"cursor,attr"`
}

type oaiRecord struct {
	Header   oaiHeader   `xml:"header"`
	Metadata oaiMetadata `xml:"metadata"`
}

type oaiHeader struct {
	Identifier string   `xml:"identifier"`
	Datestamp  string   `xml:"datestamp"`
	SetSpec    []string `xml:"setSpec"`
}

type oaiMetadata struct {
	ArXiv oaiArXiv `xml:"arXiv"`
}

type oaiArXiv struct {
	ID         string      `xml:"id"`
	Created    string      `xml:"created"`
	Updated    string      `xml:"updated"`
	Title      string      `xml:"title"`
	Authors    []oaiAuthor `xml:"authors>author"`
	Categories string      `xml:"categories"`
	Comments   string      `xml:"comments"`
	JournalRef string      `xml:"journal-ref"`
	DOI        string      `xml:"doi"`
	License    string      `xml:"license"`
	Abstract   string      `xml:"abstract"`
}

type oaiAuthor struct {
	Keyname   string `xml:"keyname"`
	Forenames string `xml:"forenames"`
	Suffix    string `xml:"suffix"`
}
