// Package llmstxt는 에이전트가 읽는 /llms.txt를 만든다. 이 서버의 실제 주소와 챌린지 존이 채워진다.
package llmstxt

import (
	"bytes"
	_ "embed"
	"net/http"
	"text/template"

	"ssl-checker/internal/lang"
	"ssl-checker/internal/monitor"
)

//go:embed llms.txt.tmpl
var sourceKO string

//go:embed llms.en.txt.tmpl
var sourceEN string

// 언어마다 템플릿 하나다. 두 파일은 같은 구조를 가져야 한다(테스트가 {{ }} 지시어와 절 개수를 비교한다).
var templates = map[lang.Lang]*template.Template{
	lang.KO: template.Must(template.New("llms.ko").Parse(sourceKO)),
	lang.EN: template.Must(template.New("llms.en").Parse(sourceEN)),
}

// Data는 템플릿에 채우는 값이다.
type Data struct {
	BaseURL string // 비어 있으면 자리표시자를 쓴다
	Zone    string // CHALLENGE_ZONE
	Lang    lang.Lang
	Codes   []monitor.CodeInfo
}

// Render는 /llms.txt 본문이다. Lang이 비어 있으면 영어다.
func Render(d Data) (string, error) {
	if d.Lang == "" {
		d.Lang = lang.Default
	}
	if d.BaseURL == "" {
		d.BaseURL = d.Lang.UnknownBaseURL()
	}
	if d.Codes == nil {
		d.Codes = monitor.Codes(d.Lang)
	}
	tmpl, ok := templates[d.Lang]
	if !ok {
		tmpl = templates[lang.Default]
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Handler는 GET /llms.txt다. baseURL은 요청에서 서버 주소를 만드는 함수다(PUBLIC_URL 우선).
func Handler(zone string, l lang.Lang, baseURL func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := Render(Data{BaseURL: baseURL(r), Zone: zone, Lang: l})
		if err != nil {
			http.Error(w, "llms.txt render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache") // PUBLIC_URL이 없으면 요청마다 주소가 달라질 수 있다
		_, _ = w.Write([]byte(body))
	})
}
