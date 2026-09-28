package neocities

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

var subcommands = []string{"upload", "delete", "list", "info", "push", "logout", "config", "pizza", "pull"}

type prompter interface {
	Ask(label, def string) (string, error)
	Mask(label, def string) (string, error)
}

type app struct {
	args      []string
	cmd       string
	sub       []string
	in        io.Reader
	out       io.Writer
	getenv    func(string) string
	now       func() time.Time
	getwd     func() (string, error)
	goos      string
	home      func() (string, error)
	newClient func(Options) (*Client, error)
	randInt   func(int) int
	prompt    prompter
	cfg       *storedConfig
	sitename  string
	apiKey    string
}

// Run executes the neocities CLI and returns an exit code.
func Run(args []string) int {
	return newApp(args, os.Stdin, os.Stdout).run()
}

func newApp(args []string, in io.Reader, out io.Writer) *app {
	a := &app{
		args:      args,
		in:        in,
		out:       out,
		getenv:    os.Getenv,
		now:       time.Now,
		getwd:     os.Getwd,
		goos:      runtime.GOOS,
		home:      os.UserHomeDir,
		newClient: NewClient,
		randInt:   rand.IntN,
	}
	a.prompt = newLinePrompt(in, out)
	return a
}

func (a *app) run() int {
	if len(a.args) > 0 && a.args[0] == "version" {
		fmt.Fprintln(a.out, Version)
		return 0
	}
	if len(a.args) > 0 {
		a.cmd = a.args[0]
		a.sub = a.args[1:]
	}
	if isHelpToken(a.cmd) && len(a.sub) > 0 && isSubcommand(a.sub[0]) {
		return a.commandHelp(a.sub[0])
	}
	if a.cmd == "" || !isSubcommand(a.cmd) {
		return a.helpGeneral()
	}
	if a.cmd != "info" && containsHelp(a.sub) {
		return a.commandHelp(a.cmd)
	}

	if a.cmd == "config" {
		cfg, err := a.loadConfig()
		if err != nil {
			a.showErr(err)
			return 1
		}
		a.cfg = cfg
		return a.configure()
	}

	cfg, err := a.loadConfig()
	if err != nil {
		a.showErr(err)
		return 1
	}
	a.cfg = cfg
	a.sitename = cfg.Sitename
	key := trimSpace(a.getenv("NEOCITIES_API_KEY"))
	if key == "" {
		key = cfg.APIKey
	}

	var client *Client
	if key == "" {
		fmt.Fprintln(a.out, "Please login to get your API key:")
		user, err := a.prompt.Ask("sitename:", a.getenv("NEOCITIES_SITENAME"))
		if err != nil {
			a.showErr(err)
			return 1
		}
		pass, err := a.prompt.Mask("password:", a.getenv("NEOCITIES_PASSWORD"))
		if err != nil {
			a.showErr(err)
			return 1
		}
		var code int
		client, code = a.storeLogin(user, pass)
		if code != 0 {
			return code
		}
	} else {
		a.apiKey = key
		var err error
		client, err = a.newClient(Options{APIKey: key})
		if err != nil {
			a.showErr(err)
			return 1
		}
	}

	switch a.cmd {
	case "upload":
		return a.upload(client)
	case "delete":
		return a.delete(client)
	case "list":
		return a.list(client)
	case "info":
		return a.info(client)
	case "push":
		return a.push(client)
	case "logout":
		return a.logout()
	case "pull":
		return a.pull(client)
	case "pizza":
		return a.pizza()
	default:
		return a.helpGeneral()
	}
}

func (a *app) configure() int {
	if len(a.sub) == 0 {
		return a.helpConfig()
	}
	if len(a.sub) != 2 {
		a.displayResponse(Response{Result: "error", Message: "sitename and password are required"})
		a.helpConfig()
		return 1
	}
	_, code := a.storeLogin(a.sub[0], a.sub[1])
	return code
}

