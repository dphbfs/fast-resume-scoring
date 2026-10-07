// Package replay records Jev exchanges to a file and answers from that file
// later, so the CLIs can run without an API key or network (demos,
// examples, offline debugging). A request is identified by a SHA-256 of
// its state and questions, so a replay only answers requests that are
// byte-for-byte the ones recorded: same inputs, same tuning file.
package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/dphbfs/fast-resume-scoring/internal/platform/fsutil"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

// FormatVersion is the recording file's format version.
const FormatVersion = 1

// ErrNotRecorded means the recording has no answer for a request.
var ErrNotRecorded = errors.New("replay: request not in the recording")

// File is the recording on disk: responses keyed by request Key.
type File struct {
	Version   int                              `json:"version"`
	Exchanges map[string]port.ClassifyResponse `json:"exchanges"`
}

// Key identifies a request by a SHA-256 of its JSON encoding. Maps encode
// with sorted keys, so equal requests always get the same Key.
func Key(req port.ClassifyRequest) (string, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("replay: encode request: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Player answers from a recording and never touches the network.
type Player struct {
	path      string
	exchanges map[string]port.ClassifyResponse
	metrics   port.Metrics
}

// NewPlayer loads the recording at path.
func NewPlayer(path string, m port.Metrics) (*Player, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("replay: %s: %w", path, err)
	}
	if f.Version != FormatVersion {
		return nil, fmt.Errorf("replay: %s: format version %d, want %d", path, f.Version, FormatVersion)
	}
	return &Player{path: path, exchanges: f.Exchanges, metrics: m}, nil
}

// Classify returns the recorded response for req, or ErrNotRecorded.
func (p *Player) Classify(_ context.Context, req port.ClassifyRequest) (port.ClassifyResponse, error) {
	key, err := Key(req)
	if err != nil {
		return port.ClassifyResponse{}, err
	}
	resp, ok := p.exchanges[key]
	if !ok {
		p.metrics.Add("jev.replay_misses", 1)
		return port.ClassifyResponse{}, fmt.Errorf(
			"%w %s (request %s): the input files, tuning file, or Job Summary differ from the recorded run; record again with JEV_RECORD",
			ErrNotRecorded, p.path, key[:12])
	}
	p.metrics.Add("jev.replayed", 1)
	return resp, nil
}

// Recorder passes requests to a real client and saves every successful
// exchange, rewriting the file after each one so an interrupted run keeps
// what it has. Existing exchanges in the file are kept.
type Recorder struct {
	next port.AIClassifierClient
	path string

	mu   sync.Mutex
	file File
}

// NewRecorder wraps next and records into path, extending the recording
// already there, if any.
func NewRecorder(next port.AIClassifierClient, path string) (*Recorder, error) {
	f := File{Version: FormatVersion, Exchanges: map[string]port.ClassifyResponse{}}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("replay: %w", err)
	default:
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("replay: %s: %w", path, err)
		}
		if f.Version != FormatVersion {
			return nil, fmt.Errorf("replay: %s: format version %d, want %d", path, f.Version, FormatVersion)
		}
		if f.Exchanges == nil {
			f.Exchanges = map[string]port.ClassifyResponse{}
		}
	}
	return &Recorder{next: next, path: path, file: f}, nil
}

// Classify asks the wrapped client and records the answer.
func (r *Recorder) Classify(ctx context.Context, req port.ClassifyRequest) (port.ClassifyResponse, error) {
	key, err := Key(req)
	if err != nil {
		return port.ClassifyResponse{}, err
	}
	resp, err := r.next.Classify(ctx, req)
	if err != nil {
		return resp, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.file.Exchanges[key] = resp
	if err := fsutil.WriteJSONAtomic(r.path, r.file); err != nil {
		return port.ClassifyResponse{}, fmt.Errorf("replay: %w", err)
	}
	return resp, nil
}
