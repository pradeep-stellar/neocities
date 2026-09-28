package neocities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewClientRequiresCredentials(t *testing.T) {
	_, err := NewClient(Options{})
	if err == nil || !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("got %v", err)
	}
}

func TestClientAuthAndCalls(t *testing.T) {
	api := newFakeAPI()
	srv := api.server(t)
	c, err := NewClient(Options{APIKey: "secret", BaseURL: srv.URL + "/api/"})
	if err != nil {
		t.Fatal(err)
	}

	list, err := c.List("/")
	if err != nil {
		t.Fatal(err)
	}
	if list.Result != "success" {
		t.Fatalf("list result %q", list.Result)
	}
	if api.listQuery[0] != "path=%2F" {
		t.Fatalf("list query %q", api.listQuery[0])
	}
	if api.auth[0] != "Bearer secret" {
		t.Fatalf("auth %q", api.auth[0])
	}

	all, err := c.List("")
	if err != nil {
		t.Fatal(err)
	}
	if all.Result != "success" {
		t.Fatal(all.Result)
	}
	if api.listQuery[1] != "" {
		t.Fatalf("all-files query %q", api.listQuery[1])
	}

	info, err := c.Info("penelope")
	if err != nil {
		t.Fatal(err)
	}
	if info.Fields[0].Name != "sitename" || info.Fields[0].Value != "penelope" {
		t.Fatalf("info fields %#v", info.Fields)
	}
	if formatInfoValue(info.Fields[1].Name, info.Fields[1].Value) != "3" {
		t.Fatalf("hits %#v", info.Fields[1].Value)
	}

	basic, err := NewClient(Options{Sitename: "penelope", Password: "pw", BaseURL: srv.URL + "/api/"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := basic.Key()
	if err != nil {
		t.Fatal(err)
	}
	if key.APIKey != "server-key" {
		t.Fatalf("key %q", key.APIKey)
	}
	if !strings.HasPrefix(api.auth[len(api.auth)-1], "Basic ") {
		t.Fatalf("basic auth %q", api.auth[len(api.auth)-1])
	}
}

func TestUploadSkipsMatchingHash(t *testing.T) {
	api := newFakeAPI()
	api.hashHit["index.html"] = true
	srv := api.server(t)
	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL + "/api/"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := c.Upload(path, "index.html", false)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ErrorType != "file_exists" {
		t.Fatalf("resp %#v", resp)
	}
	if len(api.uploads) != 0 {
		t.Fatalf("uploaded %#v", api.uploads)
	}
}

func TestUploadSendsFile(t *testing.T) {
	api := newFakeAPI()
	srv := api.server(t)
	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL + "/api/"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "img.jpg")
	if err := os.WriteFile(path, []byte("jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := c.Upload(path, "/images/img.jpg", false)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Result != "success" || resp.Message != "file(s) have been uploaded" {
		t.Fatalf("resp %#v", resp)
	}
	if string(api.uploads["/images/img.jpg"]) != "jpeg" {
		t.Fatalf("uploads %#v", api.uploads)
	}

	dry, err := c.Upload(path, "other.jpg", true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Result != "success" {
		t.Fatalf("dry %#v", dry)
	}
	if _, ok := api.uploads["other.jpg"]; ok {
		t.Fatal("dry run uploaded")
	}
}

func TestDeleteAndPull(t *testing.T) {
	api := newFakeAPI()
	api.files["index.html"] = []byte("hello")
	api.files["css/a.css"] = []byte("body{}")
	api.files["../outside.txt"] = []byte("nope")
	api.list = `{
		"result":"success",
		"files":[
			{"path":"css","is_directory":true,"updated_at":"2013-12-05 01:28:24 -0800"},
			{"path":"index.html","is_directory":false,"size":5,"updated_at":"2013-12-05 01:28:24 -0800"},
			{"path":"css/a.css","is_directory":false,"size":6,"updated_at":"2013-12-05 01:28:24 -0800"},
			{"path":"../outside.txt","is_directory":false,"size":4,"updated_at":"2013-12-05 01:28:24 -0800"}
		]
	}`
	srv := api.server(t)
	c, err := NewClient(Options{APIKey: "k", BaseURL: srv.URL + "/api/"})
	if err != nil {
		t.Fatal(err)
	}
	c.fileBase = srv.URL + "/"

	del, err := c.Delete("old.txt", "gone")
	if err != nil {
		t.Fatal(err)
	}
	if del.Result != "success" {
		t.Fatalf("delete %#v", del)
	}
	if strings.Join(api.deletes[0], ",") != "old.txt,gone" {
		t.Fatalf("deletes %#v", api.deletes)
	}

	dir := t.TempDir()
	var buf strings.Builder
	stats, err := c.Pull(PullOptions{Sitename: "penelope", Dir: dir, Output: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Fetched != 2 {
		t.Fatalf("fetched %d output %s", stats.Fetched, buf.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("index %q %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "css", "a.css"))
	if err != nil || string(got) != "body{}" {
		t.Fatalf("css %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outside.txt")); err == nil {
		t.Fatal("wrote outside the destination")
	}
	if !strings.Contains(buf.String(), "Successfully fetched 2 files") {
		t.Fatalf("summary %s", buf.String())
	}

	buf.Reset()
	stats, err = c.Pull(PullOptions{
		Sitename:     "penelope",
		Dir:          dir,
		Output:       &buf,
		LastPullTime: "2099-01-02 03:04:05 -0700",
		LastPullLoc:  dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Fetched != 0 {
		t.Fatalf("second fetch %d %s", stats.Fetched, buf.String())
	}
	if !strings.Contains(buf.String(), "NO NEW UPDATES") {
		t.Fatalf("skip output %s", buf.String())
	}
}

func TestSafeJoin(t *testing.T) {
	dir := t.TempDir()
	if _, err := safeJoin(dir, "../etc/passwd"); err == nil {
		t.Fatal("accepted parent traversal")
	}
	if _, err := safeJoin(dir, "/etc/passwd"); err == nil {
		t.Fatal("accepted absolute path")
	}
	got, err := safeJoin(dir, "css/a.css")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "css", "a.css") {
		t.Fatalf("got %s", got)
	}
}
