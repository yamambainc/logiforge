package api

import (
	"compress/gzip"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/yamambainc/hypothesis-verification-harness/internal/backend"
)

// Options は、logiforge serve の、API のほかに配るものである。
type Options struct {
	// OAuth は、WebUI のサインインの中継の設定である。nil なら、/v1/oauth/ は not_found を返す。
	OAuth *OAuth
	// WebUI は、ビルドした WebUI（index.html と assets/）である。nil なら、WebUI を配らない。
	WebUI fs.FS
}

// WebUICSP は、WebUI の応答に付ける CSP である（design/webui/security.md）。
const WebUICSP = "default-src 'self'; img-src 'self' data: blob:; style-src 'self'; frame-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"

// MermaidCSP は、Mermaid を描く文書（mermaid.html）だけに付ける CSP である（design/webui/markdown.md）。
// Mermaid は描く途中でインラインの style を使うので、この文書だけ style-src に 'unsafe-inline' を足す。
// この文書は、sandbox の iframe（allow-same-origin なし）で開く。CSP の sandbox allow-scripts で、URL を直接開かれても、
// オパークなオリジンで動く（アプリのオリジンとストレージに触れない）。通信もしない（connect-src は default-src の 'none'）。
const MermaidCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; sandbox allow-scripts"

// mermaidPage は、Mermaid を描く文書のパスである。
const mermaidPage = "mermaid.html"

// NewServer は、logiforge serve の HTTP ハンドラーを返す。
// /v1/ と /healthz は API、それ以外は WebUI である。すべての応答に nosniff を付け、gzip を受け付ける要求には gzip で返す。
func NewServer(b *backend.Backend, log *slog.Logger, opt Options) http.Handler {
	api := NewHandler(b, log)
	mux := http.NewServeMux()
	mux.Handle("/v1/", api)
	mux.Handle("/healthz", api)
	if o := opt.OAuth; o != nil {
		mux.HandleFunc("GET /v1/oauth/config", o.config)
		mux.HandleFunc("POST /v1/oauth/token", o.token)
	}
	if opt.WebUI != nil {
		mux.Handle("/", webUI(opt.WebUI))
	}
	return withHeaders(withGzip(mux))
}

// withHeaders は、すべての応答に X-Content-Type-Options: nosniff を付ける。
func withHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h.ServeHTTP(w, r)
	})
}

// webUI は、ビルドした WebUI を配る。ファイルがなければ、index.html を返す（SPA の画面のパス）。
// ハッシュ付きの assets/ は immutable で長くキャッシュさせ、index.html はキャッシュさせない（design/webui/stack.md の「配り方」）。
func webUI(root fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", WebUICSP)
		h.Set("Referrer-Policy", "no-referrer")
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == mermaidPage {
			h.Set("Content-Security-Policy", MermaidCSP)
		}
		if name != "" && name != "index.html" && !strings.HasPrefix(path.Base(name), ".") {
			if data, err := fs.ReadFile(root, name); err == nil {
				if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
					h.Set("Content-Type", ct)
				}
				if strings.HasPrefix(name, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
					// sandbox の iframe（オパークなオリジン）の中の mermaid.html が、モジュールのスクリプトを読めるように。中身は公開してよい静的なファイルだけである
					h.Set("Access-Control-Allow-Origin", "*")
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				_, _ = w.Write(data)
				return
			}
			// assets/ の中にないファイルは、index.html に置き換えない（古い版の JS を求められたときに、HTML を返さないため）。
			if strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		data, err := fs.ReadFile(root, "index.html")
		if err != nil {
			h.Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "WebUI がビルドされていない（webui/ で npm run build）\n")
			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
}

// gzip で返す Content-Type。artifact と添付（octet-stream）は、もとのまま返す。
func compressible(ct string) bool {
	mt, _, _ := mime.ParseMediaType(ct)
	switch {
	case mt == "application/json", mt == "application/problem+json", mt == "text/markdown",
		mt == "text/html", mt == "text/css", mt == "text/javascript", mt == "application/javascript", mt == "image/svg+xml":
		return true
	}
	return false
}

var gzPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression); return w }}

// withGzip は、要求が Accept-Encoding: gzip を持ち、応答が compressible なら、gzip で返す。
func withGzip(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !acceptsGzip(r) || r.Method == http.MethodHead {
			h.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		h.ServeHTTP(gw, r)
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") && strings.TrimSpace(q) != "q=0" {
			return true
		}
	}
	return false
}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	h := g.Header()
	if h.Get("Content-Encoding") == "" && compressible(h.Get("Content-Type")) {
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		g.gz = gzPool.Get().(*gzip.Writer)
		g.gz.Reset(g.ResponseWriter)
	}
}

func (g *gzipWriter) WriteHeader(status int) {
	// 本文を持てない応答（1xx、204、304）は、gzip にしない
	if status < 200 || status == http.StatusNoContent || status == http.StatusNotModified {
		g.decided = true
	}
	g.decide()
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.decide()
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzPool.Put(g.gz)
		g.gz = nil
	}
}