// storeLogin exchanges a sitename and password for an API key and writes it
// to the config file. An existing last-pull record is kept.
func (a *app) storeLogin(user, pass string) (*Client, int) {
	user = trimSpace(user)
	if user == "" || pass == "" {
		a.displayResponse(Response{Result: "error", Message: "client requires a login (sitename/password) or an api_key"})
		return nil, 1
	}
	client, err := a.newClient(Options{Sitename: user, Password: pass})
	if err != nil {
		a.showErr(err)
		return nil, 1
	}
	resp, err := client.Key()
	if err != nil {
		a.showErr(err)
		return nil, 1
	}
	if trimSpace(resp.APIKey) == "" {
		a.displayResponse(resp.Response)
		return nil, 1
	}
	if a.cfg == nil {
		a.cfg = &storedConfig{}
	}
	a.sitename = user
	a.apiKey = trimSpace(resp.APIKey)
	a.cfg.APIKey = a.apiKey
	a.cfg.Sitename = user
	if err := a.saveConfig(a.cfg); err != nil {
		a.showErr(err)
		return nil, 1
	}
	cfgPath, err := a.configPath()
	if err != nil {
		a.showErr(err)
		return nil, 1
	}
	fmt.Fprintf(a.out, "The api key for %s has been stored in %s.\n", bold(user), bold(cfgPath))
	client, err = a.newClient(Options{APIKey: a.apiKey})
	if err != nil {
		a.showErr(err)
		return nil, 1
	}
	return client, 0
}

func (a *app) showErr(err error) {
	fmt.Fprintln(a.out, redBold("ERROR:"), err.Error())
}

func (a *app) displayResponse(resp Response) {
	switch {
	case resp.Result == "success":
		fmt.Fprintf(a.out, "%s %s\n", greenBold("SUCCESS:"), resp.Message)
	case resp.Result == "error" && resp.ErrorType == "file_exists":
		out := fmt.Sprintf("%s %s", yellowBold("EXISTS:"), resp.Message)
		if resp.ErrorType != "" {
			out += " (" + resp.ErrorType + ")"
		}
		fmt.Fprintln(a.out, out)
	default:
		out := fmt.Sprintf("%s %s", redBold("ERROR:"), resp.Message)
		if resp.ErrorType != "" {
			out += " (" + resp.ErrorType + ")"
		}
		fmt.Fprintln(a.out, out)
	}
}

