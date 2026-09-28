package neocities

import "testing"

func TestFnmatch(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*", "", true},
		{"*", ".hidden", false},
		{"*.txt", ".txt", false},
		{"*.txt", "a.txt", true},
		{"*.txt", "dir/a.txt", true},
		{".*", ".git", true},
		{"dir/**", "dir", false},
		{"dir/**", "dir/a", true},
		{"dir**", "dir", true},
		{"dir**", "dir/a/b", true},
		{"secret.txt", "secret.txt", true},
		{"secret.txt", "dir/secret.txt", false},
		{"skipdir**", "skipdir", true},
		{"skipdir**", "skipdir/hidden.txt", true},
		{"*.txt", "dir/.txt", true},
		{"foo?", "foo", false},
		{"foo?", "foox", true},
		{"foo?", "foo/", true},
		{"[abc]", "b", true},
		{"[abc]", "d", false},
		{"[!abc]", "d", true},
		{`file\*`, "file*", true},
		{`file\*`, "filea", false},
		{"**/*", "a/b", true},
		{"**/*", ".a/b", false},
		{"**/*", "secret.txt", false},
		{"a*", "a.b", true},
		{"*", "foo/.hidden", true},
		{"", "foo", false},
		{"*.html", "index.html", true},
		{"vendor/**", "vendor/pkg/a.go", true},
		{"*.go", "cmd/neocities/main.go", true},
		{"?", ".", false},
		{"?", "a", true},
		{"[abc]", ".", false},
		{"[.]", ".", false},
		{`\.`, ".", true},
		{".git/**", ".git/config", true},
		{".git**", ".git", true},
		{"**", "foo/bar", true},
		{"**", ".foo", false},
		{"foo[abc]", "foob", true},
		{"a[b-d]e", "ace", true},
		{"a[b-d]e", "aee", false},
		{"[-a]", "-", true},
		{"[a-]", "-", true},
		{"node_modules**", "node_modules/foo/bar.js", true},
	}
	for _, tc := range cases {
		if got := fnmatch(tc.pattern, tc.name); got != tc.want {
			t.Errorf("fnmatch(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}
