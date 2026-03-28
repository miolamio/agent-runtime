// internal/proxy/forward.go
package proxy

import (
	"io"
	"net/http"
	"strings"
	"time"
)

var forwardClient = &http.Client{
	Timeout: 5 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// ForwardRequest proxies a request to the upstream provider.
// Only x-api-key, User-Agent, and Host are modified; everything else passes through.
func ForwardRequest(w http.ResponseWriter, r *http.Request, baseURL, apiKey, userAgent string) {
	upstreamURL := strings.TrimRight(baseURL, "/") + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		http.Error(w, `{"error":{"message":"proxy error"}}`, http.StatusBadGateway)
		return
	}

	// Copy all headers from incoming request
	for key, vals := range r.Header {
		for _, v := range vals {
			upReq.Header.Add(key, v)
		}
	}
	// Override only these
	upReq.Header.Set("x-api-key", apiKey)
	upReq.Header.Set("User-Agent", userAgent)

	resp, err := forwardClient.Do(upReq)
	if err != nil {
		http.Error(w, `{"error":{"message":"upstream unreachable"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers (filter out key leaks)
	for key, vals := range resp.Header {
		lk := strings.ToLower(key)
		if lk == "x-api-key" || lk == "authorization" {
			continue
		}
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Stream response body
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		flusher, ok := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				if ok { flusher.Flush() }
			}
			if err != nil { break }
		}
	} else {
		io.Copy(w, resp.Body)
	}
}