func (a *app) delete(client *Client) int {
	if len(a.sub) == 0 {
		return a.helpDelete()
	}
	failed := false
	for _, file := range a.sub {
		fmt.Fprintln(a.out, bold("Deleting "+file+" ..."))
		resp, err := client.Delete(file)
		if err != nil {
			a.showErr(err)
			failed = true
			continue
		}
		a.displayResponse(resp)
		if resp.Result != "success" {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

func (a *app) logout() int {
	args := append([]string(nil), a.sub...)
	confirmed := false
	for len(args) > 0 {
		switch {
		case args[0] == "-y":
			args = args[1:]
			confirmed = true
		case strings.HasPrefix(args[0], "-"):
			fmt.Fprintln(a.out, redBold("Unknown option: "+strconv.Quote(args[0])))
			a.helpLogout()
			return 1
		default:
			args = nil
		}
	}
	if !confirmed {
		return a.helpLogout()
	}
	path, err := a.configPath()
	if err != nil {
		a.showErr(err)
		return 1
	}
	if err := os.Remove(path); err != nil {
		a.showErr(err)
		return 1
	}
	fmt.Fprintln(a.out, bold("Your api key has been removed."))
	return 0
}

func (a *app) info(client *Client) int {
	name := a.sitename
	if len(a.sub) > 0 {
		name = a.sub[0]
	}
	resp, err := client.Info(name)
	if err != nil {
		a.showErr(err)
		return 1
	}
	if resp.Result == "error" {
		a.displayResponse(resp.Response)
		return 1
	}
	rows := make([][]string, 0, len(resp.Fields))
	for _, f := range resp.Fields {
		rows = append(rows, []string{f.Name, formatInfoValue(f.Name, f.Value)})
	}
	widths := colWidths(rows)
	for _, row := range rows {
		writeRow(a.out, row, widths, []func(string) string{bold, nil})
	}
	return 0
}

func (a *app) list(client *Client) int {
	if len(a.sub) == 0 {
		return a.helpList()
	}
	detail := false
	all := false
	var rest []string
	for _, arg := range a.sub {
		switch arg {
		case "-d":
			detail = true
		case "-a":
			all = true
		default:
			rest = append(rest, arg)
		}
	}
	path := ""
	if !all && len(rest) > 0 {
		path = rest[0]
	}
	resp, err := client.List(path)
	if err != nil {
		a.showErr(err)
		return 1
	}
	if resp.Result == "error" {
		a.displayResponse(resp.Response)
		return 1
	}
	if detail {
		rows := [][]string{{"Path", "Size", "Updated"}}
		for _, file := range resp.Files {
			size := ""
			if file.Size != nil {
				size = strconv.FormatInt(*file.Size, 10)
			}
			updated := file.UpdatedAt
			if t, err := parseTime(file.UpdatedAt); err == nil {
				updated = t.Local().Format("2006-01-02 15:04:05 -0700")
			}
			rows = append(rows, []string{file.Path, size, updated})
		}
		widths := colWidths(rows)
		writeRow(a.out, rows[0], widths, []func(string) string{bold, bold, bold})
		for i, file := range resp.Files {
			color := greenBold
			if file.IsDirectory {
				color = blueBold
			}
			writeRow(a.out, rows[i+1], widths, []func(string) string{color, nil, nil})
		}
		return 0
	}
	for _, file := range resp.Files {
		color := greenBold
		if file.IsDirectory {
			color = blueBold
		}
		fmt.Fprintln(a.out, color(file.Path))
	}
	return 0
}

func (a *app) upload(client *Client) int {
	if len(a.sub) == 0 {
		return a.helpUpload()
	}
	dir := ""
	args := append([]string(nil), a.sub...)
	for len(args) > 0 {
		switch args[0] {
		case "-d":
			if len(args) < 2 {
				fmt.Fprintln(a.out, redBold("Unknown option: \"-d\""))
				a.helpUpload()
				return 1
			}
			dir = args[1]
			args = args[2:]
		default:
			if strings.HasPrefix(args[0], "-") {
				fmt.Fprintln(a.out, redBold("Unknown option: "+strconv.Quote(args[0])))
				a.helpUpload()
				return 1
			}
			goto files
		}
	}
files:
	failed := false
	for _, p := range args {
		info, err := os.Stat(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				a.displayResponse(Response{Result: "error", Message: p + " does not exist locally."})
			} else {
				a.showErr(err)
			}
			failed = true
			continue
		}
		if info.IsDir() {
			fmt.Fprintf(a.out, "%s is a directory, skipping (see the push command)\n", p)
			continue
		}
		remote := path.Join("/", dir, filepath.Base(p))
		fmt.Fprintln(a.out, bold(fmt.Sprintf("Uploading %s to %s ...", p, remote)))
		resp, err := client.Upload(p, remote, false)
		if err != nil {
			a.showErr(err)
			failed = true
			continue
		}
		a.displayResponse(resp)
		if resp.Result != "success" && resp.ErrorType != "file_exists" {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

func (a *app) push(client *Client) int {
	noGitignore := false
	dryRun := false
	prune := false
	var excluded []string
	args := append([]string(nil), a.sub...)
	for len(args) > 0 {
		switch args[0] {
		case "--no-gitignore":
			noGitignore = true
			args = args[1:]
		case "--dry-run":
			dryRun = true
			args = args[1:]
		case "--prune":
			prune = true
			args = args[1:]
		case "-e":
			if len(args) < 2 {
				fmt.Fprintln(a.out, redBold("Unknown option: \"-e\""))
				a.helpPush()
				return 1
			}
			excluded = append(excluded, args[1])
			args = args[2:]
		default:
			if strings.HasPrefix(args[0], "-") {
				fmt.Fprintln(a.out, redBold("Unknown option: "+strconv.Quote(args[0])))
				a.helpPush()
				return 1
			}
			goto ready
		}
	}
ready:
	if len(args) == 0 {
		a.displayResponse(Response{Result: "error", Message: "no local path provided"})
		a.helpPush()
		return 1
	}
	root := args[0]
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.displayResponse(Response{Result: "error", Message: "path " + root + " does not exist"})
		} else {
			a.showErr(err)
		}
		a.helpPush()
		return 1
	}
	if !info.IsDir() {
		a.displayResponse(Response{Result: "error", Message: "provided path is not a directory"})
		a.helpPush()
		return 1
	}
	root, err = filepath.Abs(root)
	if err != nil {
		a.showErr(err)
		return 1
	}
	if dryRun {
		fmt.Fprintln(a.out, greenBold("Doing a dry run, not actually pushing anything"))
	}

	failed := false
	if prune {
		if err := a.prune(client, root, dryRun, &failed); err != nil {
			a.showErr(err)
			return 1
		}
	}

	paths, usedGit, err := collectPushPaths(root, noGitignore, excluded)
	if err != nil {
		a.showErr(err)
		return 1
	}
	if usedGit {
		fmt.Fprintln(a.out, "Not pushing .gitignore entries (--no-gitignore to disable)")
	}
	for _, rel := range paths {
		local := filepath.Join(root, filepath.FromSlash(rel))
		fmt.Fprint(a.out, bold("Uploading "+rel+" ... "))
		resp, err := client.Upload(local, rel, dryRun)
		if err != nil {
			fmt.Fprintln(a.out)
			a.showErr(err)
			failed = true
			continue
		}
		switch {
		case resp.Result == "error" && resp.ErrorType == "file_exists":
			fmt.Fprintln(a.out, yellowBold("EXISTS"))
		case resp.Result == "success":
			fmt.Fprintln(a.out, greenBold("SUCCESS"))
		default:
			fmt.Fprintln(a.out)
			a.displayResponse(resp)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

func (a *app) prune(client *Client, root string, dryRun bool, failed *bool) error {
	resp, err := client.List("")
	if err != nil {
		return err
	}
	if resp.Result == "error" {
		a.displayResponse(resp.Response)
		*failed = true
		return nil
	}
	pruned := map[string]struct{}{}
	for _, file := range resp.Files {
		local, joinErr := safeJoin(root, file.Path)
		exists := false
		if joinErr == nil {
			if _, statErr := os.Stat(local); statErr == nil {
				exists = true
			}
		}
		if !exists && file.IsDirectory && joinErr == nil {
			pruned[local] = struct{}{}
		}
		parentPruned := false
		if joinErr == nil {
			_, parentPruned = pruned[filepath.Dir(local)]
		}
		if exists || parentPruned {
			continue
		}
		fmt.Fprint(a.out, bold("Deleting "+file.Path+" ... "))
		var del Response
		if dryRun {
			del = Response{Result: "success"}
		} else {
			del, err = client.Delete(file.Path)
			if err != nil {
				fmt.Fprintln(a.out)
				a.showErr(err)
				*failed = true
				continue
			}
		}
		if del.Result == "success" {
			fmt.Fprintln(a.out, greenBold("SUCCESS"))
			continue
		}
		fmt.Fprintln(a.out)
		a.displayResponse(del)
		*failed = true
	}
	return nil
}

func collectPushPaths(root string, noGitignore bool, excluded []string) ([]string, bool, error) {
	var patterns []string
	usedGit := false
	if !noGitignore {
		pats, err := loadGitignore(root)
		if err == nil {
			patterns = pats
			usedGit = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
	}
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if hasPathSegment(rel, ".git") || pathIgnored(rel, patterns) || excludedPath(rel, excluded) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	return paths, usedGit, err
}

func loadGitignore(root string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil, err
	}
	var patterns []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(line)))
		if statErr == nil && info.IsDir() {
			line += "**"
		}
		patterns = append(patterns, line)
	}
	return patterns, nil
}

func pathIgnored(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if fnmatch(pattern, path) {
			return true
		}
	}
	return false
}

func excludedPath(path string, excluded []string) bool {
	parts := strings.Split(path, "/")
	for _, ex := range excluded {
		ex = filepath.ToSlash(strings.TrimSpace(ex))
		if ex == "" {
			continue
		}
		if path == ex || strings.HasPrefix(path, strings.TrimSuffix(ex, "/")+"/") {
			return true
		}
		for _, part := range parts {
			if part == ex {
				return true
			}
		}
	}
	return false
}

func hasPathSegment(path, name string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == name {
			return true
		}
	}
	return false
}

func (a *app) pull(client *Client) int {
	quiet := len(a.sub) > 0 && (a.sub[0] == "--quiet" || a.sub[0] == "-q")
	var stop func()
	if quiet && isTerminal(a.out) {
		stop = startSpinner(a.out, "Retrieving files for "+bold(a.sitename))
	}
	defer func() {
		if stop != nil {
			stop()
		}
	}()

	cwd, err := a.getwd()
	if err != nil {
		return a.pullFatal(stop, err)
	}
	var lastTime, lastLoc string
	if a.cfg != nil && a.cfg.LastPull != nil {
		lastTime = a.cfg.LastPull.Time
		lastLoc = a.cfg.LastPull.Loc
	}
	_, err = client.Pull(PullOptions{
		Sitename:     a.sitename,
		LastPullTime: lastTime,
		LastPullLoc:  lastLoc,
		Quiet:        quiet,
		Dir:          cwd,
		Output:       a.out,
		BeforeSummary: func() {
			if stop != nil {
				stop()
				stop = nil
			}
		},
	})
	if err != nil {
		return a.pullFatal(stop, err)
	}
	if a.cfg == nil {
		a.cfg = &storedConfig{}
	}
	if a.cfg.APIKey == "" {
		a.cfg.APIKey = a.apiKey
	}
	if a.cfg.Sitename == "" {
		a.cfg.Sitename = a.sitename
	}
	a.cfg.LastPull = &storedPull{Time: formatTime(a.now()), Loc: cwd}
	if err := a.saveConfig(a.cfg); err != nil {
		return a.pullFatal(stop, err)
	}
	return 0
}

func (a *app) pullFatal(stop func(), err error) int {
	if stop != nil {
		stop()
	}
	fmt.Fprintln(a.out, redBold("\nA fatal error occurred :-("))
	fmt.Fprintln(a.out, red(err.Error()))
	return 1
}

func (a *app) pizza() int {
	fmt.Fprintln(a.out, brightRed(pizzaExcuses[a.randInt(len(pizzaExcuses))]))
	return 0
}

func formatInfoValue(key string, v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		if (key == "created_at" || key == "last_updated") && s != "" {
			if t, err := parseTime(s); err == nil {
				return t.Local().Format("2006-01-02 15:04:05 -0700")
			}
		}
		return s
	}
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, formatInfoValue("", item))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(t)
	}
}

