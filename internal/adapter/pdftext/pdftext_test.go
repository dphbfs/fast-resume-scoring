package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The PDFs in testdata are examples/resume.md rendered two ways:
// bullets.pdf by LibreOffice (bullet glyphs on their own lines, blank
// lines inside wrapped bullets) and two-column.pdf by Chrome (a skills
// sidebar, no bullet glyphs). image-only.pdf has no text layer.

// Test-only child inputs that simulate a crashing and a hanging reader.
const (
	crashInput = "test: crash"
	hangInput  = "test: hang"
)

// TestMain serves the child process: Extract runs the test binary itself.
func TestMain(m *testing.M) {
	if maxText, ok := os.LookupEnv(childEnv); ok {
		raw, _ := io.ReadAll(os.Stdin)
		switch string(raw) {
		case crashInput:
			panic("simulated crash")
		case hangInput:
			time.Sleep(time.Hour)
		}
		os.Exit(RunChild(maxText, bytes.NewReader(raw), os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExtract(t *testing.T) {
	for name, want := range map[string][]string{
		"bullets.pdf": {
			"# Experience",
			"• Built the shipment-tracking API in Go (gRPC and REST), serving 2,000 requests per second at p99\nunder 80 ms.",
			"# Skills\nLanguages: Go, Python, SQL, Bash",
		},
		"two-column.pdf": {
			"# Skills\nLanguages: Go, Python, SQL, Bash",
			"# Experience\nBackend Engineer — Parcelwise",
		},
	} {
		t.Run(name, func(t *testing.T) {
			text, err := Extract(context.Background(), readTestdata(t, name), 48<<10)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range want {
				if !strings.Contains(text, w) {
					t.Errorf("text does not contain %q:\n%s", w, text)
				}
			}
			if strings.Contains(text, "\n•\n") || strings.Contains(text, "\n\n") {
				t.Errorf("lone bullet or blank line left:\n%s", text)
			}
		})
	}
}

func TestExtractErrors(t *testing.T) {
	bullets := readTestdata(t, "bullets.pdf")
	// The library's Page.findInherited loops forever on this /Parent.
	cyclicParent := buildPDF(
		"<</Type /Catalog /Pages 2 0 R>>",
		"<</Type /Pages /Count 1 /Kids [3 0 R]>>",
		"<</Type /Page /Parent 3 0 R /Contents 4 0 R>>",
		"<</Length 0>>\nstream\n\nendstream",
	)
	defer setTimeout(500 * time.Millisecond)()
	for name, tc := range map[string]struct {
		raw     []byte
		maxText int
		want    error
	}{
		"no text layer": {readTestdata(t, "image-only.pdf"), 48 << 10, ErrNoText},
		"too large":     {bullets, 100, ErrTooLarge},
		"not a pdf":     {[]byte("hello, world"), 48 << 10, ErrUnreadable},
		"truncated":     {bullets[:len(bullets)/2], 48 << 10, ErrUnreadable},
		"empty":         {nil, 48 << 10, ErrUnreadable},
		"reader hangs":  {cyclicParent, 48 << 10, ErrTimeout},
		"child crashes": {[]byte(crashInput), 48 << 10, ErrUnreadable},
		"child hangs":   {[]byte(hangInput), 48 << 10, ErrTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Extract(context.Background(), tc.raw, tc.maxText); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := Extract(ctx, bullets, 48<<10); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

// The library's own page lookup loops forever on this tree; extract must
// not.
func TestExtractCyclicPageTree(t *testing.T) {
	raw := buildPDF("<</Type /Catalog /Pages 2 0 R>>", "<</Type /Pages /Count 1 /Kids [2 0 R]>>")
	done := make(chan error, 1)
	go func() {
		_, err := extract(bytes.NewReader(raw), int64(len(raw)), 48<<10)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errPageTree) {
			t.Errorf("err = %v, want errPageTree", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("extract did not return")
	}
}

func TestChildEnvironKeepsSecrets(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "secret")
	env := childEnviron(1024)
	if !slices.Contains(env, childEnv+"=1024") {
		t.Errorf("env = %q, want the child marker", env)
	}
	for _, kv := range env {
		if strings.Contains(kv, "secret") {
			t.Errorf("env passes %q to the child", kv)
		}
	}
}

func TestRunChildInvalidLimit(t *testing.T) {
	var stderr bytes.Buffer
	if code := RunChild("x", strings.NewReader(""), io.Discard, &stderr); code != 1 || stderr.Len() == 0 {
		t.Errorf("code = %d, stderr = %q; want 1 and a message", code, stderr.String())
	}
}

func TestNormalize(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"lone bullet":     {"•\nBuilt APIs\n", "• Built APIs"},
		"no space":        {"•Built APIs", "• Built APIs"},
		"blank lines":     {"Built APIs at\n \n\nscale", "Built APIs at\nscale"},
		"headings":        {"EXPERIENCE\nTechnical Skills:\nGo", "# EXPERIENCE\n# Technical Skills:\nGo"},
		"not a heading":   {"Skills: Go, SQL", "Skills: Go, SQL"},
		"nbsp and spaces": {"  Go\u00a0services  ", "Go services"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := normalize(tc.in); got != tc.want {
				t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// setTimeout sets the Extract timeout and returns a func that restores it.
func setTimeout(d time.Duration) func() {
	old := timeout
	timeout = d
	return func() { timeout = old }
}

// buildPDF returns a PDF whose objects are 1..n, in order, with object 1
// as the Root.
func buildPDF(objects ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d /Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}
