package money

import "testing"

func TestParseReadsWhatAPersonTypes(t *testing.T) {
	cases := []struct {
		typed string
		want  int64
	}{
		{"", 0},
		{"0", 0},
		{"129.57", 12957},
		{"$129.57", 12957},
		{"1,299", 129900},
		{"$5,700.88", 570088},
		{"  $12.5  ", 1250},
		{".75", 75},
		{"-40.00", -4000},
		// More precision than money has is dropped rather than refused.
		{"10.999", 1099},
	}

	for _, test := range cases {
		got, err := Parse(test.typed)
		if err != nil {
			t.Errorf("Parse(%q) returned %s", test.typed, err.Error())
			continue
		}

		if got != test.want {
			t.Errorf("Parse(%q) = %d, want %d", test.typed, got, test.want)
		}
	}
}

// A price that cannot be read has to say so. Reading it as zero would quietly
// mark a pair of tickets as free.
func TestParseRefusesWhatIsNotAnAmount(t *testing.T) {
	for _, typed := range []string{"free", "12x", "$", "1.2.3", "--5"} {
		if _, err := Parse(typed); err == nil {
			t.Errorf("Parse(%q) was accepted, want an error", typed)
		}
	}
}

func TestFormatWritesDollars(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "$0.00"},
		{5, "$0.05"},
		{12957, "$129.57"},
		{570088, "$5,700.88"},
		{100000000, "$1,000,000.00"},
		{-4000, "-$40.00"},
	}

	for _, test := range cases {
		if got := Format(test.cents); got != test.want {
			t.Errorf("Format(%d) = %q, want %q", test.cents, got, test.want)
		}
	}
}

// Whatever Input writes has to read back as the same amount, or a form saved
// without being touched would change the number it was showing.
func TestInputRoundTrips(t *testing.T) {
	for _, cents := range []int64{0, 1, 999, 12957, 570088, -4000} {
		typed := Input(cents)
		got, err := Parse(typed)
		if err != nil {
			t.Fatalf("Parse(Input(%d)) = %q returned %s", cents, typed, err.Error())
		}

		if got != cents {
			t.Errorf("Parse(Input(%d)) = %d, want %d", cents, got, cents)
		}
	}
}

func TestBlankLeavesNothingForNothing(t *testing.T) {
	if got := Blank(0); got != "" {
		t.Errorf("Blank(0) = %q, want empty", got)
	}

	if got := Blank(12957); got != "$129.57" {
		t.Errorf("Blank(12957) = %q, want $129.57", got)
	}
}
