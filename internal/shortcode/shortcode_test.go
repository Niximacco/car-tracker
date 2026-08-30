package shortcode

import "testing"

func TestNewHonoursLen(t *testing.T) {
	for _, length := range []int{1, 4, 6, 12, 32} {
		if got := New(Len(length)); len(got) != length {
			t.Errorf("New(Len(%d)) = %q, want length %d", length, got, len(got))
		}
	}
}

func TestNewDefaultsWhenLengthIsUnusable(t *testing.T) {
	for _, length := range []int{0, -1} {
		if got := New(Len(length)); len(got) != default_len {
			t.Errorf("New(Len(%d)) = %q, want length %d", length, got, default_len)
		}
	}
}

func TestNewAvoidsConfusableCharacters(t *testing.T) {
	// 200 codes of length 6 is 1200 characters, enough that a confusable
	// character left in the alphabet would show up.
	for i := 0; i < 200; i++ {
		code := New()
		for _, character := range code {
			switch character {
			case '0', 'O', 'I', 'L', '1':
				t.Fatalf("New() = %q, which contains confusable %q", code, character)
			}
		}
	}
}

func TestNewIsNotConstant(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		seen[New(Len(8))] = true
	}

	if len(seen) < 45 {
		t.Errorf("50 codes produced only %d distinct values", len(seen))
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"ABC123", true},
		{"with-dash", true},
		{"with_underscore", true},
		{"", false},
		{"has space", false},
		{"has/slash", false},
		{"has?query", false},
		{"../escape", false},
	}

	for _, test := range tests {
		if got := Valid(test.code); got != test.want {
			t.Errorf("Valid(%q) = %v, want %v", test.code, got, test.want)
		}
	}
}
