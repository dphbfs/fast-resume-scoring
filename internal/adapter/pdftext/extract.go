package pdftext

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

// MaxPages caps the pages read. A resume longer than this is almost
// certainly not a resume.
const MaxPages = 20

// extract reads the PDF in r (size bytes) in this process. It can hang or
// exhaust memory on a malformed file, so only the child process calls it
// (see Extract).
func extract(r io.ReaderAt, size int64, maxText int) (text string, err error) {
	// The library reports some malformed input by panicking.
	defer func() {
		if p := recover(); p != nil {
			text, err = "", fmt.Errorf("the reader failed: %v", p)
		}
	}()

	doc, err := pdf.NewReader(r, size)
	if err != nil {
		return "", err
	}
	pages, err := collectPages(doc.Trailer().Key("Root").Key("Pages"))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, p := range pages {
		page, err := p.GetPlainText(nil)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", i+1, err)
		}
		b.WriteString(page)
		b.WriteByte('\n')
		if b.Len() > maxText {
			return "", ErrTooLarge
		}
	}
	text = normalize(b.String())
	if text == "" {
		return "", ErrNoText
	}
	return text, nil
}

// Page tree limits. The library's own Reader.Page loops forever on a page
// tree that contains itself, so collectPages walks the tree with bounds.
const (
	maxTreeDepth = 32
	maxTreeNodes = 1000
)

// errPageTree means the page tree is too deep or too large, which in a
// resume means it is malformed (or cyclic).
var errPageTree = errors.New("malformed page tree")

// collectPages returns the first MaxPages pages of the page tree under
// root, in document order.
func collectPages(root pdf.Value) ([]pdf.Page, error) {
	var pages []pdf.Page
	nodes := 0
	var walk func(node pdf.Value, depth int) error
	walk = func(node pdf.Value, depth int) error {
		if depth > maxTreeDepth {
			return errPageTree
		}
		kids := node.Key("Kids")
		for i := 0; i < kids.Len() && len(pages) < MaxPages; i++ {
			if nodes++; nodes > maxTreeNodes {
				return errPageTree
			}
			kid := kids.Index(i)
			switch kid.Key("Type").Name() {
			case "Pages":
				if err := walk(kid, depth+1); err != nil {
					return err
				}
			case "Page":
				pages = append(pages, pdf.Page{V: kid})
			}
		}
		return nil
	}
	return pages, walk(root, 0)
}

// bulletGlyphs are list markers that PDF writers often emit on a line of
// their own, or with no space before the item text.
var bulletGlyphs = map[string]bool{"•": true, "●": true, "◦": true, "▪": true, "■": true, "‣": true}

// headings are common Resume Section headings. A line that is exactly one
// of them (any case, optional trailing colon) becomes "# Heading", so the
// Resume parser sees the sections a Markdown resume would have.
var headings = map[string]bool{
	"summary": true, "professional summary": true, "profile": true, "professional profile": true,
	"about": true, "about me": true, "objective": true, "career objective": true,
	"experience": true, "work experience": true, "professional experience": true,
	"employment": true, "employment history": true, "work history": true, "career history": true,
	"skills": true, "technical skills": true, "core skills": true, "key skills": true,
	"technologies": true, "tech stack": true, "tools": true, "skills & tools": true,
	"education": true, "certifications": true, "certificates": true,
	"licenses & certifications": true, "education & certifications": true,
	"projects": true, "personal projects": true, "side projects": true, "selected projects": true,
	"awards": true, "publications": true, "languages": true, "volunteering": true, "interests": true,
}

// normalize turns extracted PDF text into the Markdown convention of a
// Resume: known headings become "# Heading", a bullet glyph starts a
// "• item" line (joined to its text when the PDF split them), and blank
// lines are dropped, because PDF writers emit them inside wrapped lines.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	var out []string
	bullet := ""
	for line := range strings.Lines(s) {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case bulletGlyphs[line]:
			bullet = line
		case bullet != "":
			out = append(out, bullet+" "+line)
			bullet = ""
		case headings[strings.ToLower(strings.TrimSuffix(line, ":"))]:
			out = append(out, "# "+line)
		default:
			out = append(out, spaceAfterBullet(line))
		}
	}
	return strings.Join(out, "\n")
}

// spaceAfterBullet turns "•item" into "• item".
func spaceAfterBullet(line string) string {
	for g := range bulletGlyphs {
		if rest, ok := strings.CutPrefix(line, g); ok && rest != "" && rest[0] != ' ' {
			return g + " " + rest
		}
	}
	return line
}
