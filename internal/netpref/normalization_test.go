package netpref

import "testing"

func TestQueryNormalizationIdempotentRootDot(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"API.Example.Com.", "api.example.com"},
		{"api.example.com", "api.example.com"},
		{"0..", "0.."}, {"API..", "api.."},
		{"api...", "api..."}, {"api ", "api "},
		{".", ""}, {"..", ".."}, {"", ""},
	} {
		got := normalizeQueryName(tc.input)
		if got != tc.want || normalizeQueryName(got) != got {
			t.Fatalf("normalize %q: got %q, twice %q, want %q", tc.input, got, normalizeQueryName(got), tc.want)
		}
	}
}

func TestDomainSelectorsRejectRepeatedRootDots(t *testing.T) {
	c := Config{DomainSets: []DomainSet{{ID: "set", Domains: []string{"api.test", "*.example.test"}}}}
	c.compileDomainSets()
	for _, name := range []string{"api.test..", "api.test...", "example.test..", "sub.example.test.."} {
		for _, pattern := range []string{"api.test", "*.example.test"} {
			if score, _ := selectorMatch(&c, pattern, "", name); score >= 0 {
				t.Fatalf("direct selector %q accepted %q", pattern, name)
			}
		}
		if score, _ := selectorMatch(&c, "", "set", name); score >= 0 {
			t.Fatalf("set selector accepted %q", name)
		}
	}
	for _, name := range []string{"API.TEST.", "example.test.", "sub.example.test."} {
		if score, _ := selectorMatch(&c, "", "set", name); score < 0 {
			t.Fatalf("valid rooted query rejected: %q", name)
		}
	}
}
