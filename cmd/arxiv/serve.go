package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmc/arxiv"
)

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "..."
	},
	"parseAuthors":     parseAuthors,
	"parseCategories":  parseCategories,
	"arxivIDToDate":    arxivIDToDate,
}).Parse(`
{{define "head"}}
<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>{{.Title}} - arXiv Cache</title>
	<style>
		* { box-sizing: border-box; }
		body { font-family: system-ui, sans-serif; max-width: 900px; margin: 0 auto; padding: 1rem; line-height: 1.5; }
		a { color: #0066cc; }
		.search-form { margin: 1rem 0; }
		.search-form input[type="text"] { padding: 0.5rem; width: 300px; font-size: 1rem; }
		.search-form button { padding: 0.5rem 1rem; font-size: 1rem; cursor: pointer; }
		.paper { border-bottom: 1px solid #eee; padding: 1rem 0; }
		.paper-id { font-family: monospace; color: #666; }
		.paper-title { font-size: 1.1rem; font-weight: 600; margin: 0.25rem 0; }
		.paper-authors { color: #444; }
		.paper-categories { font-size: 0.9rem; color: #666; }
		.paper-abstract { margin: 1rem 0; white-space: pre-wrap; }
		.paper-meta { font-size: 0.9rem; color: #666; margin: 0.5rem 0; }
		.badge { display: inline-block; background: #e0e0e0; padding: 0.1rem 0.4rem; border-radius: 3px; font-size: 0.8rem; margin-right: 0.25rem; }
		.badge-src { background: #d4edda; }
		.badge-pdf { background: #cce5ff; }
		.files { margin: 1rem 0; }
		.files ul { list-style: none; padding: 0; }
		.files li { font-family: monospace; padding: 0.25rem 0; }
		.nav { margin-bottom: 1rem; }
		pre { background: #f5f5f5; padding: 1rem; overflow-x: auto; }
		.refs { margin: 1rem 0; }
		.refs ul { list-style: none; padding: 0; }
		.refs li { padding: 0.25rem 0; font-family: monospace; }
		.ref-cached { color: #28a745; }
		.ref-uncached { color: #6c757d; }
		.author-link { color: #0066cc; text-decoration: none; }
		.author-link:hover { text-decoration: underline; }
		.btn { display: inline-block; padding: 0.4rem 0.8rem; background: #0066cc; color: white; text-decoration: none; border-radius: 4px; border: none; cursor: pointer; font-size: 0.9rem; }
		.btn:hover { background: #0052a3; }
		.btn-sm { padding: 0.2rem 0.5rem; font-size: 0.8rem; }
		.btn-secondary { background: #6c757d; }
		.btn-secondary:hover { background: #5a6268; }
		.pdf-section { margin: 1rem 0; padding: 1rem; background: #f8f9fa; border-radius: 4px; }
		.fetch-form { display: inline; }
		.fetch-link { color: #6c757d; font-size: 0.85em; }
		.cite-count { color: #6c757d; font-size: 0.85em; }
		.cat-link { font-family: monospace; background: #f0f0f0; padding: 0.1rem 0.3rem; border-radius: 3px; text-decoration: none; font-size: 0.9em; }
		.cat-link:hover { background: #e0e0e0; }
		.categories-list { display: flex; flex-wrap: wrap; gap: 0.5rem; }
		.cat-item { background: #f8f9fa; padding: 0.5rem 1rem; border-radius: 4px; }
		.cat-item a { text-decoration: none; font-family: monospace; }
		.cat-count { color: #666; font-size: 0.9em; }
		.search-results { margin-top: 1rem; }
		.search-status { color: #666; font-size: 0.9em; margin: 0.5rem 0; }
		.graph-section { margin: 2rem 0; }
		.graph-container { border: 1px solid #ddd; border-radius: 4px; background: #fafafa; position: relative; }
		.graph-container svg { display: block; }
		.graph-controls { position: absolute; top: 0.5rem; right: 0.5rem; z-index: 10; }
		.graph-legend { position: absolute; bottom: 0.5rem; left: 0.5rem; background: rgba(255,255,255,0.9); padding: 0.5rem; border-radius: 4px; font-size: 11px; }
		.legend-item { display: flex; align-items: center; margin: 0.15rem 0; }
		.legend-color { width: 10px; height: 10px; border-radius: 50%; margin-right: 0.4rem; }
		.graph-tooltip { position: fixed; padding: 6px 10px; background: rgba(0,0,0,0.85); color: white; border-radius: 4px; font-size: 11px; pointer-events: none; max-width: 280px; z-index: 1000; display: none; }
		.graph-node { cursor: pointer; transition: r 0.15s; }
		.graph-link { stroke: #999; stroke-opacity: 0.5; fill: none; }
		.highlight-node { stroke: #ff6b00; stroke-width: 3px; }
		.highlight-link { stroke: #ff6b00; stroke-opacity: 1; stroke-width: 2px; }
		.highlight-ref { background: #fff3cd !important; }
		@keyframes pulse { 0%, 100% { transform: scale(1); } 50% { transform: scale(1.3); } }
		.pulse-node { animation: pulse 0.6s ease-in-out; }
		.graph-fullscreen { position: fixed; top: 0; left: 0; right: 0; bottom: 0; z-index: 1000; background: white; }
		.graph-fullscreen .graph-container { width: 100%; height: 100%; border: none; border-radius: 0; }
		.prefetch-status { font-size: 0.8em; color: #666; font-weight: normal; margin-left: 0.5rem; }
		.prefetch-status::before { content: ""; display: inline-block; width: 12px; height: 12px; border: 2px solid #ddd; border-top-color: #0066cc; border-radius: 50%; margin-right: 0.4rem; animation: spin 1s linear infinite; vertical-align: middle; }
		@keyframes spin { to { transform: rotate(360deg); } }
		.prefetch-done { color: #28a745; }
		.prefetch-done::before { display: none; }
	</style>
</head>
<body>
<div class="nav"><a href="/">Home</a> | <a href="/categories">Categories</a></div>
{{end}}

{{define "foot"}}
</body>
</html>
{{end}}

{{define "index"}}
{{template "head" .}}
<h1>arXiv Cache</h1>
<form class="search-form" action="/search" method="get" id="search-form">
	<input type="text" name="q" id="search-input" placeholder="Search papers..." value="{{.Query}}" autocomplete="off">
	<button type="submit">Search</button>
</form>
<div id="search-results" class="search-results"></div>
<p>{{.Stats.TotalPapers}} papers cached ({{.Stats.SourcesDownloaded}} with source, {{.Stats.PDFsDownloaded}} with PDF)</p>
<div id="recent-papers">
<h2>Recent Papers</h2>
{{range .Papers}}
<div class="paper">
	<span class="paper-id">{{.ID}}</span>
	{{if .SourceDownloaded}}<span class="badge badge-src">src</span>{{end}}
	{{if .PDFDownloaded}}<span class="badge badge-pdf">pdf</span>{{end}}
	<div class="paper-title"><a href="/paper/{{.ID}}">{{.Title}}</a></div>
	<div class="paper-authors">{{.Authors}}</div>
	<div class="paper-categories">{{range $i, $c := parseCategories .Categories}}{{if $i}} {{end}}<a class="cat-link" href="/category/{{$c}}">{{$c}}</a>{{end}}</div>
</div>
{{else}}
<p>No papers cached yet. Use <code>arxiv fetch</code> or <code>arxiv sync</code> to add papers.</p>
{{end}}
</div>
<script>
(function() {
	const input = document.getElementById('search-input');
	const results = document.getElementById('search-results');
	const recent = document.getElementById('recent-papers');
	let timeout = null;

	function renderPaper(p) {
		const cats = (p.categories || '').split(' ').filter(c => c).map(c =>
			'<a class="cat-link" href="/category/' + c + '">' + c + '</a>'
		).join(' ');
		const badges = (p.src ? '<span class="badge badge-src">src</span>' : '') +
		               (p.pdf ? '<span class="badge badge-pdf">pdf</span>' : '');
		return '<div class="paper">' +
			'<span class="paper-id">' + p.id + '</span> ' + badges +
			'<div class="paper-title"><a href="/paper/' + p.id + '">' + escapeHtml(p.title) + '</a></div>' +
			'<div class="paper-authors">' + escapeHtml(p.authors) + '</div>' +
			'<div class="paper-categories">' + cats + '</div>' +
		'</div>';
	}

	function escapeHtml(s) {
		const div = document.createElement('div');
		div.textContent = s;
		return div.innerHTML;
	}

	function doSearch(query) {
		if (!query.trim()) {
			results.innerHTML = '';
			recent.style.display = '';
			return;
		}
		results.innerHTML = '<p class="search-status">Searching...</p>';
		recent.style.display = 'none';

		fetch('/search?format=json&q=' + encodeURIComponent(query))
			.then(r => r.json())
			.then(data => {
				if (!data || data.length === 0) {
					results.innerHTML = '<p class="search-status">No results for "' + escapeHtml(query) + '"</p>';
					return;
				}
				results.innerHTML = '<p class="search-status">' + data.length + ' results for "' + escapeHtml(query) + '"</p>' +
					data.map(renderPaper).join('');
			})
			.catch(err => {
				results.innerHTML = '<p class="search-status">Search error</p>';
			});
	}

	input.addEventListener('input', function() {
		clearTimeout(timeout);
		timeout = setTimeout(() => doSearch(input.value), 300);
	});
})();
</script>
{{template "foot" .}}
{{end}}

{{define "search"}}
{{template "head" .}}
<h1>Search Results</h1>
<form class="search-form" action="/search" method="get">
	<input type="text" name="q" placeholder="Search papers..." value="{{.Query}}">
	<button type="submit">Search</button>
</form>
<p>{{len .Papers}} results for "{{.Query}}"</p>
{{range .Papers}}
<div class="paper">
	<span class="paper-id">{{.ID}}</span>
	{{if .SourceDownloaded}}<span class="badge badge-src">src</span>{{end}}
	{{if .PDFDownloaded}}<span class="badge badge-pdf">pdf</span>{{end}}
	<div class="paper-title"><a href="/paper/{{.ID}}">{{.Title}}</a></div>
	<div class="paper-authors">{{.Authors}}</div>
	<div class="paper-categories">{{range $i, $c := parseCategories .Categories}}{{if $i}} {{end}}<a class="cat-link" href="/category/{{$c}}">{{$c}}</a>{{end}}</div>
</div>
{{else}}
<p>No results found.</p>
{{end}}
{{template "foot" .}}
{{end}}

{{define "paper"}}
<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<meta name="view-transition" content="same-origin">
	<title>{{.Paper.Title}} - arXiv Cache</title>
	<script src="https://d3js.org/d3.v7.min.js"></script>
	<style>
		/* View Transitions */
		::view-transition-old(root) { animation: fade-out 0.2s ease-out; }
		::view-transition-new(root) { animation: fade-in 0.2s ease-in; }
		@keyframes fade-out { from { opacity: 1; } to { opacity: 0; } }
		@keyframes fade-in { from { opacity: 0; } to { opacity: 1; } }

		/* Hero transition for paper cards expanding into full view */
		::view-transition-old(paper-hero) {
			animation: scale-down 0.3s ease-out;
			transform-origin: center;
		}
		::view-transition-new(paper-hero) {
			animation: scale-up 0.3s ease-out;
			transform-origin: center;
		}
		@keyframes scale-down {
			from { transform: scale(1); opacity: 1; }
			to { transform: scale(0.8); opacity: 0; }
		}
		@keyframes scale-up {
			from { transform: scale(0.8); opacity: 0; }
			to { transform: scale(1); opacity: 1; }
		}

		* { box-sizing: border-box; }
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; line-height: 1.6; color: #1e293b; max-width: 960px; margin: 0 auto; padding: 1rem; }
		a { color: #1d4ed8; text-decoration: none; }
		a:hover { text-decoration: underline; }
		h1 { font-size: 1.5rem; font-weight: 600; margin-bottom: 0.5rem; view-transition-name: paper-hero; }
		h2 { font-size: 1.1rem; font-weight: 600; margin: 1.5rem 0 0.75rem; border-bottom: 1px solid #e2e8f0; padding-bottom: 0.25rem; }

		.nav { margin-bottom: 1.5rem; font-size: 0.875rem; }
		.nav a { margin-right: 1rem; }
		.meta { font-size: 0.875rem; color: #64748b; margin-bottom: 0.5rem; }
		.meta code { font-family: monospace; background: #f1f5f9; padding: 0.1rem 0.3rem; border-radius: 3px; }
		.authors { margin-bottom: 0.5rem; }
		.categories { margin-bottom: 1rem; }
		.cat-link { display: inline-block; font-family: monospace; font-size: 0.8rem; background: #f1f5f9; padding: 0.15rem 0.5rem; border-radius: 3px; margin-right: 0.25rem; margin-bottom: 0.25rem; }
		.abstract { white-space: pre-wrap; color: #334155; margin-bottom: 1rem; }
		.actions { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-bottom: 1rem; }
		.btn { display: inline-block; padding: 0.4rem 0.75rem; font-size: 0.8rem; background: #1d4ed8; color: #fff; border-radius: 4px; border: none; cursor: pointer; }
		.btn:hover { background: #1e40af; text-decoration: none; }
		.btn-secondary { background: #64748b; }
		.btn-secondary:hover { background: #475569; }
		.links { font-size: 0.875rem; margin-bottom: 1rem; }
		.links a { margin-right: 1rem; }

		/* Citation graph */
		.graph-container { position: relative; background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 6px; height: 500px; margin: 1rem 0; }
		.graph-controls { position: absolute; top: 0.5rem; right: 0.5rem; display: flex; gap: 0.5rem; z-index: 10; }
		.graph-btn { padding: 0.3rem 0.6rem; font-size: 0.75rem; background: #fff; border: 1px solid #e2e8f0; border-radius: 4px; cursor: pointer; }
		.graph-btn:hover { background: #f1f5f9; }
		.graph-legend { position: absolute; bottom: 0.5rem; left: 0.5rem; background: #fff; border: 1px solid #e2e8f0; border-radius: 4px; padding: 0.4rem 0.6rem; font-size: 0.7rem; }
		.legend-item { display: flex; align-items: center; gap: 0.4rem; margin: 0.15rem 0; }
		.legend-dot { width: 10px; height: 10px; border-radius: 50%; }
		.graph-tooltip { position: fixed; padding: 0.5rem 0.75rem; background: rgba(0,0,0,0.9); color: #fff; border-radius: 4px; font-size: 0.75rem; max-width: 300px; pointer-events: none; z-index: 100; display: none; }
		.graph-node { cursor: pointer; stroke: #fff; stroke-width: 1.5px; }
		.graph-node:hover { stroke-width: 3px; }
		.graph-node.selected { stroke: #1d4ed8; stroke-width: 3px; }
		.graph-link { stroke: #94a3b8; stroke-opacity: 0.4; fill: none; }
		.graph-link.highlighted { stroke: #1d4ed8; stroke-opacity: 1; stroke-width: 2px; }
		.fullscreen { position: fixed; top: 0; left: 0; right: 0; bottom: 0; z-index: 1000; height: 100vh; border-radius: 0; }

		/* References list */
		.refs { list-style: none; padding: 0; }
		.refs li { padding: 0.5rem 0.75rem; border-bottom: 1px solid #f1f5f9; font-size: 0.875rem; border-radius: 4px; margin-bottom: 2px; cursor: pointer; transition: background 0.15s, transform 0.1s; }
		.refs li:hover { background: #eff6ff; transform: translateX(2px); }
		.refs li.transitioning { view-transition-name: paper-hero; }
		.ref-id { font-family: monospace; color: #64748b; }
		.ref-title { color: #1e293b; font-weight: 500; }
		.ref-meta { font-size: 0.75rem; color: #94a3b8; }
		.ref-cached { color: #059669; }
		.ref-uncached { color: #94a3b8; font-style: italic; }
		.ref-date { color: #94a3b8; font-size: 0.8rem; }
		.badge { display: inline-block; font-size: 0.65rem; padding: 0.1rem 0.3rem; border-radius: 2px; margin-left: 0.25rem; }
		.badge-src { background: #dcfce7; color: #166534; }
		.badge-ref { background: #dbeafe; color: #1e40af; }
		.badge-citing { background: #fef3c7; color: #92400e; }

		/* Files list */
		.files { list-style: none; padding: 0; }
		.files li { font-family: monospace; font-size: 0.875rem; padding: 0.25rem 0; }

		/* Loading */
		.loading { font-size: 0.8rem; color: #64748b; }
		.loading::before { content: ""; display: inline-block; width: 10px; height: 10px; border: 2px solid #e2e8f0; border-top-color: #1d4ed8; border-radius: 50%; margin-right: 0.4rem; animation: spin 1s linear infinite; vertical-align: middle; }
		@keyframes spin { to { transform: rotate(360deg); } }
	</style>
</head>
<body>
<div class="nav"><a href="/">Home</a> <a href="/categories">Categories</a></div>

<h1>{{.Paper.Title}}</h1>
<div class="meta">
	<code>{{.Paper.ID}}</code> · {{.Paper.Created.Format "2006-01-02"}} · Cited by {{.CitedByCount}}
</div>
<div class="authors">{{range $i, $a := parseAuthors .Paper.Authors}}{{if $i}}, {{end}}<a href="/author/{{$a}}">{{$a}}</a>{{end}}</div>
<div class="categories">{{range $i, $c := parseCategories .Paper.Categories}}<a class="cat-link" href="/category/{{$c}}">{{$c}}</a>{{end}}</div>

<div class="abstract">{{.Paper.Abstract}}</div>

<div class="actions">
	{{if .Paper.PDFDownloaded}}<a class="btn" href="/pdf/{{.Paper.ID}}">View PDF</a>{{else}}<form style="display:inline" method="POST" action="/pdf/{{.Paper.ID}}/fetch"><button class="btn btn-secondary">Fetch PDF</button></form>{{end}}
</div>
{{if .FetchingSource}}<div class="loading" id="source-status">Fetching TeX source and extracting references...</div>{{end}}
<div class="links">
	<a href="{{.Paper.AbstractURL}}">arXiv</a>
	<a href="{{.Paper.PDFURL}}">PDF (external)</a>
	{{if .Paper.DOI}}<a href="https://doi.org/{{.Paper.DOI}}">DOI</a>{{end}}
</div>

<h2>Citation Graph</h2>
<div class="graph-container" id="graph-container">
	<div class="graph-controls">
		<button class="graph-btn" id="reset-btn">Reset</button>
		<button class="graph-btn" id="fullscreen-btn">Fullscreen</button>
	</div>
	<div class="graph-legend">
		<div class="legend-item"><div class="legend-dot" style="background:#1d4ed8;"></div> This paper</div>
		<div class="legend-item"><div class="legend-dot" style="background:#334155;"></div> Reference</div>
		<div class="legend-item"><div class="legend-dot" style="background:#10b981;"></div> Citing</div>
		<div class="legend-item"><div class="legend-dot" style="background:#94a3b8;"></div> Uncached</div>
	</div>
</div>
<div class="graph-tooltip" id="tooltip"></div>

{{if .PaperList}}
<h2>References{{if .UncachedCount}} <span class="loading" id="prefetch-status">Loading titles...</span>{{end}}</h2>
<ul class="refs" id="refs-list">
{{range .PaperList}}{{if .IsRef}}
<li data-id="{{.ID}}">
	<span class="ref-id">{{.ID}}</span>
	{{if .Cached}}<a class="ref-title" href="/paper/{{.ID}}">{{.Title}}</a> <span class="ref-date">({{arxivIDToDate .ID}})</span>{{if .Citations}} <span class="ref-meta">{{.Citations}} cites</span>{{end}}
	{{else}}<span class="ref-uncached">{{.ID}}</span> <span class="ref-date">({{arxivIDToDate .ID}})</span> <a href="/paper/{{.ID}}/fetch">[fetch]</a>{{end}}
</li>
{{end}}{{end}}
</ul>

<h2>Cited By</h2>
<ul class="refs" id="citing-list">
{{range .PaperList}}{{if .IsCiting}}
<li data-id="{{.ID}}">
	<span class="ref-id">{{.ID}}</span>
	<a class="ref-title" href="/paper/{{.ID}}">{{.Title}}</a> <span class="ref-date">({{arxivIDToDate .ID}})</span>{{if .Citations}} <span class="ref-meta">{{.Citations}} cites</span>{{end}}
</li>
{{end}}{{end}}
</ul>
{{end}}

{{if .Files}}
<h2>Source Files</h2>
<ul class="files">
{{range .Files}}<li><a href="/src/{{$.Paper.ID}}/{{.}}">{{.}}</a></li>{{end}}
</ul>
{{end}}

<script>
(function() {
	const paperID = "{{.Paper.ID}}";
	const container = document.getElementById('graph-container');
	const tooltip = document.getElementById('tooltip');
	let simulation, svg, g, nodes, links;

	// FLIP-style navigation with View Transitions API
	function navigateWithTransition(element, url) {
		if (!document.startViewTransition) {
			window.location.href = url;
			return;
		}

		// Apply view-transition-name to clicked element
		if (element.tagName === 'circle') {
			// For SVG circles (graph nodes), we need a workaround
			// SVG elements don't support view-transition-name well
			// Fall back to root transition
			document.startViewTransition(() => { window.location.href = url; });
		} else if (element.closest) {
			// For DOM elements like list items
			const li = element.closest('li');
			if (li) {
				li.classList.add('transitioning');
			}
			document.startViewTransition(() => { window.location.href = url; });
		} else {
			document.startViewTransition(() => { window.location.href = url; });
		}
	}

	// Make reference list items clickable
	document.querySelectorAll('.refs li').forEach(li => {
		const id = li.dataset.id;
		if (!id) return;

		// Find if this is cached (has a link) or uncached
		const link = li.querySelector('a.ref-title');
		const fetchLink = li.querySelector('a[href$="/fetch"]');

		li.addEventListener('click', (e) => {
			// Don't intercept if clicking directly on a link
			if (e.target.tagName === 'A') return;

			const url = link ? '/paper/' + id : '/paper/' + id + '/fetch';
			navigateWithTransition(li, url);
		});
	});

	// Paper list for coloring nodes (will be updated dynamically)
	let paperList = [
		{{range .PaperList}}{id:"{{.ID}}",isRef:{{.IsRef}},isCiting:{{.IsCiting}},cached:{{.Cached}}},{{end}}
	];

	let graphInitialized = false;
	let zoom;

	function highlightNode(id, highlight) {
		if (!nodes || !links) return;
		nodes.classed('selected', d => d.id === id && highlight);
		links.classed('highlighted', d => (d.source.id === id || d.target.id === id) && highlight);
	}

	function setupRefListHovers() {
		document.querySelectorAll('.refs li').forEach(el => {
			el.addEventListener('mouseenter', () => highlightNode(el.dataset.id, true));
			el.addEventListener('mouseleave', () => highlightNode(el.dataset.id, false));
		});
	}

	// Initialize reference list hovers
	setupRefListHovers();

	function getNodeColor(d) {
		if (d.id === paperID) return '#1d4ed8';
		if (!d.cached) return '#94a3b8';
		const item = paperList.find(p => p.id === d.id);
		if (item && item.isCiting) return '#10b981';
		return '#334155';
	}

	function refreshGraph() {
		return fetch('/paper/' + paperID + '/graph')
			.then(r => r.json())
			.then(data => {
				if (!data.nodes || data.nodes.length === 0) {
					if (!graphInitialized) {
						container.innerHTML = '<p style="padding:2rem;color:#64748b;text-align:center;">No citation data available.</p>';
					}
					return;
				}

				const width = container.clientWidth;
				const height = container.clientHeight;

				// First time: create SVG and setup
				if (!graphInitialized) {
					// Remove placeholder if present
					const placeholder = container.querySelector('p');
					if (placeholder) placeholder.remove();

					svg = d3.select('#graph-container')
						.append('svg')
						.attr('width', width)
						.attr('height', height);

					g = svg.append('g');

					zoom = d3.zoom()
						.scaleExtent([0.2, 4])
						.on('zoom', e => g.attr('transform', e.transform));
					svg.call(zoom);

					svg.append('defs').append('marker')
						.attr('id', 'arrow')
						.attr('viewBox', '0 -5 10 10')
						.attr('refX', 20)
						.attr('markerWidth', 6)
						.attr('markerHeight', 6)
						.attr('orient', 'auto')
						.append('path')
						.attr('d', 'M0,-5L10,0L0,5')
						.attr('fill', '#94a3b8');

					// Create group elements for links and nodes
					g.append('g').attr('class', 'links-group');
					g.append('g').attr('class', 'nodes-group');

					document.getElementById('reset-btn').addEventListener('click', () => {
						svg.transition().duration(500).call(zoom.transform, d3.zoomIdentity);
						if (simulation) simulation.alpha(0.3).restart();
					});

					document.getElementById('fullscreen-btn').addEventListener('click', () => {
						container.classList.toggle('fullscreen');
						const btn = document.getElementById('fullscreen-btn');
						btn.textContent = container.classList.contains('fullscreen') ? 'Exit' : 'Fullscreen';
						svg.attr('width', container.clientWidth).attr('height', container.clientHeight);
						if (simulation) {
							simulation.force('center', d3.forceCenter(container.clientWidth / 2, container.clientHeight / 2));
							simulation.alpha(0.3).restart();
						}
					});

					document.addEventListener('keydown', e => {
						if (e.key === 'Escape' && container.classList.contains('fullscreen')) {
							container.classList.remove('fullscreen');
							document.getElementById('fullscreen-btn').textContent = 'Fullscreen';
							svg.attr('width', container.clientWidth).attr('height', container.clientHeight);
							if (simulation) {
								simulation.force('center', d3.forceCenter(container.clientWidth / 2, container.clientHeight / 2));
								simulation.alpha(0.3).restart();
							}
						}
					});

					graphInitialized = true;
				}

				// Update scales based on new data
				const maxCitations = Math.max(1, ...data.nodes.map(n => n.citations));
				const radiusScale = d3.scaleSqrt().domain([0, maxCitations]).range([5, 18]);

				// Stop existing simulation
				if (simulation) simulation.stop();

				// Create new simulation
				simulation = d3.forceSimulation(data.nodes)
					.force('link', d3.forceLink(data.edges).id(d => d.id).distance(80))
					.force('charge', d3.forceManyBody().strength(-250))
					.force('center', d3.forceCenter(width / 2, height / 2))
					.force('collision', d3.forceCollide().radius(d => radiusScale(d.citations) + 4));

				// Update links with D3 data join
				links = g.select('.links-group')
					.selectAll('line')
					.data(data.edges, d => d.source.id ? d.source.id + '->' + d.target.id : d.source + '->' + d.target)
					.join(
						enter => enter.append('line')
							.attr('class', 'graph-link')
							.attr('marker-end', 'url(#arrow)')
							.style('opacity', 0)
							.call(el => el.transition().duration(300).style('opacity', 1)),
						update => update,
						exit => exit.transition().duration(200).style('opacity', 0).remove()
					);

				// Update nodes with D3 data join
				nodes = g.select('.nodes-group')
					.selectAll('circle')
					.data(data.nodes, d => d.id)
					.join(
						enter => enter.append('circle')
							.attr('class', 'graph-node')
							.attr('r', 0)
							.attr('fill', getNodeColor)
							.call(el => el.transition().duration(300).attr('r', d => d.id === paperID ? 12 : radiusScale(d.citations))),
						update => update
							.call(el => el.transition().duration(300)
								.attr('r', d => d.id === paperID ? 12 : radiusScale(d.citations))
								.attr('fill', getNodeColor)),
						exit => exit.transition().duration(200).attr('r', 0).remove()
					);

				// Re-apply drag behavior
				nodes.call(d3.drag()
					.on('start', (e) => { if (!e.active) simulation.alphaTarget(0.3).restart(); e.subject.fx = e.subject.x; e.subject.fy = e.subject.y; })
					.on('drag', (e) => { e.subject.fx = e.x; e.subject.fy = e.y; })
					.on('end', (e) => { if (!e.active) simulation.alphaTarget(0); e.subject.fx = null; e.subject.fy = null; }));

				// Re-apply event handlers
				nodes.on('click', (e, d) => {
					const url = d.cached ? '/paper/' + d.id : '/paper/' + d.id + '/fetch';
					navigateWithTransition(e.target, url);
				});

				nodes.on('mouseover', (e, d) => {
					tooltip.style.display = 'block';
					tooltip.innerHTML = '<strong>' + d.id + '</strong><br>' + d.title + '<br>' + d.year + ' · ' + d.citations + ' citations';
					highlightNode(d.id, true);
					document.querySelectorAll('.refs li[data-id="' + d.id + '"]').forEach(el => el.style.background = '#eff6ff');
				});

				nodes.on('mousemove', e => {
					tooltip.style.left = (e.clientX + 12) + 'px';
					tooltip.style.top = (e.clientY + 12) + 'px';
				});

				nodes.on('mouseout', (e, d) => {
					tooltip.style.display = 'none';
					highlightNode(d.id, false);
					document.querySelectorAll('.refs li[data-id="' + d.id + '"]').forEach(el => el.style.background = '');
				});

				simulation.on('tick', () => {
					links.attr('x1', d => d.source.x).attr('y1', d => d.source.y)
						 .attr('x2', d => d.target.x).attr('y2', d => d.target.y);
					nodes.attr('cx', d => d.x).attr('cy', d => d.y);
				});
			});
	}

	// Refresh references list from server
	function refreshRefsList() {
		return fetch('/paper/' + paperID + '/refs')
			.then(r => r.json())
			.then(refs => {
				// Update paperList for graph coloring
				paperList = refs.map(r => ({
					id: r.id,
					isRef: true,
					isCiting: false,
					cached: r.hasTitle
				}));

				const refsList = document.getElementById('refs-list');
				if (!refsList) return;

				// Clear and rebuild refs list
				refsList.innerHTML = '';
				refs.forEach(r => {
					const li = document.createElement('li');
					li.dataset.id = r.id;
					const dateStr = arxivIDToDate(r.id);
					const datePart = dateStr ? ' <span class="ref-date">(' + dateStr + ')</span>' : '';
					if (r.hasTitle) {
						li.innerHTML = '<span class="ref-id">' + r.id + '</span> <a class="ref-title" href="/paper/' + r.id + '">' + escapeHtml(r.title) + '</a>' + datePart + (r.citedBy ? ' <span class="ref-meta">' + r.citedBy + ' cites</span>' : '');
					} else {
						li.innerHTML = '<span class="ref-id">' + r.id + '</span> <span class="ref-uncached">' + r.id + '</span>' + datePart + ' <a href="/paper/' + r.id + '/fetch">[fetch]</a>';
					}
					refsList.appendChild(li);
				});

				// Re-setup click handlers and hovers
				setupRefListHovers();
				setupRefListClicks();
			});
	}

	function escapeHtml(s) {
		const div = document.createElement('div');
		div.textContent = s;
		return div.innerHTML;
	}

	// Parse arXiv ID to date string (e.g., "2302.13971" -> "Feb 2023")
	function arxivIDToDate(id) {
		const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
		let yymm = '';
		// Old format: category/YYMMNNN
		const slashIdx = id.indexOf('/');
		if (slashIdx >= 0) {
			const num = id.substring(slashIdx + 1);
			if (num.length >= 4) yymm = num.substring(0, 4);
		} else {
			// New format: YYMM.NNNNN
			const dotIdx = id.indexOf('.');
			if (dotIdx === 4) yymm = id.substring(0, 4);
		}
		if (yymm.length !== 4) return '';
		const yy = parseInt(yymm.substring(0, 2), 10);
		const mm = parseInt(yymm.substring(2, 4), 10);
		if (isNaN(yy) || isNaN(mm) || mm < 1 || mm > 12) return '';
		const year = yy >= 91 ? 1900 + yy : 2000 + yy;
		return months[mm - 1] + ' ' + year;
	}

	function setupRefListClicks() {
		document.querySelectorAll('.refs li').forEach(li => {
			const id = li.dataset.id;
			if (!id) return;
			li.addEventListener('click', (e) => {
				if (e.target.tagName === 'A') return;
				const link = li.querySelector('a.ref-title');
				const url = link ? '/paper/' + id : '/paper/' + id + '/fetch';
				navigateWithTransition(li, url);
			});
		});
	}

	// Initial graph load
	refreshGraph();

	{{if .UncachedCount}}
	const prefetchPoller = setInterval(() => {
		fetch('/paper/' + paperID + '/refs')
			.then(r => r.json())
			.then(refs => {
				let uncached = 0;
				refs.forEach(r => {
					if (!r.hasTitle) { uncached++; return; }
					const li = document.querySelector('#refs-list li[data-id="' + r.id + '"]');
					if (li && !li.querySelector('a.ref-title')) {
						const dateStr = arxivIDToDate(r.id);
						const datePart = dateStr ? ' <span class="ref-date">(' + dateStr + ')</span>' : '';
						li.innerHTML = '<span class="ref-id">' + r.id + '</span> <a class="ref-title" href="/paper/' + r.id + '">' + escapeHtml(r.title) + '</a>' + datePart + (r.citedBy ? ' <span class="ref-meta">' + r.citedBy + ' cites</span>' : '');
					}
				});
				if (uncached === 0) {
					clearInterval(prefetchPoller);
					const status = document.getElementById('prefetch-status');
					if (status) status.remove();
				}
			});
	}, 2000);
	{{end}}

	{{if .FetchingSource}}
	// Poll for source download completion
	const sourcePoller = setInterval(() => {
		fetch('/paper/' + paperID + '/status')
			.then(r => r.json())
			.then(status => {
				if (status.sourceDownloaded) {
					clearInterval(sourcePoller);
					// Remove the loading indicator
					const sourceStatus = document.getElementById('source-status');
					if (sourceStatus) sourceStatus.remove();
					// Refresh the graph and references list
					refreshRefsList().then(() => refreshGraph());
				}
			});
	}, 2000);
	{{end}}
})();
</script>
</body>
</html>
{{end}}

{{define "author"}}
{{template "head" .}}
<h1>Papers by {{.Author}}</h1>
<p>{{len .Papers}} papers found</p>
{{range .Papers}}
<div class="paper">
	<span class="paper-id">{{.ID}}</span>
	{{if .SourceDownloaded}}<span class="badge badge-src">src</span>{{end}}
	{{if .PDFDownloaded}}<span class="badge badge-pdf">pdf</span>{{end}}
	<div class="paper-title"><a href="/paper/{{.ID}}">{{.Title}}</a></div>
	<div class="paper-authors">{{.Authors}}</div>
	<div class="paper-categories">{{range $i, $c := parseCategories .Categories}}{{if $i}} {{end}}<a class="cat-link" href="/category/{{$c}}">{{$c}}</a>{{end}}</div>
</div>
{{else}}
<p>No papers found for this author.</p>
{{end}}
{{template "foot" .}}
{{end}}

{{define "category"}}
{{template "head" .}}
<h1>Category: {{.Category}}</h1>
<p>{{len .Papers}} papers</p>
{{range .Papers}}
<div class="paper">
	<span class="paper-id">{{.ID}}</span>
	{{if .SourceDownloaded}}<span class="badge badge-src">src</span>{{end}}
	{{if .PDFDownloaded}}<span class="badge badge-pdf">pdf</span>{{end}}
	<div class="paper-title"><a href="/paper/{{.ID}}">{{.Title}}</a></div>
	<div class="paper-authors">{{.Authors}}</div>
	<div class="paper-categories">{{range $i, $c := parseCategories .Categories}}{{if $i}} {{end}}<a class="cat-link" href="/category/{{$c}}">{{$c}}</a>{{end}}</div>
</div>
{{else}}
<p>No papers in this category.</p>
{{end}}
{{template "foot" .}}
{{end}}

{{define "categories"}}
{{template "head" .}}
<h1>Categories</h1>
<p>{{len .Categories}} categories</p>
<div class="categories-list">
{{range .Categories}}
<div class="cat-item">
	<a href="/category/{{.Name}}">{{.Name}}</a>
	<span class="cat-count">({{.Count}})</span>
</div>
{{end}}
</div>
{{template "foot" .}}
{{end}}

`))

