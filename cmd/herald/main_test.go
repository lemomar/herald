package main

import "testing"

func TestSanitizePrevCommand(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"false", "false"},
		{"echo hey", "echo"},
		{"false; herald", "false"},
		{"echo hey; false; herald", "false"},
		{"echo hey; false", "false"},
		{"./herald", ""},
		{"herald --evaluate", ""},
		{"", ""},
	}

	for _, tc := range cases {
		if got := sanitizePrevCommand(tc.in); got != tc.want {
			t.Fatalf("sanitizePrevCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
