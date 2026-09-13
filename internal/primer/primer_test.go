package primer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// siteLinkRe matches a Markdown link into the docs site, capturing the page
	// path and the anchor.
	siteLinkRe = regexp.MustCompile(`\]\(https://gburgyan\.github\.io/aat/([^)#\s]*)(?:#([^)\s]*))?\)`)
	// relativeLinkRe matches a Markdown link to another docs page by file name.
	relativeLinkRe = regexp.MustCompile(`\]\([\w./-]+\.md(?:#[^)]*)?\)`)
	headingRe      = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
	inlineLinkRe   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	notSlugRe      = regexp.MustCompile(`[^\w\s-]`)
	slugSpaceRe    = regexp.MustCompile(`[-\s]+`)
)

func TestMarkdown(t *testing.T) {
	assert.True(t, strings.HasPrefix(Markdown(), "# AAT for AI Assistants\n"))
}

// TestSiteLinks: the primer is read as raw Markdown, where a link relative to
// the docs site leads nowhere, so it links to the site instead. Every such
// link, in the primer and in the site's llms.txt index, must name a page that
// exists and, with an anchor, a heading on that page: mkdocs does not check
// links written as URLs.
func TestSiteLinks(t *testing.T) {
	docs := filepath.Join("..", "..", "docs", "user")
	index, err := os.ReadFile(filepath.Join(docs, "llms.txt"))
	require.NoError(t, err)

	for name, text := range map[string]string{"llms.md": Markdown(), "llms.txt": string(index)} {
		assert.Empty(t, relativeLinkRe.FindAllString(text, -1), "%s links to pages by file name; use site URLs", name)

		links := siteLinkRe.FindAllStringSubmatch(text, -1)
		require.NotEmpty(t, links, name)
		for _, link := range links {
			page, anchor := link[1], link[2]
			if page == "llms-full.txt" {
				continue // written by the docs build (docs/hooks/llms_txt.py)
			}
			data, err := readPage(docs, page)
			if !assert.NoError(t, err, "%s: %s names no page", name, link[0]) {
				continue
			}
			if anchor != "" {
				assert.Contains(t, headingAnchors(string(data)), anchor, "%s: %s names no heading", name, link[0])
			}
		}
	}
}

// readPage reads the docs source of a site page path such as "archives/" or
// "examples/shop/".
func readPage(docs, page string) ([]byte, error) {
	page = strings.TrimSuffix(page, "/")
	if page == "" {
		return os.ReadFile(filepath.Join(docs, "index.md"))
	}
	data, err := os.ReadFile(filepath.Join(docs, page+".md"))
	if err != nil {
		return os.ReadFile(filepath.Join(docs, page, "index.md"))
	}
	return data, nil
}

// headingAnchors returns the anchors mkdocs gives a page's headings, outside
// code blocks: Python-Markdown's toc slugify of the heading text.
func headingAnchors(markdown string) []string {
	var anchors []string
	fenced := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			anchors = append(anchors, slug(m[1]))
		}
	}
	return anchors
}

func slug(heading string) string {
	text := inlineLinkRe.ReplaceAllString(heading, "$1")
	ascii := strings.Map(func(r rune) rune {
		if r > 127 {
			return -1
		}
		return r
	}, text)
	cleaned := strings.ToLower(strings.TrimSpace(notSlugRe.ReplaceAllString(ascii, "")))
	return slugSpaceRe.ReplaceAllString(cleaned, "-")
}

func TestSlug(t *testing.T) {
	assert.Equal(t, "what-is-redacted-and-what-is-not", slug("What Is Redacted, and What Is Not"))
	assert.Equal(t, "rebuilding-summaries-aat-run-rebuild-summaries", slug("Rebuilding Summaries (`aat run rebuild-summaries`)"))
	assert.Equal(t, "inspecting-a-run-from-the-cli", slug("Inspecting a Run from the CLI"))
}
