package neocities

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultAPI = "https://neocities.org/api/"

// Options configures a Client. Provide APIKey, or both Sitename and Password.
type Options struct {
	APIKey     string
	Sitename   string
	Password   string
	BaseURL    string
	HTTPClient *http.Client
}

// Client talks to the Neocities API.
type Client struct {
	base     *url.URL
	apiKey   string
	user     string
	pass     string
	http     *http.Client
	fileBase string // overrides the public site origin used by Pull; tests set this
}

// Response is the common Neocities API envelope.
type Response struct {
	Result    string `json:"result"`
	Message   string `json:"message"`
	ErrorType string `json:"error_type"`
}

// File is one entry from the list API.
type File struct {
	Path        string
	IsDirectory bool
	Size        *int64
	UpdatedAt   string
	SHA1        string
}

// ListResponse is the result of listing site files.
type ListResponse struct {
	Response
	Files []File
}

// Field is one key from the info API, in response order.
type Field struct {
	Name  string
	Value any
}

// InfoResponse is the result of the info API.
type InfoResponse struct {
	Response
	Fields []Field
}

// KeyResponse is the result of the key API.
type KeyResponse struct {
	Response
	APIKey string `json:"api_key"`
}

// HashResult is the result of upload_hash.
type HashResult struct {
	Response
	Files map[string]bool
}

// PullOptions controls a pull of the authenticated site.
type PullOptions struct {
	Sitename     string
	LastPullTime string
	LastPullLoc  string
	Quiet        bool
	Dir          string
	Output       io.Writer
	// BeforeSummary runs after files are fetched and before the summary line.
	// The CLI uses it to stop the progress spinner.
	BeforeSummary func()
}

// PullStats summarizes a pull.
type PullStats struct {
	Fetched int
	Elapsed time.Duration
}

// NewClient returns a client authenticated with an API key or site credentials.
func NewClient(opts Options) (*Client, error) {
	if opts.APIKey == "" && (opts.Sitename == "" || opts.Password == "") {
		return nil, errors.New("client requires a login (sitename/password) or an api_key")
	}
	raw := opts.BaseURL
	if raw == "" {
		raw = defaultAPI
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("api url: %w", err)
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport}
	}
	return &Client{
		base:   u,
		apiKey: strings.TrimSpace(opts.APIKey),
		user:   opts.Sitename,
		pass:   opts.Password,
		http:   hc,
	}, nil
}

// List returns files under path. An empty path lists the whole site.
func (c *Client) List(path string) (ListResponse, error) {
	q := url.Values{}
	if path != "" {
		q.Set("path", path)
	}
	var wire struct {
		Response
		Files []wireFile `json:"files"`
	}
	if err := c.getJSON("list", q, &wire); err != nil {
		return ListResponse{}, err
	}
	out := ListResponse{Response: wire.Response, Files: make([]File, 0, len(wire.Files))}
	for _, f := range wire.Files {
		out.Files = append(out.Files, f.file())
	}
	return out, nil
}

// Info returns stats for sitename. An empty sitename returns the authenticated site.
func (c *Client) Info(sitename string) (InfoResponse, error) {
	q := url.Values{}
	if sitename != "" {
		q.Set("sitename", sitename)
	}
	var wire struct {
		Response
		Info json.RawMessage `json:"info"`
	}
	if err := c.getJSON("info", q, &wire); err != nil {
		return InfoResponse{}, err
	}
	fields, err := parseInfoFields(wire.Info)
	if err != nil {
		return InfoResponse{}, err
	}
	return InfoResponse{Response: wire.Response, Fields: fields}, nil
}

// Key returns an API key for the authenticated site.
func (c *Client) Key() (KeyResponse, error) {
	var resp KeyResponse
	err := c.getJSON("key", nil, &resp)
	return resp, err
}

// UploadHash checks whether remotePath already has sha1Hex as its content hash.
func (c *Client) UploadHash(remotePath, sha1Hex string) (HashResult, error) {
	form := url.Values{}
	form.Set(remotePath, sha1Hex)
	var resp HashResult
	err := c.postForm("upload_hash", form, &resp)
	return resp, err
}

