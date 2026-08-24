package state

import (
	"os"
	"strings"
	"testing"
)

func TestEnsureCreatesStablePrivateState(t *testing.T) {
	paths, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := Ensure(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected first initialization to create secrets")
	}
	second, created, err := Ensure(paths)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected second initialization to reuse secrets")
	}
	if first != second {
		t.Fatal("secrets changed across initialization")
	}
	if err = ValidatePermissions(paths); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	for _, want := range []string{"host: \"127.0.0.1\"", "allow-remote: false", "usage-statistics-enabled: false", first.APIKey} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated config is missing %q", want)
		}
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "::1", " LOCALHOST "} {
		if !IsLoopbackHost(host) {
			t.Fatalf("expected %q to be allowed", host)
		}
	}
	for _, host := range []string{"", "0.0.0.0", "192.168.1.10"} {
		if IsLoopbackHost(host) {
			t.Fatalf("expected %q to be rejected", host)
		}
	}
}
