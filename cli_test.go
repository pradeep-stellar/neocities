package neocities

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestApp(t *testing.T, args ...string) (*app, *fakeAPI, *bytes.Buffer) {
	t.Helper()
	api := newFakeAPI()
	srv := api.server(t)
	var buf bytes.Buffer
	home := t.TempDir()
	wd := t.TempDir()
	a := newApp(args, strings.NewReader(""), &buf)
	a.home = func() (string, error) { return home, nil }
	a.goos = "darwin"
	a.getwd = func() (string, error) { return wd, nil }
	a.getenv = func(string) string { return "" }
	a.now = func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("EST", -5*3600)) }
	a.randInt = func(int) int { return 0 }
	a.newClient = func(opts Options) (*Client, error) {
		if opts.BaseURL == "" {
			opts.BaseURL = srv.URL + "/api/"
		}
		c, err := NewClient(opts)
		if c != nil {
			c.fileBase = srv.URL + "/"
		}
		return c, err
	}
	return a, api, &buf
}

func (a *app) withKey(key string) {
	prev := a.getenv
	a.getenv = func(k string) string {
		if k == "NEOCITIES_API_KEY" {
			return key
		}
		if prev != nil {
			return prev(k)
		}
		return ""
	}
}

func TestVersionAndHelp(t *testing.T) {
	a, _, buf := newTestApp(t, "version")
	if code := a.run(); code != 0 {
		t.Fatalf("code %d", code)
	}
	if buf.String() != Version+"\n" {
		t.Fatalf("version %q", buf.String())
	}

	a, _, buf = newTestApp(t)
	if code := a.run(); code != 0 {
		t.Fatalf("code %d", code)
	}
	out := buf.String()
	for _, want := range []string{"Subcommands:", "push", "upload", "pull", "pizza", "Neocities"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q\n%s", want, out)
		}
	}

	a, _, buf = newTestApp(t, "help", "push")
	if code := a.run(); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(buf.String(), "--prune") || !strings.Contains(buf.String(), "--dry-run") {
		t.Fatalf("push help %s", buf.String())
	}

	a, _, buf = newTestApp(t, "nope")
	if code := a.run(); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(buf.String(), "Subcommands:") {
		t.Fatalf("unknown command %s", buf.String())
	}
}

func TestConfigPaths(t *testing.T) {
	a := &app{
		goos:   "darwin",
		getenv: func(string) string { return "" },
		home:   func() (string, error) { return "/Users/me", nil },
	}
	dir, err := a.configDir()
	if err != nil || dir != filepath.Join("/Users/me", "Library", "Application Support", "neocities") {
		t.Fatalf("darwin %s %v", dir, err)
	}

	a.goos = "linux"
	a.getenv = func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return "/xdg"
		}
		return ""
	}
	dir, err = a.configDir()
	if err != nil || dir != filepath.Join("/xdg", "neocities") {
		t.Fatalf("xdg %s %v", dir, err)
	}

	a.getenv = func(string) string { return "" }
	a.home = func() (string, error) { return "/home/me", nil }
	dir, err = a.configDir()
	if err != nil || dir != filepath.Join("/home/me", ".config", "neocities") {
		t.Fatalf("linux %s %v", dir, err)
	}

	a.goos = "windows"
	a.getenv = func(k string) string {
		if k == "LOCALAPPDATA" {
			return `C:\Users\me\AppData\Local`
		}
		return ""
	}
	dir, err = a.configDir()
	if err != nil || dir != filepath.Join(`C:\Users\me\AppData\Local`, "neocities") {
		t.Fatalf("windows %s %v", dir, err)
	}
}

func TestLoginAndList(t *testing.T) {
	a, api, buf := newTestApp(t, "list", "/")
	a.prompt = &scriptPrompt{asks: []string{"penelope"}, masks: []string{"secret"}}
	api.list = `{"result":"success","files":[{"path":"index.html","is_directory":false,"size":5,"updated_at":"2013-12-05 01:28:24 -0800"},{"path":"css","is_directory":true,"updated_at":"2013-12-05 01:28:24 -0800"}]}`
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "has been stored") || !strings.Contains(out, "index.html") || !strings.Contains(out, "css") {
		t.Fatalf("output %s", out)
	}
	if !strings.HasPrefix(api.auth[0], "Basic ") {
		t.Fatalf("first auth %q", api.auth[0])
	}
	if api.auth[len(api.auth)-1] != "Bearer server-key" {
		t.Fatalf("list auth %q", api.auth[len(api.auth)-1])
	}
	cfg, err := a.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "server-key" || cfg.Sitename != "penelope" {
		t.Fatalf("config %#v", cfg)
	}
	path, err := a.configPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestListDetailAndInfo(t *testing.T) {
	a, api, buf := newTestApp(t, "list", "-d", "/css")
	a.withKey("k")
	api.list = `{"result":"success","files":[{"path":"css/a.css","is_directory":false,"size":12,"updated_at":"2013-12-05 01:28:24 -0800"}]}`
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "css/a.css") || !strings.Contains(buf.String(), "12") {
		t.Fatalf("list %s", buf.String())
	}
	if api.listQuery[0] != "path=%2Fcss" {
		t.Fatalf("query %q", api.listQuery[0])
	}

	a, _, buf = newTestApp(t, "list", "-a")
	a.withKey("k")
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}

	a, _, buf = newTestApp(t, "info")
	a.withKey("k")
	if err := a.saveConfig(&storedConfig{APIKey: "k", Sitename: "penelope"}); err != nil {
		t.Fatal(err)
	}
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	out := buf.String()
	when, err := parseTime("2013-12-05 01:28:24 -0800")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"penelope", "art, fun", when.Local().Format("2006-01-02 15:04:05 -0700")} {
		if !strings.Contains(out, want) {
			t.Fatalf("info missing %q\n%s", want, out)
		}
	}
}

