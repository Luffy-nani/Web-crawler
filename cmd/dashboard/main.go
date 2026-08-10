package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"webcrawler/internal/analyzer"
	"webcrawler/internal/embedder"
	"webcrawler/internal/search"
	"webcrawler/internal/store"
)

const (
	addr               = ":8080"
	dbPath             = "crawler.db"
	recentChangesLimit = 20
	searchTopK         = 5
)

type searchResult struct {
	Answer  string
	Sources []string
}

type questionAsker func(question string) (searchResult, error)

type dashboardApp struct {
	db           *store.Store
	ask          questionAsker
	overviewTmpl *template.Template
	searchTmpl   *template.Template
}

type overviewPageData struct {
	Pages   []store.TrackedPage
	Changes []store.Change
}

type searchPageData struct {
	Question string
	Answer   string
	Sources  []string
	Error    string
}

func main() {
	embed, err := embedder.New()
	if err != nil {
		log.Fatalf("failed to init embedder: %v", err)
	}
	analyze := analyzer.New()

	db, err := store.New(dbPath)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	app, err := newDashboardApp(db, buildSearchPipeline(embed, analyze, db))
	if err != nil {
		log.Fatalf("failed to init dashboard: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.overviewHandler)
	mux.HandleFunc("/search", app.searchHandler)

	log.Printf("dashboard available at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("dashboard server error: %v", err)
	}
}

func newDashboardApp(db *store.Store, ask questionAsker) (*dashboardApp, error) {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format(time.RFC3339)
		},
		"statusText": func(p store.TrackedPage) string {
			if p.Err != "" {
				return "error: " + p.Err
			}
			return fmt.Sprintf("%d", p.StatusCode)
		},
	}

	overviewTmpl, err := template.New("overview").Funcs(funcs).Parse(overviewTemplate)
	if err != nil {
		return nil, err
	}
	searchTmpl, err := template.New("search").Funcs(funcs).Parse(searchTemplate)
	if err != nil {
		return nil, err
	}

	return &dashboardApp{
		db:           db,
		ask:          ask,
		overviewTmpl: overviewTmpl,
		searchTmpl:   searchTmpl,
	}, nil
}

func (a *dashboardApp) overviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	pages, err := a.db.GetTrackedPages()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to load pages: %v", err), http.StatusInternalServerError)
		return
	}
	changes, err := a.db.GetRecentChanges(recentChangesLimit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to load changes: %v", err), http.StatusInternalServerError)
		return
	}

	data := overviewPageData{
		Pages:   pages,
		Changes: changes,
	}
	if err := a.overviewTmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("failed to render page: %v", err), http.StatusInternalServerError)
		return
	}
}

func (a *dashboardApp) searchHandler(w http.ResponseWriter, r *http.Request) {
	question := strings.TrimSpace(r.FormValue("q"))
	data := searchPageData{Question: question}

	if question != "" {
		result, err := a.ask(question)
		if err != nil {
			data.Error = err.Error()
		} else {
			data.Answer = result.Answer
			data.Sources = result.Sources
		}
	}

	if err := a.searchTmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("failed to render page: %v", err), http.StatusInternalServerError)
		return
	}
}

func buildSearchPipeline(embed *embedder.Embedder, analyze *analyzer.Analyzer, db *store.Store) questionAsker {
	return func(question string) (searchResult, error) {
		results, err := search.TopK(question, searchTopK, embed, db)
		if err != nil {
			return searchResult{}, err
		}
		if len(results) == 0 {
			return searchResult{Answer: "No results found."}, nil
		}

		context := make([]string, 0, len(results))
		sources := make([]string, 0, len(results))
		seen := make(map[string]bool, len(results))
		for _, result := range results {
			context = append(context, fmt.Sprintf("[%s]\n%s", result.Chunk.URL, result.Chunk.Text))
			if !seen[result.Chunk.URL] {
				seen[result.Chunk.URL] = true
				sources = append(sources, result.Chunk.URL)
			}
		}

		answer, err := analyze.Answer(question, context)
		if err != nil {
			return searchResult{}, err
		}

		return searchResult{
			Answer:  answer,
			Sources: sources,
		}, nil
	}
}

const overviewTemplate = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Argus Dashboard</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; max-width: 960px; }
    table { border-collapse: collapse; width: 100%; margin-bottom: 2rem; }
    th, td { border: 1px solid #ddd; padding: 0.5rem; text-align: left; vertical-align: top; }
    th { background: #f6f6f6; }
    ul { padding-left: 1.25rem; }
  </style>
</head>
<body>
  <h1>Argus Crawl Overview</h1>
  <p><a href="/search">Ask a question</a></p>

  <h2>Tracked URLs</h2>
  <table>
    <thead>
      <tr><th>URL</th><th>Last Fetch</th><th>Status</th></tr>
    </thead>
    <tbody>
      {{range .Pages}}
      <tr>
        <td><a href="{{.URL}}">{{.URL}}</a></td>
        <td>{{formatTime .FetchedAt}}</td>
        <td>{{statusText .}}</td>
      </tr>
      {{else}}
      <tr><td colspan="3">No pages crawled yet.</td></tr>
      {{end}}
    </tbody>
  </table>

  <h2>Recent Changes</h2>
  <ul>
    {{range .Changes}}
    <li><strong>{{.URL}}</strong> — {{.Summary}} <em>({{formatTime .DetectedAt}})</em></li>
    {{else}}
    <li>No detected changes yet.</li>
    {{end}}
  </ul>
</body>
</html>
`

const searchTemplate = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Argus Search</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; max-width: 960px; }
    input[type="text"] { width: 100%; max-width: 700px; padding: 0.5rem; }
    button { margin-top: 0.5rem; padding: 0.5rem 1rem; }
    .error { color: #b00020; }
  </style>
</head>
<body>
  <h1>Ask Argus</h1>
  <p><a href="/">Back to overview</a></p>
  <form method="get" action="/search">
    <label for="q">Question</label><br>
    <input id="q" name="q" type="text" value="{{.Question}}">
    <br>
    <button type="submit">Search</button>
  </form>

  {{if .Error}}
  <p class="error">Search failed: {{.Error}}</p>
  {{end}}

  {{if .Answer}}
  <h2>Answer</h2>
  <p>{{.Answer}}</p>
  {{end}}

  {{if .Sources}}
  <h2>Sources</h2>
  <ul>
    {{range .Sources}}
    <li><a href="{{.}}">{{.}}</a></li>
    {{end}}
  </ul>
  {{end}}
</body>
</html>
`