func startSpinner(w io.Writer, status string) func() {
	frames := []string{"😺", "😸", "😹", "😻", "😼", "😽", "🙀", "😿", "😾"}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-stop:
				fmt.Fprint(w, "\r\033[K")
				return
			case <-ticker.C:
				fmt.Fprintf(w, "\r%s %s", frames[i%len(frames)], status)
				i++
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func isHelpToken(s string) bool {
	return s == "-h" || s == "--help" || s == "help"
}

func containsHelp(args []string) bool {
	for _, arg := range args {
		if isHelpToken(arg) {
			return true
		}
	}
	return false
}

func isSubcommand(s string) bool {
	for _, cmd := range subcommands {
		if cmd == s {
			return true
		}
	}
	return false
}

func (a *app) commandHelp(cmd string) int {
	switch cmd {
	case "list":
		return a.helpList()
	case "delete":
		return a.helpDelete()
	case "upload":
		return a.helpUpload()
	case "pull":
		return a.helpPull()
	case "push":
		return a.helpPush()
	case "info":
		return a.helpInfo()
	case "logout":
		return a.helpLogout()
	case "config":
		return a.helpConfig()
	case "pizza":
		return a.pizza()
	default:
		return a.helpGeneral()
	}
}

func (a *app) banner() {
	eyes := []string{"o", "~", "O"}
	mouths := []string{"^", "o", "~", "-", "v", "U"}
	e1 := eyes[a.randInt(len(eyes))]
	e2 := eyes[a.randInt(len(eyes))]
	mouth := mouths[a.randInt(len(mouths))]
	fmt.Fprintf(a.out, "\n  |\\---/|\n  | %s_%s |  %s\n   \\_%s_/\n\n", e1, e2, onCyanBold(" Neocities "), mouth)
}

func (a *app) helpGeneral() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s\n", dim("Subcommands:"))
	fmt.Fprint(a.out, `
    push        Recursively upload a local directory to your site
    upload      Upload individual files to your Neocities site
    delete      Delete files from your Neocities site
    list        List files from your Neocities site
    info        Information and stats for your site
    logout      Remove the site api key from the config
    config      Store your site api key from a sitename and password
    version     Unceremoniously display version and self destruct
    pull        Get the most recent version of files from your site
    pizza       Order a free pizza

`)
	return 0
}

