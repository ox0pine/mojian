package main

import "testing"

func TestShouldDetach(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stdinTTY bool
		dev      bool
		env      string
		want     bool
	}{
		{"terminal launch", true, false, "", true},
		{"piped or redirected stdin", false, false, "", false},
		{"mygo dev", true, true, "", false},
		{"already detached", true, false, "1", false},
	} {
		t.Setenv(detachedEnv, tc.env)
		if got := shouldDetach(tc.stdinTTY, tc.dev); got != tc.want {
			t.Errorf("%s: shouldDetach(%v, %v) = %v, want %v", tc.name, tc.stdinTTY, tc.dev, got, tc.want)
		}
	}
}
