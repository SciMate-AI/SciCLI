package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeDDGRedirect(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "//duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.example.com%2Fpage&rut=abc123",
			want:  "https://www.example.com/page",
		},
		{
			input: "https://already-direct.com",
			want:  "https://already-direct.com",
		},
		{
			input: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		got := decodeDDGRedirect(tc.input)
		assert.Equal(t, tc.want, got, "input: %s", tc.input)
	}
}

func TestParseDDGLiteExtractsResults(t *testing.T) {
	// Minimal DDG Lite-style HTML fixture.
	html := `<html><body><table>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage1">Result One</a></td></tr>
<tr><td class="result-snippet">Snippet for result one.</td></tr>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fpage2">Result Two</a></td></tr>
<tr><td class="result-snippet">Snippet for result two.</td></tr>
</table></body></html>`

	results, err := parseDDGLite(html, 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, "Result One", results[0].Title)
	assert.Equal(t, "https://example.com/page1", results[0].URL)
	assert.True(t, strings.Contains(results[0].Snippet, "result one"))

	assert.Equal(t, "Result Two", results[1].Title)
	assert.Equal(t, "https://example.com/page2", results[1].URL)
}

func TestParseDDGLiteRespectsMaxResults(t *testing.T) {
	html := `<html><body><table>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fa.com">A</a></td></tr>
<tr><td class="result-snippet">a</td></tr>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fb.com">B</a></td></tr>
<tr><td class="result-snippet">b</td></tr>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fc.com">C</a></td></tr>
<tr><td class="result-snippet">c</td></tr>
</table></body></html>`

	results, err := parseDDGLite(html, 2)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}
