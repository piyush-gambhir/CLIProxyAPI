package handlers

import (
	"net/http"
	"strings"
)

var gatewayHeaderPrefixes = []string{
	"x-litellm-",
	"helicone-",
	"x-portkey-",
	"cf-aig-",
	"x-kong-",
	"x-bt-",
}

var hopByHopHeaders = map[string]struct{}{

	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},

	"Set-Cookie": {},

	"Content-Length":   {},
	"Content-Encoding": {},
}

var cpaReservedResponseHeaders = map[string]struct{}{
	"Access-Control-Allow-Credentials": {},
	"Access-Control-Allow-Headers":     {},
	"Access-Control-Allow-Methods":     {},
	"Access-Control-Allow-Origin":      {},
	"Access-Control-Expose-Headers":    {},
	"Access-Control-Max-Age":           {},
	"X-Cpa-Trace-Id":                   {},
}

func IsCPAReservedResponseHeader(name string) bool {
	_, reserved := cpaReservedResponseHeaders[http.CanonicalHeaderKey(name)]
	return reserved
}

func FilterUpstreamHeaders(src http.Header) http.Header {
	if src == nil {
		return nil
	}
	connectionScoped := connectionScopedHeaders(src)
	dst := make(http.Header)
	for key, values := range src {
		canonicalKey := http.CanonicalHeaderKey(key)
		if _, blocked := hopByHopHeaders[canonicalKey]; blocked {
			continue
		}
		if _, reserved := cpaReservedResponseHeaders[canonicalKey]; reserved {
			continue
		}
		if _, scoped := connectionScoped[canonicalKey]; scoped {
			continue
		}

		lowerKey := strings.ToLower(key)
		gatewayMatch := false
		for _, prefix := range gatewayHeaderPrefixes {
			if strings.HasPrefix(lowerKey, prefix) {
				gatewayMatch = true
				break
			}
		}
		if gatewayMatch {
			continue
		}
		dst[key] = values
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func connectionScopedHeaders(src http.Header) map[string]struct{} {
	scoped := make(map[string]struct{})
	for _, rawValue := range src.Values("Connection") {
		for _, token := range strings.Split(rawValue, ",") {
			headerName := strings.TrimSpace(token)
			if headerName == "" {
				continue
			}
			scoped[http.CanonicalHeaderKey(headerName)] = struct{}{}
		}
	}
	return scoped
}

func WriteUpstreamHeaders(dst http.Header, src http.Header) {
	if src == nil {
		return
	}
	for key, values := range src {

		if dst.Get(key) != "" {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}
