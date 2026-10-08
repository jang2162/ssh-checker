// Package webui는 내장한 대시보드(SPA) 정적 파일을 제공한다.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"ssl-checker/internal/lang"
)

// dist는 web/에서 `npm run build`가 만든 결과다. 저장소에는 .keep만 있어서 Node 없이도 go build가 된다.
//
//go:embed all:dist
var embedded embed.FS

// csp는 화면이 같은 출처의 스크립트·스타일만 쓰게 하고, 다른 사이트가 화면을 iframe으로 감싸지 못하게 한다.
const csp = "default-src 'self'; style-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

const placeholderKO = `<!doctype html>
<html lang="ko"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>ssl-checker</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto; padding: 0 1rem">
<h1>화면이 빌드되지 않았습니다</h1>
<p>이 바이너리에는 대시보드가 들어 있지 않습니다. <code>web/</code>에서 <code>npm ci &amp;&amp; npm run build</code>를 실행한 뒤 다시 빌드하세요. Docker 이미지는 빌드할 때 화면을 함께 만듭니다.</p>
<p>API(<code>/api</code>)와 <code>/llms.txt</code>는 화면과 상관없이 동작합니다.</p>
</body></html>
`

const placeholderEN = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>ssl-checker</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto; padding: 0 1rem">
<h1>The dashboard was not built</h1>
<p>This binary does not include the dashboard. Run <code>npm ci &amp;&amp; npm run build</code> in <code>web/</code> and build again. The Docker image builds the dashboard for you.</p>
<p>The API (<code>/api</code>) and <code>/llms.txt</code> work without the dashboard.</p>
</body></html>
`

// Handler는 내장한 화면을 제공하고, 그 밖의 요청은 next로 넘긴다.
func Handler(next http.Handler, l lang.Lang) http.Handler {
	files, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist는 컴파일할 때 내장돼 항상 있다
	}
	return New(next, files, l)
}

// New는 files(Vite 빌드 출력)를 제공하는 핸들러다. 테스트가 가짜 파일 시스템을 넣을 수 있게 Handler와 나눴다.
func New(next http.Handler, files fs.FS, l lang.Lang) http.Handler {
	return &handler{next: next, files: files, lang: l}
}

type handler struct {
	next  http.Handler
	files fs.FS
	lang  lang.Lang // UI_LANGUAGE. index.html의 <html lang>에 넣어, 화면이 이 언어로 뜬다
}

var htmlLangRE = regexp.MustCompile(`<html lang="[A-Za-z-]*"`)

// withLang은 index.html의 <html lang>을 서버가 정한 언어로 바꾼다. 화면은 이 값을 읽어 언어를 정한다.
func withLang(index []byte, l lang.Lang) []byte {
	return htmlLangRE.ReplaceAll(index, []byte(`<html lang="`+string(l)+`"`))
}

// reserved는 서버 API가 맡는 경로다. 화면이 가로채면 안 된다(GET /update의 405 같은 기존 응답이 달라진다).
func reserved(p string) bool {
	switch p {
	case "/update", "/verify", "/register", "/health", "/llms.txt", "/api":
		return true
	}
	return strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/acme/")
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || reserved(r.URL.Path) {
		h.next.ServeHTTP(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	data, err := h.read(name)
	if err != nil {
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r) // 없는 스크립트에 HTML을 돌려주면 브라우저가 구문 오류만 낸다
			return
		}
		// 화면 경로(/domains/3 등)는 SPA가 처리한다.
		name = "index.html"
		if data, err = h.read(name); err != nil {
			servePlaceholder(w, h.lang)
			return
		}
	}
	if name == "index.html" {
		data = withLang(data, h.lang)
	}
	hd := w.Header()
	hd.Set("Content-Security-Policy", csp)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Referrer-Policy", "same-origin")
	if strings.HasPrefix(name, "assets/") {
		hd.Set("Cache-Control", "public, max-age=31536000, immutable") // 파일 이름에 해시가 들어 있다
	} else {
		hd.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// read는 내장 파일 하나를 읽는다. .keep 같은 점 파일과 디렉터리는 없는 것으로 본다.
func (h *handler) read(name string) ([]byte, error) {
	if strings.HasPrefix(path.Base(name), ".") {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(h.files, name)
}

func servePlaceholder(w http.ResponseWriter, l lang.Lang) {
	hd := w.Header()
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(l.Pick(placeholderEN, placeholderKO)))
}
