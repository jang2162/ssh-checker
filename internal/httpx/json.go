// Package httpx는 API 서버가 공통으로 쓰는 HTTP 도우미를 모은다.
package httpx

import (
	"encoding/json"
	"net/http"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError는 acme-dns와 같은 {"error": "<code>"} 형태로 응답한다.
func WriteError(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, map[string]string{"error": code})
}
