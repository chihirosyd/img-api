package config

import "testing"

func TestParseWhitelist(t *testing.T) {
	got := parseWhitelist(" a.com, blog.b.com ,,c.com ")
	want := []string{"a.com", "blog.b.com", "c.com"}

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if parseWhitelist("") != nil {
		t.Error("empty string should return nil")
	}
	if parseWhitelist(" , , ") != nil {
		t.Error("whitespace-only string should return nil")
	}
}
