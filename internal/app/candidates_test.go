package app

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-tailoring/tuning"
)

func candidateTexts(cs []domain.Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Text
	}
	return out
}

func TestTokenize(t *testing.T) {
	got := tokenize(`Experience with Node.js, C++ and C# (CI/CD, X.509); "5+ years".`, true)
	var texts []string
	for _, tok := range got {
		texts = append(texts, tok.text)
	}
	want := []string{"Experience", "with", "Node.js", "C++", "and", "C#", "CI/CD", "X.509", "5+", "years"}
	if !slices.Equal(texts, want) {
		t.Fatalf("tokens\n got: %q\nwant: %q", texts, want)
	}

	// Breaks: after "Node.js," and "CI/CD," and "X.509);", and before "(CI/CD".
	breaks := map[string]bool{"Node.js": true, "C#": true, "CI/CD": true, "X.509": true, "years": true}
	for _, tok := range got {
		if tok.breakAfter != breaks[tok.text] {
			t.Errorf("token %q breakAfter = %v, want %v", tok.text, tok.breakAfter, breaks[tok.text])
		}
	}
}

func TestTokenizeSplitsSlashesAndDashes(t *testing.T) {
	var got []string
	for _, tok := range tokenize("TypeScript/Node.js and CI/CD or A/B tests in Web3 or DeFi—whether JWT/OIDC", true) {
		got = append(got, tok.text)
	}
	want := []string{"TypeScript", "Node.js", "and", "CI/CD", "or", "A/B", "tests", "in", "Web3", "or", "DeFi", "whether", "JWT", "OIDC"}
	if !slices.Equal(got, want) {
		t.Fatalf("tokens\n got: %q\nwant: %q", got, want)
	}
}

