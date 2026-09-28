package neocities

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type fakeAPI struct {
	mu        sync.Mutex
	auth      []string
	listQuery []string
	hashes    []string
	uploads   map[string][]byte
	deletes   [][]string
	files     map[string][]byte
	hashHit   map[string]bool
	list      string
	info      string
	key       string
	keyStatus int
	keyBody   string
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		uploads: map[string][]byte{},
		files:   map[string][]byte{},
		hashHit: map[string]bool{},
		list:    `{"result":"success","files":[]}`,
		info:    `{"result":"success","info":{"sitename":"penelope","hits":3,"created_at":"2013-12-05 01:28:24 -0800","domain":null,"tags":["art","fun"]}}`,
		key:     "server-key",
	}
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()

	switch r.URL.Path {
	case "/api/list":
		f.mu.Lock()
		f.listQuery = append(f.listQuery, r.URL.RawQuery)
		body := f.list
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	case "/api/info":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, f.info)
	case "/api/key":
		w.Header().Set("Content-Type", "application/json")
		if f.keyBody != "" {
			if f.keyStatus != 0 {
				w.WriteHeader(f.keyStatus)
			}
			_, _ = io.WriteString(w, f.keyBody)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "success", "api_key": f.key})
	case "/api/upload_hash":
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		files := map[string]bool{}
		f.mu.Lock()
		for k := range r.PostForm {
			files[k] = f.hashHit[k]
			f.hashes = append(f.hashes, k)
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "success", "files": files})
	case "/api/upload":
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		for name, headers := range r.MultipartForm.File {
			fh, err := headers[0].Open()
			if err != nil {
				f.mu.Unlock()
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			b, _ := io.ReadAll(fh)
			fh.Close()
			f.uploads[name] = b
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":"success","message":"file(s) have been uploaded"}`)
	case "/api/delete":
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		names := append([]string(nil), r.PostForm["filenames[]"]...)
		f.mu.Lock()
		f.deletes = append(f.deletes, names)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":"success","message":"file(s) have been deleted"}`)
	default:
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		name := r.URL.Path
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}
		f.mu.Lock()
		b, ok := f.files[name]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}
}
