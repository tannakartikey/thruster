package internal

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
)

func NewProxyHandler(targetUrl *url.URL, badGatewayPage string) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(targetUrl)
			r.Out.Host = r.In.Host
			r.Out.Header["X-Forwarded-For"] = r.In.Header["X-Forwarded-For"]
			r.SetXForwarded()
		},
		ErrorHandler: ProxyErrorHandler(badGatewayPage),
		Transport:    createProxyTransport(),
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filePath := filepath.Join("public", "cached_pages", path.Clean(r.URL.Path)) + ".html"
		if _, err := os.Stat(filePath); err == nil {
			slog.Info("******** serving from page cache", "path", filePath)
			http.ServeFile(w, r, filePath)
			return
		} else {
			slog.Info("******** no page cache found", "path", filePath)
		}
		proxy.ServeHTTP(w, r)
	})
}

func ProxyErrorHandler(badGatewayPage string) func(w http.ResponseWriter, r *http.Request, err error) {
	content, err := os.ReadFile(badGatewayPage)
	if err != nil {
		slog.Debug("No custom 502 page found", "path", badGatewayPage)
		content = nil
	}

	return func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Info("Unable to proxy request", "path", r.URL.Path, "error", err)

		if isRequestEntityTooLarge(err) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}

		if content != nil {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadGateway)
			w.Write(content)
		} else {
			w.WriteHeader(http.StatusBadGateway)
		}
	}
}

func isRequestEntityTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

func createProxyTransport() *http.Transport {
	// The default transport requests compressed responses even if the client
	// didn't. If it receives a compressed response but the client wants
	// uncompressed, the transport decompresses the response transparently.
	//
	// Although that seems helpful, it doesn't play well with X-Sendfile
	// responses, as it may result in us being handed a reference to a file on
	// disk that is already compressed, and we'd have to similarly decompress it
	// before serving it to the client. This is wasteful, especially since there
	// was probably an uncompressed version of it on disk already. It's also a bit
	// fiddly to do on the fly without the ability to seek around in the
	// uncompressed content.
	//
	// Compression between us and the upstream server is likely to be of limited
	// use anyway, since we're only proxying from localhost. Given that fact --
	// and the fact that most clients *will* request compressed responses anyway,
	// which makes all of this moot -- our best option is to disable this
	// on-the-fly compression.

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true

	return transport
}