func TestSentenceCandidates(t *testing.T) {
	tests := []struct {
		name     string
		sentence string
		maxWords int
		want     []string
	}{
		{
			name:     "stopwords never start or end a window",
			sentence: "5+ years of Go",
			maxWords: 4,
			want:     []string{"5+ years", "5+ years of Go", "years", "years of Go", "Go"},
		},
		{
			name:     "windows do not cross commas or brackets",
			sentence: "key-value stores (MongoDB, Memcache, Redis)",
			maxWords: 4,
			want:     []string{"key-value", "key-value stores", "stores", "MongoDB", "Memcache", "Redis"},
		},
		{
			name:     "max window length",
			sentence: "distributed systems design experience",
			maxWords: 2,
			want: []string{
				"distributed", "distributed systems", "systems", "systems design",
				"design", "design experience", "experience",
			},
		},
		{
			name:     "slash-joined names are alternatives, not one phrase",
			sentence: "Terraform/Terragrunt experience",
			maxWords: 2,
			want: []string{
				"Terraform", "Terragrunt", "Terragrunt experience", "experience",
				"Terraform/Terragrunt", "Terraform/Terragrunt experience",
			},
		},
		{
			name:     "case-insensitive duplicates within a sentence are dropped",
			sentence: "Go services and go tooling",
			maxWords: 1,
			want:     []string{"Go", "services", "tooling"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidateTexts(sentenceCandidates(domain.ContextSentence{Ref: "s1", Text: tt.sentence}, tt.maxWords))
			if !slices.Equal(got, tt.want) {
				t.Errorf("candidates\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestSentenceCandidatesKeepRef(t *testing.T) {
	for _, c := range sentenceCandidates(domain.ContextSentence{Ref: "s7", Text: "client/server architectures"}, 2) {
		if c.Ref != "s7" {
			t.Errorf("candidate %q ref = %q, want s7", c.Text, c.Ref)
		}
	}
}

func chunkTexts(cs []chunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Text
	}
	return out
}

func TestChunkSentence(t *testing.T) {
	tests := []struct {
		name     string
		sentence string
		want     []string
	}{
		{
			name:     "such as and commas separate list items",
			sentence: "Experience with streaming data infrastructure such as Kafka, Flink, or Spark",
			want:     []string{"streaming data infrastructure", "Kafka", "Flink", "Spark"}, // "Experience" is generic-only
		},
		{
			name:     "and/or and brackets separate, of does not",
			sentence: "5+ years of Go and Rust (or C++)",
			want:     []string{"5+ years of Go", "Rust", "C++"},
		},
		{
			name:     "including, e.g. and like separate",
			sentence: "Cloud platforms including AWS; databases e.g. Postgres; tools like Terraform",
			want:     []string{"Cloud platforms", "AWS", "databases", "Postgres", "tools", "Terraform"},
		},
		{
			name:     "stopword-only pieces are dropped",
			sentence: "You will own it, and more",
			want:     []string{"You will own it"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chunkTexts(chunkSentence(domain.ContextSentence{Ref: "s1", Text: tt.sentence}, 4))
			if !slices.Equal(got, tt.want) {
				t.Errorf("chunks\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestChunkOptions(t *testing.T) {
	cs := chunkSentence(domain.ContextSentence{Ref: "s2", Text: "client/server architectures"}, 4)
	if len(cs) != 1 {
		t.Fatalf("chunks = %q, want one", chunkTexts(cs))
	}
	c := cs[0]
	if c.Ref != "s2" {
		t.Errorf("ref = %q, want s2", c.Ref)
	}
	for _, want := range []string{"client/server architectures", "client/server", "architectures", "client", "server architectures"} {
		if !slices.Contains(c.Options, want) {
			t.Errorf("options %q missing %q", c.Options, want)
		}
	}
}

func TestStripLabelPrefix(t *testing.T) {
	tests := map[string]string{
		"Deep Backend Expertise: A strong command of backend architecture": "A strong command of backend architecture",
		"Raise the Bar: Advocate for product quality":                      "Advocate for product quality",
		"Mentor & Lead: Mentor associate engineers":                        "Mentor associate engineers",
		"Bonus: you have experience with Kubernetes":                       "you have experience with Kubernetes",
		"Kubernetes: operators and controllers":                            "Kubernetes: operators and controllers", // a skill, not a label
		"Tech stack: Go, Kafka":                                            "Tech stack: Go, Kafka",                 // lowercase word
		"5+ years of Go":                                                   "5+ years of Go",
		"A Very Long Label With Many Words: text":                          "A Very Long Label With Many Words: text",
	}
	for in, want := range tests {
		if got := stripLabelPrefix(in); got != want {
			t.Errorf("stripLabelPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestYearsFragmentsAreNotOffered(t *testing.T) {
	cs := chunkSentence(domain.ContextSentence{Ref: "s1", Text: "2 years of experience working with Java, related field, or foreign equivalent"}, 4)
	var opts []string
	for _, c := range cs {
		opts = append(opts, c.Options...)
	}
	for _, bad := range []string{"years", "years of experience working", "2 years of experience working", "related field", "foreign equivalent", "equivalent"} {
		if slices.Contains(opts, bad) {
			t.Errorf("options %q contain years/qualification fragment %q", opts, bad)
		}
	}
	if !slices.Contains(opts, "Java") {
		t.Errorf("options %q lost Java", opts)
	}
	// A years qualifier attached to a skill stays available.
	cs = chunkSentence(domain.ContextSentence{Ref: "s2", Text: "5+ years of Go"}, 4)
	if len(cs) != 1 || !slices.Contains(cs[0].Options, "5+ years of Go") {
		t.Errorf("chunks = %+v, want the qualified skill offered", cs)
	}
}

func TestGenericOnlyCandidatesAreDropped(t *testing.T) {
	cs := chunkSentence(domain.ContextSentence{Ref: "s1", Text: "Hands-on experience, deep demonstrated expertise, Go experience"}, 4)
	var opts []string
	for _, c := range cs {
		opts = append(opts, c.Options...)
	}
	for _, bad := range []string{"Hands-on experience", "Hands-on", "experience", "deep demonstrated expertise"} {
		if slices.Contains(opts, bad) {
			t.Errorf("options %q contain generic-only %q", opts, bad)
		}
	}
	if !slices.Contains(opts, "Go experience") || !slices.Contains(opts, "Go") {
		t.Errorf("options %q lost \"Go experience\" or \"Go\"", opts)
	}
}

func TestGenerateCandidatesSkipsHeadings(t *testing.T) {
	e := New(nil, nil, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{MaxWindowWords: 2}, tuning.Default())
	r := &run{}
	r.sentences, r.headings = splitSentences("Bonus Points\n- Kubernetes\n")
	for i := range r.sentences {
		r.sentences[i].Section = domain.SectionPreferred
	}
	if err := e.generateCandidates(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := chunkTexts(r.chunks); !slices.Equal(got, []string{"Kubernetes"}) {
		t.Errorf("chunks = %q, want [Kubernetes]", got)
	}
	// "Bonus Points" is a weak heading: not skipped, but every Candidate is
	// generic-only, so it yields no chunk.
	if ts := r.trace.Sentences; len(ts) != 2 || ts[0].Dropped || ts[1].Heading != "Bonus Points" {
		t.Errorf("trace sentences = %+v, want both kept, item under the Bonus Points heading", ts)
	}
}

func TestGenerateCandidatesSkipsOnlyStrongHeadings(t *testing.T) {
	e := New(nil, nil, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{MaxWindowWords: 4}, tuning.Default())
	r := &run{}
	r.sentences, r.headings, r.strongHeading = splitLines(
		"What You Bring:\nExperience with TypeScript/Node.js\n## Tools\n- Kubernetes\n")
	for i := range r.sentences {
		r.sentences[i].Section = domain.SectionRequired
	}
	if err := e.generateCandidates(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	// "Experience with TypeScript/Node.js" is short and unpunctuated, so it
	// is heading context for the next line, but it is not skipped.
	if got := chunkTexts(r.chunks); !slices.Equal(got, []string{"TypeScript/Node.js", "Kubernetes"}) {
		t.Errorf("chunks = %q, want [TypeScript/Node.js Kubernetes]", got)
	}
}

func TestGenerateCandidatesSkipsDroppedSections(t *testing.T) {
	m := metrics.NewRecorder()
	e := New(nil, nil, m, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Pipeline{MaxWindowWords: 2}, tuning.Default())
	r := &run{sentences: []domain.ContextSentence{
		{Ref: "s1", Text: "Kubernetes", Section: domain.SectionRequired},
		{Ref: "s2", Text: "Dental insurance", Section: domain.SectionBenefits},
		{Ref: "s3", Text: "Terraform", Section: domain.SectionPreferred},
		{Ref: "s4", Text: "About the role", Section: domain.SectionOther},
	}}
	if err := e.generateCandidates(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := chunkTexts(r.chunks); !slices.Equal(got, []string{"Kubernetes", "Terraform"}) {
		t.Errorf("chunks = %q", got)
	}
	c := m.Summary().Counters
	if c["chunks.total"] != 2 || c["candidates.total"] != 2 {
		t.Errorf("counters = %v, want chunks.total=2 candidates.total=2", c)
	}
}

func TestGenerateCandidatesSkipResponsibilities(t *testing.T) {
	for _, skip := range []bool{false, true} {
		e := New(nil, nil, metrics.NewRecorder(), slog.New(slog.NewTextHandler(io.Discard, nil)),
			config.Pipeline{MaxWindowWords: 2, SkipResponsibilities: skip}, tuning.Default())
		r := &run{}
		r.sentences, r.headings = splitSentences("- Kubernetes\n- Terraform\n")
		r.sentences[0].Section = domain.SectionRequired
		r.sentences[1].Section = domain.SectionResponsibilities
		if err := e.generateCandidates(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		want := []string{"Kubernetes", "Terraform"}
		if skip {
			want = want[:1]
		}
		if got := chunkTexts(r.chunks); !slices.Equal(got, want) {
			t.Errorf("skip %v: chunks = %q, want %q", skip, got, want)
		}
		if r.trace.Sentences[1].Dropped != skip {
			t.Errorf("skip %v: responsibilities sentence dropped = %v", skip, r.trace.Sentences[1].Dropped)
		}
	}
}