func (a *app) helpList() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - List files on your Neocities site\n\n  %s\n\n  %s           List files in your root directory\n\n  %s          Recursively display all files and directories\n\n  %s   Show detailed information on /mydir\n\n",
		greenBold("list"),
		dim("Examples:"),
		green("$ neocities list /"),
		green("$ neocities list -a"),
		green("$ neocities list -d /mydir"),
	)
	return 0
}

func (a *app) helpDelete() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - Delete files on your Neocities site\n\n  %s\n\n  %s               Delete myfile.jpg\n\n  %s   Delete myfile.jpg and myfile2.jpg\n\n  %s                    Deletes mydir and everything inside it (be careful!)\n\n",
		greenBold("delete"),
		dim("Examples:"),
		green("$ neocities delete myfile.jpg"),
		green("$ neocities delete myfile.jpg myfile2.jpg"),
		green("$ neocities delete mydir"),
	)
	return 0
}

func (a *app) helpUpload() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - Upload individual files to your Neocities site\n\n  %s\n\n  %s    Upload images to the root of your site\n\n  %s   Upload img.jpg to the 'images' directory on your site\n\n",
		greenBold("upload"),
		dim("Examples:"),
		green("$ neocities upload img.jpg img2.jpg"),
		green("$ neocities upload -d images img.jpg"),
	)
	return 0
}

