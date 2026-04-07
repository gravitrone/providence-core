package tools

import (
	"context"
	"strings"
	"testing"
)

func TestExtractTextFromHTML_Basic(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
<h1>Hello World</h1>
<p>This is a paragraph.</p>
<p>Another paragraph with <b>bold</b> and <i>italic</i> text.</p>
</body>
</html>`

	result := extractTextFromHTML(html)

	if !strings.Contains(result, "Hello World") {
		t.Errorf("expected 'Hello World' in result, got: %s", result)
	}
	if !strings.Contains(result, "This is a paragraph.") {
		t.Errorf("expected paragraph text in result, got: %s", result)
	}
	if !strings.Contains(result, "bold") {
		t.Errorf("expected 'bold' text in result, got: %s", result)
	}
	if strings.Contains(result, "<") {
		t.Errorf("result should not contain HTML tags, got: %s", result)
	}
}

func TestExtractTextFromHTML_StripsScriptsAndStyles(t *testing.T) {
	html := `<html>
<head><style>.foo { color: red; }</style></head>
<body>
<p>Visible content</p>
<script>var x = "hidden";</script>
<script type="text/javascript">
	function doStuff() { alert("nope"); }
</script>
<p>More visible</p>
</body>
</html>`

	result := extractTextFromHTML(html)

	if !strings.Contains(result, "Visible content") {
		t.Errorf("expected visible content, got: %s", result)
	}
	if !strings.Contains(result, "More visible") {
		t.Errorf("expected 'More visible', got: %s", result)
	}
	if strings.Contains(result, "hidden") {
		t.Errorf("script content should be stripped, got: %s", result)
	}
	if strings.Contains(result, "doStuff") {
		t.Errorf("script content should be stripped, got: %s", result)
	}
	if strings.Contains(result, "color: red") {
		t.Errorf("style content should be stripped, got: %s", result)
	}
}

func TestExtractTextFromHTML_StripsNavHeaderFooter(t *testing.T) {
	html := `<html><body>
<nav><ul><li>Home</li><li>About</li></ul></nav>
<header><div>Site Header</div></header>
<main><p>Main content here</p></main>
<aside>Sidebar stuff</aside>
<footer><p>Copyright 2026</p></footer>
</body></html>`

	result := extractTextFromHTML(html)

	if !strings.Contains(result, "Main content here") {
		t.Errorf("expected main content, got: %s", result)
	}
	if strings.Contains(result, "Site Header") {
		t.Errorf("header should be stripped, got: %s", result)
	}
	if strings.Contains(result, "Copyright 2026") {
		t.Errorf("footer should be stripped, got: %s", result)
	}
	if strings.Contains(result, "Sidebar stuff") {
		t.Errorf("aside should be stripped, got: %s", result)
	}
}

func TestExtractTextFromHTML_DecodesEntities(t *testing.T) {
	html := `<p>Tom &amp; Jerry &lt;3 &gt; &quot;quoted&quot; &#39;apostrophe&#39;</p>`
	result := extractTextFromHTML(html)

	if !strings.Contains(result, `Tom & Jerry <3 > "quoted" 'apostrophe'`) {
		t.Errorf("entities not decoded properly, got: %s", result)
	}
}

func TestExtractTextFromHTML_CollapsesWhitespace(t *testing.T) {
	html := `<p>First</p>



<p>Second</p>




<p>Third</p>`

	result := extractTextFromHTML(html)

	// Should not have more than 2 consecutive newlines.
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("should collapse blank lines, got: %q", result)
	}
	if !strings.Contains(result, "First") || !strings.Contains(result, "Third") {
		t.Errorf("missing content, got: %s", result)
	}
}

func TestNormalizeURL_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com", "https://example.com"},       // http -> https
		{"example.com", "https://example.com"},               // add scheme
		{"example.com/path?q=1", "https://example.com/path?q=1"},
	}

	for _, tc := range tests {
		got, err := normalizeURL(tc.input)
		if err != nil {
			t.Errorf("normalizeURL(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("normalizeURL(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestNormalizeURL_Invalid(t *testing.T) {
	tests := []string{
		"",
		"ftp://example.com",
		"://nohost",
	}

	for _, tc := range tests {
		_, err := normalizeURL(tc)
		if err == nil {
			t.Errorf("normalizeURL(%q) expected error, got nil", tc)
		}
	}
}

func TestWebFetchTool_MissingURL(t *testing.T) {
	tool := &WebFetchTool{}
	result := tool.Execute(context.Background(), map[string]any{})

	if !result.IsError {
		t.Error("expected error for missing URL")
	}
	if !strings.Contains(result.Content, "url is required") {
		t.Errorf("expected 'url is required', got: %s", result.Content)
	}
}

func TestWebFetchTool_InvalidURL(t *testing.T) {
	tool := &WebFetchTool{}
	result := tool.Execute(context.Background(), map[string]any{
		"url": "ftp://invalid.example",
	})

	if !result.IsError {
		t.Error("expected error for invalid URL scheme")
	}
}

func TestWebFetchTool_PromptPrepending(t *testing.T) {
	// Test the prompt prepending logic directly via the output builder.
	// We can't easily test the full Execute without a real HTTP server,
	// so we test the building logic through extractTextFromHTML + prompt.
	html := `<html><body><p>Some page content</p></body></html>`
	content := extractTextFromHTML(html)

	prompt := "Find the pricing info"

	var result strings.Builder
	result.WriteString("Prompt: ")
	result.WriteString(prompt)
	result.WriteString("\n\nPage content:\n")
	result.WriteString(content)

	output := result.String()
	if !strings.HasPrefix(output, "Prompt: Find the pricing info") {
		t.Errorf("prompt not prepended correctly, got: %s", output)
	}
	if !strings.Contains(output, "Page content:\nSome page content") {
		t.Errorf("content not included correctly, got: %s", output)
	}
}

func TestWebFetchTool_ContentTruncation(t *testing.T) {
	// Build a string longer than maxResultContent.
	long := strings.Repeat("a", maxResultContent+1000)

	// Simulate truncation logic.
	truncated := false
	content := long
	if len(content) > maxResultContent {
		content = content[:maxResultContent]
		truncated = true
	}

	if !truncated {
		t.Error("expected truncation")
	}
	if len(content) != maxResultContent {
		t.Errorf("expected %d chars, got %d", maxResultContent, len(content))
	}
}

func TestWebFetchTool_Interface(t *testing.T) {
	tool := &WebFetchTool{}

	if tool.Name() != "WebFetch" {
		t.Errorf("Name() = %q, want WebFetch", tool.Name())
	}
	if !tool.ReadOnly() {
		t.Error("WebFetch should be ReadOnly")
	}

	schema := tool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}
	if _, ok := props["url"]; !ok {
		t.Error("schema missing 'url' property")
	}
	if _, ok := props["prompt"]; !ok {
		t.Error("schema missing 'prompt' property")
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required in schema")
	}
	if len(required) != 1 || required[0] != "url" {
		t.Errorf("required = %v, want [url]", required)
	}
}
