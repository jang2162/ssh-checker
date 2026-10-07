// Package llmstxt는 에이전트가 읽는 /llms.txt를 만든다. 이 서버의 실제 주소와 챌린지 존이 채워진다.
package llmstxt

import (
	"bytes"
	_ "embed"
	"net/http"
	"text/template"

	"ssl-checker/internal/monitor"
)

//go:embed llms.txt.tmpl
var source string

var tmpl = template.Must(template.New("llms.txt").Parse(source))

// 서버 주소를 알 수 없을 때(PUBLIC_URL이 없고 Host도 쓸 수 없을 때) 문서에 넣는 자리표시자.
const unknownBaseURL = "<ssl-checker 주소>"

// Data는 템플릿에 채우는 값이다.
type Data struct {
	BaseURL string // 비어 있으면 자리표시자를 쓴다
	Zone    string // CHALLENGE_ZONE
	Codes   []monitor.CodeInfo
}

// Render는 /llms.txt 본문이다.
func Render(d Data) (string, error) {
	if d.BaseURL == "" {
		d.BaseURL = unknownBaseURL
	}
	if d.Codes == nil {
		d.Codes = monitor.Codes()
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Handler는 GET /llms.txt다. baseURL은 요청에서 서버 주소를 만드는 함수다(PUBLIC_URL 우선).
func Handler(zone string, baseURL func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := Render(Data{BaseURL: baseURL(r), Zone: zone})
		if err != nil {
			http.Error(w, "llms.txt render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache") // PUBLIC_URL이 없으면 요청마다 주소가 달라질 수 있다
		_, _ = w.Write([]byte(body))
	})
}