func (a *app) helpPull() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - Get the most recent version of files from your site, does not download if files haven't changed\n\n",
		magentaBold("pull"),
	)
	return 0
}

func (a *app) helpPush() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - Recursively upload a local directory to your Neocities site\n\n  %s\n\n  %s                                 Recursively upload current directory.\n\n  %s   Exclude certain files from push\n\n  %s                  Don't use .gitignore to exclude files\n\n  %s                       Just show what would be uploaded\n\n  %s                         Delete site files not in dir (be careful!)\n\n",
		greenBold("push"),
		dim("Examples:"),
		green("$ neocities push ."),
		green("$ neocities push -e node_modules -e secret.txt ."),
		green("$ neocities push --no-gitignore ."),
		green("$ neocities push --dry-run ."),
		green("$ neocities push --prune ."),
	)
	return 0
}

func (a *app) helpInfo() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - Get site info\n\n  %s\n\n  %s   Gets info for 'fauux' site\n\n",
		greenBold("info"),
		dim("Examples:"),
		green("$ neocities info fauux"),
	)
	return 0
}

func (a *app) helpConfig() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - Store your site api key from a sitename and password\n\n  %s\n\n  %s\n\n",
		greenBold("config"),
		dim("Examples:"),
		green("$ neocities config sitename password"),
	)
	return 0
}

func (a *app) helpLogout() int {
	a.banner()
	fmt.Fprintf(a.out, "  %s - Remove the site api key from the config\n\n  %s\n\n  %s\n\n",
		greenBold("logout"),
		dim("Examples:"),
		green("$ neocities logout -y"),
	)
	return 0
}

type linePrompt struct {
	in   *bufio.Reader
	out  io.Writer
	term *os.File
}

func newLinePrompt(in io.Reader, out io.Writer) *linePrompt {
	p := &linePrompt{in: bufio.NewReader(in), out: out}
	if f, ok := in.(*os.File); ok {
		p.term = f
	}
	return p
}

func (p *linePrompt) Ask(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s (%s) ", label, def)
	} else {
		fmt.Fprintf(p.out, "%s ", label)
	}
	return p.read(def)
}

func (p *linePrompt) Mask(label, def string) (string, error) {
	fmt.Fprintf(p.out, "%s ", label)
	if p.term != nil && term.IsTerminal(int(p.term.Fd())) {
		b, err := term.ReadPassword(int(p.term.Fd()))
		fmt.Fprintln(p.out)
		if err != nil {
			return "", err
		}
		if len(b) == 0 {
			return def, nil
		}
		return string(b), nil
	}
	return p.read(def)
}

func (p *linePrompt) read(def string) (string, error) {
	s, err := p.in.ReadString('\n')
	s = strings.TrimRight(s, "\r\n")
	s = strings.TrimSpace(s)
	if err == io.EOF && s == "" {
		if def != "" {
			return def, nil
		}
		return "", err
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	if s == "" {
		return def, nil
	}
	return s, nil
}