func TestUploadDeleteLogout(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "img.jpg")
	if err := os.WriteFile(img, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "images")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	a, api, buf := newTestApp(t, "upload", "-d", "images", img, nested)
	a.withKey("k")
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if string(api.uploads["/images/img.jpg"]) != "jpeg" {
		t.Fatalf("uploads %#v\n%s", api.uploads, buf.String())
	}
	if !strings.Contains(buf.String(), "is a directory, skipping") {
		t.Fatalf("dir skip %s", buf.String())
	}

	a, api, buf = newTestApp(t, "delete", "old.txt")
	a.withKey("k")
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if strings.Join(api.deletes[0], ",") != "old.txt" {
		t.Fatalf("deletes %#v", api.deletes)
	}
	if !strings.Contains(buf.String(), "SUCCESS:") {
		t.Fatalf("delete output %s", buf.String())
	}

	a, _, buf = newTestApp(t, "logout", "-y")
	a.withKey("k")
	if err := a.saveConfig(&storedConfig{APIKey: "k", Sitename: "penelope"}); err != nil {
		t.Fatal(err)
	}
	path, err := a.configPath()
	if err != nil {
		t.Fatal(err)
	}
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config still there: %v", err)
	}
}

func TestPushDryRunGitignoreAndPrune(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "index.html"), "home")
	mustWrite(t, filepath.Join(root, "sub", "a.txt"), "aaa")
	mustWrite(t, filepath.Join(root, "secret.txt"), "nope")
	mustWrite(t, filepath.Join(root, "skipdir", "hidden.txt"), "hidden")
	mustWrite(t, filepath.Join(root, ".git", "config"), "git")
	mustWrite(t, filepath.Join(root, "vendor", "pkg", "a.txt"), "vend")
	mustWrite(t, filepath.Join(root, ".gitignore"), "secret.txt\nskipdir\n")

	a, api, buf := newTestApp(t, "push", "--dry-run", "-e", "vendor", root)
	a.withKey("k")
	api.hashHit["index.html"] = true
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "dry run") || !strings.Contains(out, "Not pushing .gitignore") {
		t.Fatalf("push output %s", out)
	}
	if !strings.Contains(out, "EXISTS") || !strings.Contains(out, "SUCCESS") {
		t.Fatalf("push status %s", out)
	}
	got := map[string]bool{}
	for _, h := range api.hashes {
		got[h] = true
	}
	for _, want := range []string{"index.html", "sub/a.txt"} {
		if !got[want] {
			t.Fatalf("missing hash %s in %#v", want, api.hashes)
		}
	}
	for _, banned := range []string{"secret.txt", "skipdir/hidden.txt", ".git/config", "vendor/pkg/a.txt"} {
		if got[banned] {
			t.Fatalf("hashed excluded %s in %#v", banned, api.hashes)
		}
	}
	if len(api.uploads) != 0 {
		t.Fatalf("dry run uploaded %#v", api.uploads)
	}

	a, api, buf = newTestApp(t, "push", "--prune", root)
	a.withKey("k")
	api.list = `{"result":"success","files":[
		{"path":"gone","is_directory":true},
		{"path":"gone/x.txt","is_directory":false},
		{"path":"old.txt","is_directory":false},
		{"path":"index.html","is_directory":false}
	]}`
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	var deleted []string
	for _, group := range api.deletes {
		deleted = append(deleted, group...)
	}
	if !containsAll(deleted, "gone", "old.txt") {
		t.Fatalf("deleted %#v", api.deletes)
	}
	for _, name := range deleted {
		if name == "gone/x.txt" || name == "index.html" {
			t.Fatalf("should not delete %s in %#v", name, deleted)
		}
	}
	if string(api.uploads["index.html"]) != "home" {
		t.Fatalf("uploads %#v", api.uploads)
	}
}

