package slug

import "testing"

func TestMakeProducesUrlSafeKeys(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"Dallas Stars", "dallas-stars"},
		{"  Saint Louis Blues  ", "saint-louis-blues"},
		{"Vegas Knights (Preseason)", "vegas-knights-preseason"},
		{"2025-26", "2025-26"},
		{"Montréal Canadiens", "montreal-canadiens"},
		{"!!!", ""},
	}

	for _, test := range cases {
		if got := Make(test.text); got != test.want {
			t.Errorf("Make(%q) = %q, want %q", test.text, got, test.want)
		}
	}
}

func TestMakeStaysWithinTheKeyLimit(t *testing.T) {
	long := ""
	for range 40 {
		long += "opponent "
	}

	if got := Make(long); len(got) > MaxLength {
		t.Errorf("Make produced a %d character slug, want at most %d", len(got), MaxLength)
	}
}
