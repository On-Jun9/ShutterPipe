package web

import (
	"net/http"
	"net/url"
	"strings"
)

func isSameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}

	requestScheme := "http"
	if r.TLS != nil {
		requestScheme = "https"
	}

	return strings.EqualFold(parsed.Scheme, requestScheme) &&
		strings.EqualFold(parsed.Host, r.Host)
}

func requireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if !isSameOriginRequest(r) {
				writeAPIError(w, http.StatusForbidden, "cross-origin request denied")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
