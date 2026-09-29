package neocities

import (
	"os"
	"strings"
	"testing"
)

func TestVersionFromFile(t *testing.T) {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(data))
	if want == "" {
		t.Fatal("VERSION is empty")
	}
	if Version != want {
		t.Fatalf("Version %q, VERSION file %q", Version, want)
	}
}
