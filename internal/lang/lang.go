// Package lang는 사용자에게 보이는 글(스크립트, 대시보드, llms.txt, 판정 코드 설명)의 언어를 정한다.
package lang

import (
	"fmt"
	"strings"
)

// Lang은 UI 언어다. 서버를 띄울 때 UI_LANGUAGE로 한 번 정하고 실행 중에는 바뀌지 않는다.
type Lang string

const (
	EN Lang = "en"
	KO Lang = "ko"
)

// Default는 UI_LANGUAGE를 정하지 않았을 때의 언어다.
const Default = EN

// Parse는 UI_LANGUAGE 값을 읽는다. 비어 있으면 Default다.
func Parse(s string) (Lang, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return Default, nil
	case "en":
		return EN, nil
	case "ko":
		return KO, nil
	}
	return Default, fmt.Errorf("UI_LANGUAGE must be en or ko, got %q", s)
}

// Pick은 언어에 맞는 글을 고른다. 모르는 언어는 영어다.
func (l Lang) Pick(en, ko string) string {
	if l == KO {
		return ko
	}
	return en
}

// UnknownBaseURL은 서버 주소를 알 수 없을 때(PUBLIC_URL이 없고 Host도 쓸 수 없을 때) 안내에 넣는 자리표시자다.
func (l Lang) UnknownBaseURL() string {
	return l.Pick("<ssl-checker address>", "<ssl-checker 주소>")
}
