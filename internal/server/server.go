// Package server exposes the REST API and the embedded web application.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"revboard/internal/review"
	"revboard/internal/store"
	"revboard/internal/webui"
)

// ClientHeader must be present on every API request (CSRF protection: a
// cross-origin page cannot set it without a CORS pre-flight, which is never approved).
const ClientHeader = "X-GRB-Client"

// Server serves the API and the SPA.
type Server struct {
	store   *store.Store
	review  *review.Service
	version string
	static  fs.FS
}

// New creates a server. static may be nil to use the embedded web application.
func New(st *store.Store, version string, static fs.FS) *Server {
	if static == nil {
		static = webui.FS()
	}
	return &Server{store: st, review: &review.Service{Store: st}, version: version, static: static}
}

// Handler returns the root HTTP handler with its guards.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	mux.Handle("/", s.spaHandler())
	return recoverer(hostCheck(apiHeaderCheck(mux)))
}

// Listen opens a TCP listener on a loopback address.
func Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if !isLoopbackHost(host) {
		return nil, errors.New("refusing to listen on a non-loopback address: " + host)
	}
	return net.Listen("tcp", addr)
}

// Serve serves HTTP on ln until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostCheck rejects requests whose Host header is not a loopback name
// (protection against DNS rebinding).
func hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if !isLoopbackHost(host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiHeaderCheck requires the client header on API requests.
func apiHeaderCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Header.Get(ClientHeader) == "" {
				writeError(w, http.StatusForbidden, "missing "+ClientHeader+" header")
				return
			}
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, v)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// hashedAsset matches the content-hashed bundle names emitted by Angular (main-ABCD1234.js).
var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9]{8}\.(js|css)$`)

// spaHandler serves static files and falls back to index.html for client-side routes.
func (s *Server) spaHandler() http.Handler {
	files := http.FileServerFS(s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "unknown API endpoint")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if _, err := fs.Stat(s.static, name); err == nil {
				if hashedAsset.MatchString(name) {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(s.static, "index.html")
		if err != nil {
			http.Error(w, "web application not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
