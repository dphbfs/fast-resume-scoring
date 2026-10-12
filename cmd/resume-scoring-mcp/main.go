// Command resume-scoring-mcp serves score, extract, and check as Model Context Protocol
// tools, over stdio (default) or streamable HTTP.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/cli"
)

// version is set by the release build.
var version = "dev"

// maxRequestBytes caps an HTTP request body: two inputs of
// cli.MaxInputBytes plus a requirements result.
const maxRequestBytes = 8 << 20

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fs := flag.NewFlagSet("resume-scoring-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	transport := fs.String("transport", "stdio", "stdio or http")
	addr := fs.String("addr", "127.0.0.1:8080", "with -transport http: listen address; the endpoint is /mcp")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: resume-scoring-mcp [-transport stdio|http] [-addr host:port]")
		fmt.Fprintln(stderr, "Serves the score, extract, and check tools over MCP. Tool inputs go to the Jev provider at")
		fmt.Fprintln(stderr, "TYPESAFE_BASE_URL. Over HTTP, MCP_HTTP_TOKEN sets a bearer token; it is required when -addr")
		fmt.Fprintln(stderr, "is not a loopback address.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cli.ExitOK
		}
		return cli.ExitUsage
	}
	if fs.NArg() != 0 || (*transport != "stdio" && *transport != "http") {
		fs.Usage()
		return cli.ExitUsage
	}
	token := os.Getenv("MCP_HTTP_TOKEN")
	if *transport == "http" && token == "" && !loopback(*addr) {
		fmt.Fprintf(stderr, "resume-scoring-mcp: %s is not a loopback address: set MCP_HTTP_TOKEN\n", *addr)
		return cli.ExitUsage
	}

	s, err := initServer()
	if err != nil {
		fmt.Fprintln(stderr, "resume-scoring-mcp:", err)
		return cli.ExitError
	}
	srv := s.MCPServer(version)

	if *transport == "stdio" {
		err = srv.Run(ctx, &mcp.StdioTransport{})
	} else {
		err = serveHTTP(ctx, srv, *addr, token, stderr)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "resume-scoring-mcp:", err)
		return cli.ExitError
	}
	return cli.ExitOK
}

func serveHTTP(ctx context.Context, srv *mcp.Server, addr, token string, stderr io.Writer) error {
	mux := http.NewServeMux()
	mux.Handle("/mcp", authorize(token, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok\n") })

	hs := &http.Server{
		Addr:              addr,
		Handler:           http.MaxBytesHandler(mux, maxRequestBytes),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shutdown)
	}()
	fmt.Fprintf(stderr, "resume-scoring-mcp: serving streamable HTTP on http://%s/mcp\n", addr)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// authorize requires "Authorization: Bearer <token>" when token is set.
func authorize(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopback reports whether addr's host is a loopback address or localhost.
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
