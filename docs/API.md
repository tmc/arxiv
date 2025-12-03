# arXiv Cache API Documentation

## REST API Endpoints

All API endpoints are prefixed with `/api/v1/`. Responses are JSON with the following structure:

```json
{
  "success": true,
  "data": { ... },
  "error": "..."
}
```

### Papers

#### Get Paper
```
GET /api/v1/papers/{id}
```

Returns paper metadata, citation count, and references.

**Example:**
```bash
curl http://localhost:8080/api/v1/papers/2301.00001
```

**Response:**
```json
{
  "success": true,
  "data": {
    "paper": {
      "id": "2301.00001",
      "title": "...",
      "authors": "...",
      "abstract": "...",
      ...
    },
    "citedByCount": 42,
    "references": [...]
  }
}
```

#### Get Citations
```
GET /api/v1/papers/{id}/citations
```

Returns papers that this paper cites.

**Example:**
```bash
curl http://localhost:8080/api/v1/papers/2301.00001/citations
```

#### Get Cited By
```
GET /api/v1/papers/{id}/cited-by?limit=50
```

Returns papers that cite this paper.

**Query Parameters:**
- `limit` (optional): Maximum number of results (default: 50)

#### Get Citation Graph
```
GET /api/v1/papers/{id}/graph
```

Returns citation graph data for visualization (nodes and edges).

**Example:**
```bash
curl http://localhost:8080/api/v1/papers/2301.00001/graph
```

#### Fetch Paper
```
POST /api/v1/papers/{id}/fetch?pdf=true&source=true
```

Fetches and downloads a paper from arXiv.

**Query Parameters:**
- `pdf` (optional): Download PDF (default: false)
- `source` (optional): Download source (default: true)

**Example:**
```bash
curl -X POST "http://localhost:8080/api/v1/papers/2301.00001/fetch?source=true"
```

#### Export Paper
```
GET /api/v1/papers/{id}/export/{format}
```

Exports paper in various formats.

**Formats:**
- `bibtex` - BibTeX format (.bib)
- `ris` - RIS format (.ris)
- `json` - JSON format (.json)

**Example:**
```bash
curl http://localhost:8080/api/v1/papers/2301.00001/export/bibtex
```

### Search

#### Search Papers
```
GET /api/v1/search?q={query}&category={category}&limit={limit}
```

Searches papers by title and abstract.

**Query Parameters:**
- `q` (required): Search query
- `category` (optional): Filter by category (e.g., "cs.AI")
- `limit` (optional): Maximum results (default: 20)

**Example:**
```bash
curl "http://localhost:8080/api/v1/search?q=transformer&category=cs.CL&limit=10"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "papers": [...],
    "count": 10
  }
}
```

### Categories

#### List Categories
```
GET /api/v1/categories
```

Returns all categories with paper counts.

**Example:**
```bash
curl http://localhost:8080/api/v1/categories
```

**Response:**
```json
{
  "success": true,
  "data": [
    {"name": "cs.AI", "count": 1234},
    {"name": "cs.CL", "count": 567},
    ...
  ]
}
```

### Statistics

#### Get Cache Statistics
```
GET /api/v1/stats
```

Returns cache statistics.

**Example:**
```bash
curl http://localhost:8080/api/v1/stats
```

**Response:**
```json
{
  "success": true,
  "data": {
    "totalPapers": 10000,
    "pdfsDownloaded": 5000,
    "sourcesDownloaded": 8000,
    "queuedDownloads": 0
  }
}
```

## Rate Limiting

API requests are rate-limited to 100 requests per minute per IP address. When rate limit is exceeded, the API returns HTTP 429 (Too Many Requests).

## Caching

API responses are cached for 5 minutes. Use `If-None-Match` header with ETag for conditional requests:

```bash
curl -H "If-None-Match: \"abc123\"" http://localhost:8080/api/v1/papers/2301.00001
```

If the resource hasn't changed, you'll get HTTP 304 (Not Modified).

## Web Export Endpoints

Papers can also be exported via web interface:

- `/paper/{id}/export/bibtex` - BibTeX export
- `/paper/{id}/export/ris` - RIS export
- `/paper/{id}/export/json` - JSON export

## Examples

### Fetch and export a paper
```bash
# Fetch paper
curl -X POST "http://localhost:8080/api/v1/papers/2301.00001/fetch?source=true"

# Export as BibTeX
curl http://localhost:8080/api/v1/papers/2301.00001/export/bibtex > paper.bib
```

### Search and get details
```bash
# Search
curl "http://localhost:8080/api/v1/search?q=attention&limit=5"

# Get details for first result
curl http://localhost:8080/api/v1/papers/1706.03762
```

### Get citation graph
```bash
curl http://localhost:8080/api/v1/papers/2301.00001/graph | jq '.data.nodes | length'
```

