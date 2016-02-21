// Copyright (c) 2014-2015 Ludovic Fauvet
// Licensed under the MIT license

package http

import (
	. "github.com/wsnipex/mirrorbits/config"
	"github.com/wsnipex/mirrorbits/http/cgzip"
	"io"
	"net/http"
	"strings"
)

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
	typeGuessed bool
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.typeGuessed {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(b))
		}
		w.typeGuessed = true
	}
	return w.Writer.Write(b)
}

// NewGzipHandler is an HTTP handler used to compress responses if supported by the client
func NewGzipHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !GetConfig().Gzip || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			fn(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz, _ := cgzip.NewWriterLevel(w, cgzip.Z_BEST_SPEED)
		defer gz.Close()
		fn(&gzipResponseWriter{Writer: gz, ResponseWriter: w}, r)
	}
}
