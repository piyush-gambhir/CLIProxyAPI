package httpwire

import (
	"net/http"
	"reflect"
)

func FlushResponse(w http.ResponseWriter) error {
	for {
		if flusher, ok := w.(interface{ FlushError() error }); ok {
			return flusher.FlushError()
		}
		if flusher, ok := w.(http.Flusher); ok {
			if !isGinResponseWriter(w) {
				flusher.Flush()
				return nil
			}

			w.(interface{ WriteHeaderNow() }).WriteHeaderNow()
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return http.NewResponseController(w).Flush()
		}
		w = unwrapper.Unwrap()
	}
}

func isGinResponseWriter(w http.ResponseWriter) bool {

	t := reflect.TypeOf(w)
	return t != nil && t.Kind() == reflect.Pointer &&
		t.Elem().PkgPath() == "github.com/gin-gonic/gin" && t.Elem().Name() == "responseWriter"
}