func TestPullCommand(t *testing.T) {
	a, api, buf := newTestApp(t, "pull", "-q")
	a.withKey("k")
	api.files["index.html"] = []byte("hello")
	api.list = `{"result":"success","files":[{"path":"index.html","is_directory":false,"size":5,"updated_at":"2013-12-05 01:28:24 -0800"}]}`
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if strings.Contains(buf.String(), "Pulling ") {
		t.Fatalf("quiet pull printed progress %s", buf.String())
	}
	wd, err := a.getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(wd, "index.html"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("file %q %v", got, err)
	}
	cfg, err := a.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastPull == nil || cfg.LastPull.Loc != wd || cfg.LastPull.Time != formatTime(a.now()) {
		t.Fatalf("last pull %#v", cfg.LastPull)
	}
}

func TestConfigCommand(t *testing.T) {
	a, api, buf := newTestApp(t, "config", "penelope", "secret")
	if err := a.saveConfig(&storedConfig{
		APIKey:   "old",
		Sitename: "oldsite",
		LastPull: &storedPull{Time: "2020-01-02 03:04:05 -0700", Loc: "/tmp/site"},
	}); err != nil {
		t.Fatal(err)
	}
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if !strings.HasPrefix(api.auth[0], "Basic ") {
		t.Fatalf("auth %q", api.auth[0])
	}
	cfg, err := a.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "server-key" || cfg.Sitename != "penelope" {
		t.Fatalf("config %#v", cfg)
	}
	if cfg.LastPull == nil || cfg.LastPull.Loc != "/tmp/site" {
		t.Fatalf("last pull %#v", cfg.LastPull)
	}
	path, err := a.configPath()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret") {
		t.Fatalf("password written to config: %s", body)
	}
	if !strings.Contains(buf.String(), "has been stored") || !strings.Contains(buf.String(), path) {
		t.Fatalf("output %s", buf.String())
	}

	a, _, buf = newTestApp(t, "config")
	if code := a.run(); code != 0 || !strings.Contains(buf.String(), "sitename password") {
		t.Fatalf("code %d\n%s", code, buf.String())
	}

	a, _, buf = newTestApp(t, "help", "config")
	if code := a.run(); code != 0 || !strings.Contains(buf.String(), "sitename password") {
		t.Fatalf("help code %d\n%s", code, buf.String())
	}

	a, _, buf = newTestApp(t, "config", "penelope")
	if code := a.run(); code != 1 || !strings.Contains(buf.String(), "sitename and password are required") {
		t.Fatalf("code %d\n%s", code, buf.String())
	}

	a, api, buf = newTestApp(t, "config", "penelope", "nope")
	api.keyBody = `{"result":"error","error_type":"invalid_password","message":"invalid credentials"}`
	if code := a.run(); code != 1 || !strings.Contains(buf.String(), "invalid credentials") {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	path, err = a.configPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config written after failed login: %v", err)
	}
}

func TestPizzaAndBadLogin(t *testing.T) {
	a, _, buf := newTestApp(t, "pizza")
	a.withKey("k")
	if code := a.run(); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(buf.String(), "pineapple") {
		t.Fatalf("pizza %s", buf.String())
	}

	a, api, buf := newTestApp(t, "info")
	a.prompt = &scriptPrompt{asks: []string{"penelope"}, masks: []string{"nope"}}
	api.keyBody = `{"result":"error","error_type":"invalid_password","message":"invalid credentials"}`
	if code := a.run(); code != 1 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "invalid credentials") {
		t.Fatalf("login error %s", buf.String())
	}
}

func TestReadsRubyConfig(t *testing.T) {
	a, api, buf := newTestApp(t, "list", "-a")
	api.list = `{"result":"success","files":[{"path":"index.html","is_directory":false,"size":1,"updated_at":"2013-12-05 01:28:24 -0800"}]}`
	path, err := a.configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"API_KEY":" ruby-key ","SITENAME":"penelope","LAST_PULL":{"time":"2020-01-02 03:04:05 -0700","loc":"/tmp/site"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := a.run(); code != 0 {
		t.Fatalf("code %d\n%s", code, buf.String())
	}
	if api.auth[0] != "Bearer ruby-key" {
		t.Fatalf("auth %q", api.auth[0])
	}
}

type scriptPrompt struct {
	asks  []string
	masks []string
}

func (s *scriptPrompt) Ask(label, def string) (string, error) {
	if len(s.asks) == 0 {
		return def, nil
	}
	v := s.asks[0]
	s.asks = s.asks[1:]
	return v, nil
}

func (s *scriptPrompt) Mask(label, def string) (string, error) {
	if len(s.masks) == 0 {
		return def, nil
	}
	v := s.masks[0]
	s.masks = s.masks[1:]
	return v, nil
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsAll(got []string, want ...string) bool {
	have := map[string]bool{}
	for _, g := range got {
		have[g] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}
