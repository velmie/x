package ipx

import "net/http"

type HTTPRequestReader struct {
	r *http.Request
}

func NewHTTPRequestReader(r *http.Request) *HTTPRequestReader {
	return &HTTPRequestReader{r: r}
}

func (r HTTPRequestReader) GetHeader(headerKey string) string {
	return r.r.Header.Get(headerKey)
}

func (r HTTPRequestReader) GetRemoteAddr() string {
	return r.r.RemoteAddr
}