// Upload sends localPath to remotePath. When remotePath is empty, the base name
// is used. Files whose SHA-1 already matches the site are not sent. dryRun
// checks the hash and skips the upload.
func (c *Client) Upload(localPath, remotePath string, dryRun bool) (Response, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Response{}, fmt.Errorf("%s does not exist.", localPath)
		}
		return Response{}, err
	}
	if info.IsDir() {
		return Response{}, fmt.Errorf("%s is a directory", localPath)
	}
	if remotePath == "" {
		remotePath = filepath.Base(localPath)
	}
	remotePath = filepath.ToSlash(remotePath)

	sum, err := fileSHA1(localPath)
	if err != nil {
		return Response{}, err
	}
	hashRes, err := c.UploadHash(remotePath, sum)
	if err != nil {
		return Response{}, err
	}
	if hashRes.Files[remotePath] {
		return Response{
			Result:    "error",
			ErrorType: "file_exists",
			Message:   "file already exists and matches local file, not uploading",
		}, nil
	}
	if dryRun {
		return Response{Result: "success"}, nil
	}

	f, err := os.Open(localPath)
	if err != nil {
		return Response{}, err
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile(remotePath, filepath.Base(localPath))
	if err != nil {
		return Response{}, err
	}
	if _, err := io.Copy(part, f); err != nil {
		return Response{}, err
	}
	if err := mw.Close(); err != nil {
		return Response{}, err
	}

	endpoint, err := c.endpoint("upload", nil)
	if err != nil {
		return Response{}, err
	}
	req, err := c.newRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var resp Response
	err = c.do(req, &resp)
	return resp, err
}

// Delete removes paths from the site. Directories delete their contents.
func (c *Client) Delete(paths ...string) (Response, error) {
	if len(paths) == 0 {
		return Response{}, errors.New("no files to delete")
	}
	form := url.Values{}
	for _, p := range paths {
		form.Add("filenames[]", p)
	}
	var resp Response
	err := c.postForm("delete", form, &resp)
	return resp, err
}

// Pull downloads the site into Dir (or the working directory). Unchanged files
// are skipped when LastPullTime and LastPullLoc still describe this directory.
func (c *Client) Pull(opts PullOptions) (PullStats, error) {
	start := time.Now()
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}
	dir := opts.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return PullStats{}, err
		}
	}

	info, err := c.Info(opts.Sitename)
	if err != nil {
		return PullStats{}, err
	}
	if info.Result == "error" {
		msg := info.Message
		if msg == "" {
			msg = "failed to fetch site info"
		}
		return PullStats{}, errors.New(msg)
	}

	list, err := c.List("")
	if err != nil {
		return PullStats{}, err
	}
	if list.Result == "error" {
		msg := list.Message
		if msg == "" {
			msg = "failed to list site files"
		}
		return PullStats{}, errors.New(msg)
	}

	siteBase, err := c.siteBase(opts.Sitename, info)
	if err != nil {
		return PullStats{}, err
	}

	fetched := 0
	for _, file := range list.Files {
		local, err := safeJoin(dir, file.Path)
		if err != nil {
			if !opts.Quiet {
				fmt.Fprint(out, bold("Pulling "+file.Path+" ... "))
				fmt.Fprintln(out, redBold("FAIL"))
			}
			continue
		}
		if file.IsDirectory {
			if err := os.MkdirAll(local, 0o755); err != nil {
				return PullStats{}, err
			}
			continue
		}

		if !opts.Quiet {
			fmt.Fprint(out, bold("Pulling "+file.Path+" ... "))
		}
		if shouldSkip(file, opts.LastPullTime, opts.LastPullLoc, dir, local) {
			if !opts.Quiet {
				fmt.Fprintln(out, yellowBold("NO NEW UPDATES"))
			}
			continue
		}

		fileURL, err := joinSiteURL(siteBase, file.Path)
		if err != nil {
			return PullStats{}, err
		}
		status, err := c.download(fileURL, local)
		if err != nil {
			return PullStats{}, err
		}
		if status == http.StatusOK {
			fetched++
			if !opts.Quiet {
				fmt.Fprintln(out, greenBold("SUCCESS"))
			}
			continue
		}
		if !opts.Quiet {
			fmt.Fprintln(out, redBold("FAIL"))
		}
	}

	if opts.BeforeSummary != nil {
		opts.BeforeSummary()
	}
	elapsed := time.Since(start)
	fmt.Fprintf(out, "\n%s\n", green(fmt.Sprintf("Successfully fetched %d files in %.2f seconds", fetched, elapsed.Seconds())))
	return PullStats{Fetched: fetched, Elapsed: elapsed}, nil
}

