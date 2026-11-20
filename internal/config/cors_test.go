package config

import "testing"

func TestParseCORSOriginsDefaultAndNone(t *testing.T) {
	got := parseCORSOrigins("")
	if len(got) != 2 || got[0] != "https://pairly.space" {
		t.Fatalf("default=%v", got)
	}
	if parseCORSOrigins("none") != nil {
		t.Fatal("none should disable")
	}
	got = parseCORSOrigins(" https://a.example ,https://b.example/ ")
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("custom=%v", got)
	}
	cfg := Config{CORSOrigins: got}
	if !cfg.AllowsCORSOrigin("https://a.example") || cfg.AllowsCORSOrigin("https://evil.example") {
		t.Fatal("allow check failed")
	}
}
