package domain

import "net/http"

type APIRequest struct {
	Method string
	URL    string
	Header      http.Header
	QueryParams map[string]string
	PathParams  map[string]string
	Body        []byte
}
