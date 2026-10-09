// Package pdftext extracts plain text from a PDF, so a PDF Resume goes
// through the same pipeline as a text one. It is the only package that
// imports the PDF library (github.com/ledongthuc/pdf: pure Go, no cgo);
// swap the library here and nowhere else.
//
// The library hangs or exhausts memory on some malformed files (fuzzing
// found cycles through /Kids and /Parent, and xref sizes taken from the
// file), and Go cannot recover from either. So Extract is a proxy: it runs
// this same program again as a child process in PDF mode, with a timeout,
// and a bad file only ever takes down the child. Every program that calls
// Extract must call MaybeRunChild first thing in main.
package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MaxBytes caps the size of a PDF file.
const MaxBytes = 10 << 20

var (
	// ErrNoText means the PDF has no text layer, usually a scanned image.
	ErrNoText = errors.New("no extractable text (a scanned image?); OCR is not supported, export the resume as text or Markdown")
	// ErrTooLarge means the extracted text is longer than the caller's limit.
	ErrTooLarge = errors.New("extracted text is larger than the input limit")
	// ErrTimeout means reading the PDF took longer than the timeout.
	ErrTimeout = errors.New("reading the PDF timed out; the file may be malformed")
	// ErrUnreadable means the PDF could not be read: it is malformed,
	// encrypted, or crashed the reader.
	ErrUnreadable = errors.New("cannot read the PDF")
)

// childEnv marks the child process; its value is the text limit in bytes.
const childEnv = "FRS_PDFTEXT_CHILD"

// Child exit codes besides 0 (text on stdout) and 1 (other error on
// stderr). A Go panic or fatal error exits with 2.
const (
	exitNoText   = 3
	exitTooLarge = 4
)

// timeout bounds one Extract call; a resume normally takes milliseconds.
var timeout = 10 * time.Second

// executable is the program Extract runs as the child.
var executable = os.Executable

// Extract returns the normalized text of the PDF raw, read in a child
// process. It returns ErrTooLarge when the text exceeds maxText bytes.
func Extract(ctx context.Context, raw []byte, maxText int) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", fmt.Errorf("%w: find this program to read it: %w", ErrUnreadable, err)
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, exe) //nolint:gosec // G204: runs this same program in PDF child mode
	cmd.Env = childEnviron(maxText)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	err = cmd.Run()

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), nil
	case ctx.Err() != nil:
		return "", ctx.Err()
	case runCtx.Err() != nil:
		return "", ErrTimeout
	case !errors.As(err, &exitErr):
		return "", fmt.Errorf("%w: start the reader: %w", ErrUnreadable, err)
	}
	switch exitErr.ExitCode() {
	case exitNoText:
		return "", ErrNoText
	case exitTooLarge:
		return "", ErrTooLarge
	case 1:
		return "", fmt.Errorf("%w: %s", ErrUnreadable, firstLine(stderr.String()))
	default:
		return "", fmt.Errorf("%w: the reader crashed; the file may be malformed", ErrUnreadable)
	}
}

// childEnviron is the child's whole environment: the marker, plus what
// Windows needs to start a process. API keys and the rest stay with the
// parent.
func childEnviron(maxText int) []string {
	env := []string{childEnv + "=" + strconv.Itoa(maxText)}
	if root, ok := os.LookupEnv("SYSTEMROOT"); ok {
		env = append(env, "SYSTEMROOT="+root)
	}
	return env
}

// MaybeRunChild returns at once unless Extract started this process; then
// it reads a PDF on stdin, writes its text to stdout, and exits. Call it
// first thing in main, and in TestMain of packages whose tests call
// Extract.
func MaybeRunChild() {
	maxText, ok := os.LookupEnv(childEnv)
	if !ok {
		return
	}
	os.Exit(RunChild(maxText, os.Stdin, os.Stdout, os.Stderr))
}

// RunChild is the child's work, given the value of the marker variable;
// it returns the exit code. MaybeRunChild calls it; a TestMain may call it
// directly.
func RunChild(maxText string, stdin io.Reader, stdout, stderr io.Writer) int {
	limit, err := strconv.Atoi(maxText)
	if err != nil || limit <= 0 {
		fmt.Fprintf(stderr, "invalid %s=%q\n", childEnv, maxText)
		return 1
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, MaxBytes+1))
	if err != nil || len(raw) > MaxBytes {
		fmt.Fprintf(stderr, "read input: larger than %d MiB or unreadable\n", MaxBytes>>20)
		return 1
	}
	text, err := extract(bytes.NewReader(raw), int64(len(raw)), limit)
	switch {
	case errors.Is(err, ErrNoText):
		return exitNoText
	case errors.Is(err, ErrTooLarge):
		return exitTooLarge
	case err != nil:
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := io.WriteString(stdout, text); err != nil {
		return 1
	}
	return 0
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