func (c *Client) siteBase(sitename string, info InfoResponse) (string, error) {
	if c.fileBase != "" {
		return c.fileBase, nil
	}
	if domain := infoString(info, "domain"); domain != "" {
		return "https://" + domain + "/", nil
	}
	name := sitename
	if name == "" {
		name = infoString(info, "sitename")
	}
	if name == "" {
		return "", errors.New("could not determine site address")
	}
	return "https://" + name + ".neocities.org/", nil
}

func (c *Client) download(fileURL, dest string) (int, error) {
	req, err := c.newRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	return resp.StatusCode, closeErr
}

func shouldSkip(file File, lastTime, lastLoc, dir, local string) bool {
	if lastTime == "" || lastLoc == "" || lastLoc != dir {
		return false
	}
	if _, err := os.Stat(local); err != nil {
		return false
	}
	updated, err1 := parseTime(file.UpdatedAt)
	prev, err2 := parseTime(lastTime)
	if err1 != nil || err2 != nil {
		return false
	}
	return !updated.After(prev)
}

func infoString(info InfoResponse, name string) string {
	for _, f := range info.Fields {
		if f.Name == name {
			s, _ := f.Value.(string)
			return s
		}
	}
	return ""
}

func joinSiteURL(base, filePath string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	parts := strings.Split(filePath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	rel, err := url.Parse(strings.Join(parts, "/"))
	if err != nil {
		return "", err
	}
	return u.ResolveReference(rel).String(), nil
}

func safeJoin(dir, name string) (string, error) {
	if name == "" || strings.Contains(name, "\x00") {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	full := filepath.Join(dir, clean)
	rel, err := filepath.Rel(dir, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	return full, nil
}

// fileSHA1 is the digest Neocities uses for upload_hash.
func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c *Client) endpoint(path string, q url.Values) (string, error) {
	u := c.base.JoinPath(path)
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

func (c *Client) newRequest(method, endpoint string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "neocities/"+Version)
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	} else {
		req.SetBasicAuth(c.user, c.pass)
	}
	return req, nil
}

func (c *Client) getJSON(path string, q url.Values, dest any) error {
	endpoint, err := c.endpoint(path, q)
	if err != nil {
		return err
	}
	req, err := c.newRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	return c.do(req, dest)
}

func (c *Client) postForm(path string, form url.Values, dest any) error {
	endpoint, err := c.endpoint(path, nil)
	if err != nil {
		return err
	}
	req, err := c.newRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, dest)
}

func (c *Client) do(req *http.Request, dest any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("empty response from %s (HTTP %d)", req.URL.Path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(body, 200))
	}
	return nil
}

func truncate(b []byte, n int) string {
	s := string(bytes.TrimSpace(b))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type wireFile struct {
	Path        string `json:"path"`
	IsDirectory bool   `json:"is_directory"`
	Size        *int64 `json:"size"`
	UpdatedAt   string `json:"updated_at"`
	SHA1        string `json:"sha1_hash"`
}

func (f wireFile) file() File {
	return File{
		Path:        f.Path,
		IsDirectory: f.IsDirectory,
		Size:        f.Size,
		UpdatedAt:   f.UpdatedAt,
		SHA1:        f.SHA1,
	}
}

func (h *HashResult) UnmarshalJSON(b []byte) error {
	var wire struct {
		Result    string         `json:"result"`
		Message   string         `json:"message"`
		ErrorType string         `json:"error_type"`
		Files     map[string]any `json:"files"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	h.Result = wire.Result
	h.Message = wire.Message
	h.ErrorType = wire.ErrorType
	h.Files = make(map[string]bool, len(wire.Files))
	for k, v := range wire.Files {
		on, ok := v.(bool)
		if ok {
			h.Files[k] = on
		}
	}
	return nil
}

func parseInfoFields(raw json.RawMessage) ([]Field, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("info object expected")
	}
	var fields []Field
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("info key expected")
		}
		var val any
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		fields = append(fields, Field{Name: key, Value: val})
	}
	return fields, nil
}

func parseTime(s string) (time.Time, error) {
	var last error
	for _, layout := range []string{
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05 Z07:00",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("empty time")
	}
	return time.Time{}, last
}

func formatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05 -0700")
}
