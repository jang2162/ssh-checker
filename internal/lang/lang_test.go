package lang

import "testing"

func TestParse(t *testing.T) {
	for in, want := range map[string]Lang{"": EN, "en": EN, "EN": EN, " ko ": KO, "KO": KO} {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"fr", "korean", "en-US"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestPickAndPlaceholder(t *testing.T) {
	if EN.Pick("a", "b") != "a" || KO.Pick("a", "b") != "b" || Lang("xx").Pick("a", "b") != "a" {
		t.Error("Pick")
	}
	if EN.UnknownBaseURL() == KO.UnknownBaseURL() {
		t.Error("placeholders should differ by language")
	}
}
