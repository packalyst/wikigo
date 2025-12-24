package services

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// MarkdownService handles markdown parsing and rendering.
type MarkdownService struct {
	md        goldmark.Markdown
	sanitizer *bluemonday.Policy
}

// NewMarkdownService creates a new markdown service with secure defaults.
func NewMarkdownService() *MarkdownService {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,            // GitHub Flavored Markdown
			extension.Typographer,    // Smart quotes, dashes, etc.
			extension.DefinitionList, // Definition lists
			extension.Footnote,       // Footnotes
			&wikiLinkExtension{},     // Custom [[wiki-links]]
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(), // Auto-generate heading IDs
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),           // Treat newlines as <br>
			html.WithXHTML(),               // XHTML compatible output
			html.WithUnsafe(),              // We'll sanitize separately with bluemonday
		),
	)

	// Create a strict sanitizer policy
	sanitizer := bluemonday.UGCPolicy()

	// Allow additional safe elements
	sanitizer.AllowElements("details", "summary", "mark", "abbr", "kbd", "sub", "sup")

	// Allow data attributes for syntax highlighting
	sanitizer.AllowDataAttributes()

	// Allow class attributes for styling
	sanitizer.AllowAttrs("class").OnElements(
		"div", "span", "pre", "code", "table", "thead", "tbody", "tr", "th", "td",
		"ul", "ol", "li", "blockquote", "h1", "h2", "h3", "h4", "h5", "h6",
	)

	// Allow id attributes for heading anchors
	sanitizer.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6", "a")

	// Allow language class on code blocks
	sanitizer.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[a-zA-Z0-9_-]+$`)).OnElements("code")

	// Allow internal wiki links
	sanitizer.AllowAttrs("href").OnElements("a")
	sanitizer.AllowRelativeURLs(true)

	// Allow images with alt text
	sanitizer.AllowAttrs("alt", "title", "width", "height").OnElements("img")
	sanitizer.AllowAttrs("src").Matching(regexp.MustCompile(`^(/uploads/|https?://)`)).OnElements("img")

	return &MarkdownService{
		md:        md,
		sanitizer: sanitizer,
	}
}

// Render converts markdown to sanitized HTML.
func (s *MarkdownService) Render(markdown string) (string, error) {
	var buf bytes.Buffer

	if err := s.md.Convert([]byte(markdown), &buf); err != nil {
		return "", err
	}

	// Sanitize the output
	sanitized := s.sanitizer.SanitizeBytes(buf.Bytes())

	return string(sanitized), nil
}

// RenderUnsafe converts markdown to HTML without sanitization.
// Only use for trusted content.
func (s *MarkdownService) RenderUnsafe(markdown string) (string, error) {
	var buf bytes.Buffer

	if err := s.md.Convert([]byte(markdown), &buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// ExtractTitle extracts the first h1 heading from markdown content.
func (s *MarkdownService) ExtractTitle(markdown string) string {
	lines := strings.Split(markdown, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}

	return ""
}

// ExtractLinks extracts all wiki-style links from markdown.
func (s *MarkdownService) ExtractLinks(markdown string) []string {
	re := regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)
	matches := re.FindAllStringSubmatch(markdown, -1)

	links := make([]string, 0, len(matches))
	seen := make(map[string]bool)

	for _, match := range matches {
		if len(match) > 1 {
			link := strings.TrimSpace(match[1])
			if !seen[link] {
				links = append(links, link)
				seen[link] = true
			}
		}
	}

	return links
}

// GenerateTOC extracts headings and generates a table of contents.
func (s *MarkdownService) GenerateTOC(markdown string) []TOCEntry {
	reader := text.NewReader([]byte(markdown))
	doc := s.md.Parser().Parse(reader)

	var entries []TOCEntry

	ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		if heading, ok := node.(*ast.Heading); ok {
			text := extractTextFromNode(heading, []byte(markdown))
			id := generateHeadingID(text)

			entries = append(entries, TOCEntry{
				Level: heading.Level,
				Text:  text,
				ID:    id,
			})
		}

		return ast.WalkContinue, nil
	})

	return entries
}

// TOCEntry represents a table of contents entry.
type TOCEntry struct {
	Level int
	Text  string
	ID    string
}

// extractTextFromNode extracts plain text from an AST node.
func extractTextFromNode(node ast.Node, source []byte) string {
	var buf bytes.Buffer

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if textNode, ok := child.(*ast.Text); ok {
			buf.Write(textNode.Segment.Value(source))
		} else if child.HasChildren() {
			buf.WriteString(extractTextFromNode(child, source))
		}
	}

	return buf.String()
}

// generateHeadingID creates a URL-safe ID from heading text.
func generateHeadingID(text string) string {
	id := slugify(text)
	if id == "" {
		return "heading"
	}
	return id
}

// Wiki link extension for [[page]] syntax

type wikiLinkExtension struct{}

func (e *wikiLinkExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&wikiLinkParser{}, 100),
		),
	)
}

type wikiLinkParser struct{}

func (p *wikiLinkParser) Trigger() []byte {
	return []byte{'['}
}

func (p *wikiLinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()

	// Check for [[
	if len(line) < 4 || line[0] != '[' || line[1] != '[' {
		return nil
	}

	// Find closing ]]
	end := bytes.Index(line[2:], []byte("]]"))
	if end < 0 {
		return nil
	}

	content := string(line[2 : end+2])

	// Split on | for display text
	parts := strings.SplitN(content, "|", 2)
	pageName := strings.TrimSpace(parts[0])
	displayText := pageName
	if len(parts) > 1 {
		displayText = strings.TrimSpace(parts[1])
	}

	// Create the slug from page name
	slug := slugify(pageName)

	// Create link node
	link := ast.NewLink()
	link.Destination = []byte("/wiki/" + slug)
	link.Title = []byte(pageName)

	text := ast.NewString([]byte(displayText))
	text.SetRaw(true)
	link.AppendChild(link, text)

	// Advance reader past the wiki link
	block.Advance(segment.Start + end + 4)

	return link
}

// slugify converts a page name to a URL-safe slug.
// Preserves forward slashes for hierarchical paths like "linux/ubuntu/networking".
func slugify(name string) string {
	// Convert to lowercase
	slug := strings.ToLower(name)

	// Replace spaces and underscores with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")

	// Remove non-alphanumeric characters except hyphens and forward slashes
	re := regexp.MustCompile(`[^a-z0-9/-]`)
	slug = re.ReplaceAllString(slug, "")

	// Remove multiple consecutive hyphens
	re = regexp.MustCompile(`-+`)
	slug = re.ReplaceAllString(slug, "-")

	// Remove multiple consecutive slashes
	re = regexp.MustCompile(`/+`)
	slug = re.ReplaceAllString(slug, "/")

	// Trim hyphens and slashes from ends and around slashes
	slug = strings.Trim(slug, "-/")

	// Clean up hyphens around slashes (e.g., "foo-/bar" -> "foo/bar")
	re = regexp.MustCompile(`-*/+-*`)
	slug = re.ReplaceAllString(slug, "/")

	return slug
}

// Slugify is exported for use in handlers.
func Slugify(name string) string {
	return slugify(name)
}