func cmdServe(ctx context.Context, cacheDir string, args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8080, "Port to listen on")
	fs.Parse(args)

	cache, err := arxiv.Open(cacheDir)
	if err != nil {
		log.Fatalf("open cache: %v", err)
	}

	srv := &server{cache: cache, cacheDir: cacheDir}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/search", srv.handleSearch)
	mux.HandleFunc("/paper/", srv.handlePaper)
	mux.HandleFunc("/author/", srv.handleAuthor)
	mux.HandleFunc("/category/", srv.handleCategory)
	mux.HandleFunc("/categories", srv.handleCategories)
	mux.HandleFunc("/src/", srv.handleSource)
	mux.HandleFunc("/pdf/", srv.handlePDF)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting server at http://localhost%s", addr)

	httpServer := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		httpServer.Shutdown(context.Background())
	}()

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

type server struct {
	cache    *arxiv.Cache
	cacheDir string
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	stats, err := s.cache.Stats(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	papers, err := s.cache.ListPapers(ctx, "", 0, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":  "Home",
		"Stats":  stats,
		"Papers": papers,
		"Query":  "",
	}
	templates.ExecuteTemplate(w, "index", data)
}

func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	ctx := r.Context()
	papers, err := s.cache.Search(ctx, query, "", 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// JSON format for live search
	if r.URL.Query().Get("format") == "json" {
		type searchResult struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Authors    string `json:"authors"`
			Categories string `json:"categories"`
			Src        bool   `json:"src"`
			PDF        bool   `json:"pdf"`
		}
		results := make([]searchResult, len(papers))
		for i, p := range papers {
			results[i] = searchResult{
				ID:         p.ID,
				Title:      p.Title,
				Authors:    p.Authors,
				Categories: p.Categories,
				Src:        p.SourceDownloaded,
				PDF:        p.PDFDownloaded,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
		return
	}

	data := map[string]any{
		"Title":  "Search",
		"Query":  query,
		"Papers": papers,
	}
	templates.ExecuteTemplate(w, "search", data)
}

type refInfo struct {
	ID        string
	Title     string
	HasTitle  bool // Has metadata (title available)
	HasSource bool // Has full source downloaded
	CitedBy   int
}

func (s *server) handlePaper(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/paper/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	// Handle /paper/:id/fetch - fetch paper on demand
	if strings.HasSuffix(path, "/fetch") {
		paperID := strings.TrimSuffix(path, "/fetch")

		// Fetch metadata and source
		opts := &arxiv.DownloadOptions{DownloadPDF: false, DownloadSource: true}
		_, err := s.cache.FetchAndDownload(ctx, paperID, opts)
		if err != nil {
			http.Error(w, "failed to fetch paper: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Redirect to paper page
		http.Redirect(w, r, "/paper/"+paperID, http.StatusSeeOther)
		return
	}

	// Handle /paper/:id/graph - return citation graph JSON
	if strings.HasSuffix(path, "/graph") {
		paperID := strings.TrimSuffix(path, "/graph")
		graph, err := s.cache.GetCitationGraph(ctx, paperID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(graph)
		return
	}

	// Handle /paper/:id/prefetch-refs - prefetch reference titles
	if strings.HasSuffix(path, "/prefetch-refs") {
		paperID := strings.TrimSuffix(path, "/prefetch-refs")
		if r.Method == http.MethodPost {
			// Synchronous prefetch
			err := s.cache.PrefetchReferenceTitles(ctx, paperID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/paper/"+paperID, http.StatusSeeOther)
			return
		}
		// GET returns status of uncached references
		uncached, _ := s.cache.UncachedReferenceCount(ctx, paperID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"uncached": uncached})
		return
	}

	// Handle /paper/:id/fetch-source - fetch source and extract citations
	if strings.HasSuffix(path, "/fetch-source") {
		paperID := strings.TrimSuffix(path, "/fetch-source")
		if r.Method == http.MethodPost {
			// Download source only (not PDF)
			opts := &arxiv.DownloadOptions{DownloadPDF: false, DownloadSource: true}
			if err := s.cache.DownloadPaper(ctx, paperID, opts); err != nil {
				http.Error(w, "failed to fetch source: "+err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/paper/"+paperID, http.StatusSeeOther)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle /paper/:id/status - return paper status JSON (for polling)
	if strings.HasSuffix(path, "/status") {
		paperID := strings.TrimSuffix(path, "/status")
		paper, err := s.cache.GetPaper(ctx, paperID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		refs, _ := s.cache.References(ctx, paperID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sourceDownloaded": paper.SourceDownloaded,
			"hasReferences":    len(refs) > 0,
			"refCount":         len(refs),
		})
		return
	}

	// Handle /paper/:id/refs - return references JSON (for live updates)
	if strings.HasSuffix(path, "/refs") {
		paperID := strings.TrimSuffix(path, "/refs")
		dbRefs, err := s.cache.References(ctx, paperID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type refJSON struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			HasTitle  bool   `json:"hasTitle"`
			HasSource bool   `json:"hasSource"`
			CitedBy   int    `json:"citedBy"`
		}
		refs := make([]refJSON, len(dbRefs))
		for i, r := range dbRefs {
			refs[i] = refJSON{
				ID:        r.ID,
				Title:     r.Title,
				HasTitle:  r.HasTitle,
				HasSource: r.HasSource,
			}
			if r.HasTitle {
				refs[i].CitedBy, _ = s.cache.CitedByCount(ctx, r.ID)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(refs)
		return
	}

	id := path
	paper, err := s.cache.GetPaper(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Get citation count for this paper
	citedByCount, _ := s.cache.CitedByCount(ctx, id)

	var files []string
	if paper.SourceDownloaded && paper.SourcePath != "" {
		filepath.WalkDir(paper.SourcePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(paper.SourcePath, path)
			files = append(files, rel)
			return nil
		})
	}

	// Get paper list for sidebar
	paperList, _ := s.cache.GetPaperList(ctx, id)

	// Count uncached references
	uncachedCount := 0
	for _, p := range paperList {
		if !p.Cached && p.IsRef {
			uncachedCount++
		}
	}

	// Start background prefetch if there are uncached references
	if uncachedCount > 0 {
		go func() {
			bgCtx := context.Background()
			s.cache.PrefetchReferenceTitles(bgCtx, id)
		}()
	}

	// Auto-fetch source in background if not downloaded
	fetchingSource := false
	if !paper.SourceDownloaded {
		fetchingSource = true
		go func() {
			bgCtx := context.Background()
			opts := &arxiv.DownloadOptions{DownloadPDF: false, DownloadSource: true}
			s.cache.DownloadPaper(bgCtx, id, opts)
		}()
	}

	data := map[string]any{
		"Title":          paper.Title,
		"Paper":          paper,
		"Files":          files,
		"PaperList":      paperList,
		"UncachedCount":  uncachedCount,
		"CitedByCount":   citedByCount,
		"FetchingSource": fetchingSource,
	}
	templates.ExecuteTemplate(w, "paper", data)
}

func (s *server) handleAuthor(w http.ResponseWriter, r *http.Request) {
	author := strings.TrimPrefix(r.URL.Path, "/author/")
	if author == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	papers, err := s.cache.SearchByAuthor(ctx, author, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":  "Author: " + author,
		"Author": author,
		"Papers": papers,
	}
	templates.ExecuteTemplate(w, "author", data)
}

func (s *server) handlePDF(w http.ResponseWriter, r *http.Request) {
	// Routes: GET /pdf/{id} - serve PDF, POST /pdf/{id}/fetch - fetch PDF
	path := strings.TrimPrefix(r.URL.Path, "/pdf/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()

	// Check if this is a fetch request
	if strings.HasSuffix(path, "/fetch") {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		paperID := strings.TrimSuffix(path, "/fetch")
		returnTo := r.URL.Query().Get("return")

		// First ensure paper metadata exists (fetch if needed)
		paper, err := s.cache.Fetch(ctx, paperID)
		if err != nil {
			http.Error(w, "failed to fetch paper: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Download PDF
		opts := &arxiv.DownloadOptions{DownloadPDF: true, DownloadSource: false}
		if err := s.cache.DownloadPaper(ctx, paper.ID, opts); err != nil {
			http.Error(w, "failed to download PDF: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Redirect back
		if returnTo != "" {
			http.Redirect(w, r, "/paper/"+returnTo, http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/pdf/"+paperID, http.StatusSeeOther)
		}
		return
	}

	// Serve PDF: GET /pdf/{id}
	paperID := path
	paper, err := s.cache.GetPaper(ctx, paperID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if !paper.PDFDownloaded || paper.PDFPath == "" {
		http.Error(w, "PDF not cached", http.StatusNotFound)
		return
	}

	// Verify file exists
	if _, err := os.Stat(paper.PDFPath); err != nil {
		http.Error(w, "PDF file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", paperID+".pdf"))
	http.ServeFile(w, r, paper.PDFPath)
}

func (s *server) handleSource(w http.ResponseWriter, r *http.Request) {
	// Path format: /src/{paperID}/{filepath...}
	path := strings.TrimPrefix(r.URL.Path, "/src/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	paperID := parts[0]
	filePath := parts[1]

	ctx := r.Context()
	paper, err := s.cache.GetPaper(ctx, paperID)
	if err != nil || !paper.SourceDownloaded || paper.SourcePath == "" {
		http.NotFound(w, r)
		return
	}

	// Security: ensure the requested path is within the source directory
	fullPath := filepath.Join(paper.SourcePath, filePath)
	fullPath = filepath.Clean(fullPath)
	if !strings.HasPrefix(fullPath, paper.SourcePath) {
		http.NotFound(w, r)
		return
	}

	// Check file exists
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, fullPath)
}

// parseAuthors splits an author string into individual author names.
// arXiv author format varies but is typically comma-separated or "and"-separated.
func parseAuthors(authors string) []string {
	// Replace " and " with comma for consistent splitting
	authors = strings.ReplaceAll(authors, " and ", ", ")

	var result []string
	for _, a := range strings.Split(authors, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			result = append(result, a)
		}
	}
	return result
}

// parseCategories splits a space-separated category string.
func parseCategories(categories string) []string {
	return strings.Fields(categories)
}

// arxivIDToDate parses an arXiv ID and returns a date string like "Feb 2023".
// New format: YYMM.NNNNN (e.g., 2302.13971 -> Feb 2023)
// Old format: category/YYMMNNN (e.g., hep-th/9901001 -> Jan 1999)
func arxivIDToDate(id string) string {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

	// Check for old format: category/YYMMNNN
	if idx := strings.Index(id, "/"); idx >= 0 {
		numPart := id[idx+1:]
		if len(numPart) >= 4 {
			yy := numPart[0:2]
			mm := numPart[2:4]
			year, month := parseYYMM(yy, mm)
			if month >= 1 && month <= 12 {
				return months[month-1] + " " + fmt.Sprintf("%d", year)
			}
		}
		return ""
	}

	// New format: YYMM.NNNNN or YYMM.NNNNNN
	if idx := strings.Index(id, "."); idx >= 0 {
		yymm := id[:idx]
		if len(yymm) == 4 {
			yy := yymm[0:2]
			mm := yymm[2:4]
			year, month := parseYYMM(yy, mm)
			if month >= 1 && month <= 12 {
				return months[month-1] + " " + fmt.Sprintf("%d", year)
			}
		}
	}

	return ""
}

func parseYYMM(yy, mm string) (year, month int) {
	// Parse year: 91-99 -> 1991-1999, 00-90 -> 2000-2090
	if len(yy) == 2 && yy[0] >= '0' && yy[0] <= '9' && yy[1] >= '0' && yy[1] <= '9' {
		y := int(yy[0]-'0')*10 + int(yy[1]-'0')
		if y >= 91 {
			year = 1900 + y
		} else {
			year = 2000 + y
		}
	}
	// Parse month
	if len(mm) == 2 && mm[0] >= '0' && mm[0] <= '1' && mm[1] >= '0' && mm[1] <= '9' {
		month = int(mm[0]-'0')*10 + int(mm[1]-'0')
	}
	return
}

func (s *server) handleCategory(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimPrefix(r.URL.Path, "/category/")
	if category == "" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	papers, err := s.cache.ListPapers(ctx, category, 0, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":    "Category: " + category,
		"Category": category,
		"Papers":   papers,
	}
	templates.ExecuteTemplate(w, "category", data)
}

func (s *server) handleCategories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	categories, err := s.cache.ListCategories(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Title":      "Categories",
		"Categories": categories,
	}
	templates.ExecuteTemplate(w, "categories", data)
}
