package mail

import "testing"

func TestParseAddress(t *testing.T) {
	cases := []struct {
		in   string
		want Address
	}{
		{"21:3/194", Address{Zone: 21, Net: 3, Node: 194}},
		{"21:3/194.1", Address{Zone: 21, Net: 3, Node: 194, Point: 1}},
		{"1:234/56", Address{Zone: 1, Net: 234, Node: 56}},
		{"1:234/56@fidonet", Address{Zone: 1, Net: 234, Node: 56}},
		{" 21:3/194 ", Address{Zone: 21, Net: 3, Node: 194}},
	}
	for _, c := range cases {
		got, err := ParseAddress(c.in)
		if err != nil {
			t.Errorf("ParseAddress(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseAddress(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseAddressErrors(t *testing.T) {
	cases := []string{
		"",
		"3/194",
		"21:194",
		"21:3/abc",
		"abc:3/194",
		"21:3/194.abc",
	}
	for _, in := range cases {
		if _, err := ParseAddress(in); err == nil {
			t.Errorf("ParseAddress(%q): want error, got nil", in)
		}
	}
}

func TestAddressString(t *testing.T) {
	if got := (Address{Zone: 21, Net: 3, Node: 194}).String(); got != "21:3/194" {
		t.Errorf("String() = %q, want %q", got, "21:3/194")
	}
	if got := (Address{Zone: 21, Net: 3, Node: 194, Point: 1}).String(); got != "21:3/194.1" {
		t.Errorf("String() = %q, want %q", got, "21:3/194.1")
	}
}

func TestAddressIsZero(t *testing.T) {
	if !(Address{}).IsZero() {
		t.Error("zero Address: IsZero() = false, want true")
	}
	if (Address{Zone: 1}).IsZero() {
		t.Error("Address{Zone: 1}: IsZero() = true, want false")
	}
}

func TestAddressRoundTripThroughString(t *testing.T) {
	for _, a := range []Address{
		{Zone: 21, Net: 3, Node: 194},
		{Zone: 1, Net: 234, Node: 56, Point: 7},
	} {
		got, err := ParseAddress(a.String())
		if err != nil {
			t.Fatalf("ParseAddress(%q): %v", a.String(), err)
		}
		if got != a {
			t.Errorf("round trip %v -> %q -> %v", a, a.String(), got)
		}
	}
}
