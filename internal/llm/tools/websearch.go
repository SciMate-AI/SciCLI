package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/SciMate-AI/scicli/internal/permission"
)

const (
	WebSearchToolName = "web_search"

	webSearchDescription = `Search the web for information using DuckDuckGo and return the top results.

WHEN TO USE:
- When you need to find information that is not in the local codebase
- When you need current events, documentation, or general knowledge
- When academic APIs (arXiv, Semantic Scholar) don't have what you need

HOW TO USE:
- Provide a natural-language query string
- Optionally set max_results (default 10, max 20)
- Results include: title, URL, and a short snippet

NOTES:
- Uses DuckDuckGo (no API key required)
- After getting URLs from this tool, use the fetch tool to read full page content
- For academic papers, prefer the arXiv or Semantic Scholar APIs directly`
)

type WebSearchParams struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

type webSearchTool struct {
	client      *http.Client
	permissions permission.Service
}

func NewWebSearchTool(permissions permission.Service) BaseTool {
	return &webSearchTool{
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
		permissions: permissions,
	}
}

func (t *webSearchTool) Info() ToolInfo {
	return ToolInfo{
		Name:        WebSearchToolName,
		Description: webSearchDescription,
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query",
			},
			"max_results": map[string]any{
				"type":        "number",
				"description": "Maximum number of results to return (default 10, max 20)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *webSearchTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params WebSearchParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse("Failed to parse parameters: " + err.Error()), nil
	}
	if strings.TrimSpace(params.Query) == "" {
		return NewTextErrorResponse("query is required"), nil
	}
	if params.MaxResults <= 0 || params.MaxResults > 20 {
		params.MaxResults = 10
	}

	sessionID, _ := GetContextValues(ctx)
	if t.permissions != nil {
		p := t.permissions.Request(permission.CreatePermissionRequest{
			SessionID:   sessionID,
			ToolName:    WebSearchToolName,
			Action:      "web_search",
			Description: fmt.Sprintf("Search the web for: %s", params.Query),
			Params:      params,
		})
		if !p {
			return ToolResponse{}, permission.ErrorPermissionDenied
		}
	}

	results, err := t.search(ctx, params.Query, params.MaxResults)
	if err != nil {
		return NewTextErrorResponse("Search failed: " + err.Error()), nil
	}
	if len(results) == 0 {
		return NewTextResponse(fmt.Sprintf("No results found for: %s", params.Query)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Web search results for: %s\n\n", params.Query)
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n   URL: %s\n   %s\n\n", i+1, r.Title, r.URL, r.Snippet)
	}
	return NewTextResponse(sb.String()), nil
}

type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

// search fetches DuckDuckGo Lite HTML results and parses them.
// DuckDuckGo Lite (https://lite.duckduckgo.com/lite/) returns clean HTML
// with no JavaScript requirements — reliable for scraping.
func (t *webSearchTool) search(ctx context.Context, query string, maxResults int) ([]searchResult, error) {
	searchURL := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	// DuckDuckGo blocks requests without a recognisable User-Agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; scicli-research/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return parseDDGLite(string(body), maxResults)
}

// parseDDGLite extracts search results from DuckDuckGo Lite HTML.
// The page structure uses a plain <table> with result rows.
// DDG Lite wraps result URLs in a redirect: //duckduckgo.com/l/?uddg=ENCODED_URL
// We decode the uddg parameter to get the real destination URL.
func parseDDGLite(html string, maxResults int) ([]searchResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	var results []searchResult

	doc.Find("a.result-link").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if len(results) >= maxResults {
			return false
		}
		href, exists := s.Attr("href")
		if !exists || strings.TrimSpace(href) == "" {
			return true
		}

		// Decode the real URL from DDG's redirect wrapper.
		realURL := decodeDDGRedirect(href)
		if realURL == "" {
			return true
		}

		title := strings.TrimSpace(s.Text())
		snippet := strings.TrimSpace(
			s.Closest("tr").Next().Find(".result-snippet").Text(),
		)

		results = append(results, searchResult{
			Title:   title,
			URL:     realURL,
			Snippet: snippet,
		})
		return true
	})

	return results, nil
}

// decodeDDGRedirect extracts the real destination URL from a DDG redirect href.
// Input example: //duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&rut=...
// Returns the decoded uddg value, or empty string if parsing fails.
func decodeDDGRedirect(href string) string {
	if !strings.HasPrefix(href, "//") {
		return href // already a direct URL
	}
	full := "https:" + href
	parsed, err := url.Parse(full)
	if err != nil {
		return ""
	}
	uddg := parsed.Query().Get("uddg")
	if uddg == "" {
		return ""
	}
	return uddg
}
