# Neocities

A Go command-line tool and library for the [Neocities](https://neocities.org) API. It uploads, downloads, lists, and deletes files on a site.

## Install

Go 1.24 or newer is required.

```sh
make
make install
```

`make` writes `./neocities`. `make install` copies it to `/usr/local/bin`. Set `PREFIX` or `BINDIR` to install somewhere else:

```sh
make install PREFIX=$HOME/.local
```

From the module directly:

```sh
go build -o neocities ./cmd/neocities
```

Run `neocities` with no arguments to see the commands. `neocities help push` prints examples for one command.

## Setup

Save a login before uploading or downloading:

```sh
neocities config sitename password
```

That exchanges the sitename and password for an API key and writes `config.json`. The password is not stored. The file lives in:

| System | Path |
| --- | --- |
| macOS | `~/Library/Application Support/neocities/config.json` |
| Linux | `$XDG_CONFIG_HOME/neocities/config.json`, or `~/.config/neocities/config.json` |
| Windows | `%LOCALAPPDATA%\neocities\config.json` |

`NEOCITIES_API_KEY` overrides the saved key. `NEOCITIES_SITENAME` and `NEOCITIES_PASSWORD` are the defaults when a command prompts for a login. If no key is available, commands other than `config`, `version`, and `help` ask for a sitename and password and then save the key.

`neocities logout -y` deletes the config file.

## Commands

```text
config      Store an API key from a sitename and password
push        Recursively upload a local directory
upload      Upload individual files
delete      Delete files
list        List files
info        Site info and stats
pull        Download files that changed since the last pull
logout      Remove the saved API key
version     Print the version
pizza       Order a free pizza
```

```sh
neocities config sitename password

neocities push .
neocities push -e node_modules -e secret.txt .
neocities push --no-gitignore .
neocities push --dry-run .
neocities push --prune .

neocities upload img.jpg img2.jpg
neocities upload -d images img.jpg

neocities delete myfile.jpg myfile2.jpg

neocities list /
neocities list -a
neocities list -d /mydir

neocities info
neocities info fauux

neocities pull
neocities pull -q
```

`push` walks a directory, skips `.git`, and skips paths matched by `.gitignore` unless `--no-gitignore` is set. `-e` can be repeated. A file is left in place when its SHA-1 already matches the copy on the site. `--dry-run` reports what would be uploaded. `--prune` deletes remote files that are missing locally.

`upload -d` sets the remote directory. `list /` shows one directory, `list -a` shows the whole site, and `list -d` adds size and modification time. `pull` writes into the current directory and records the time in the config file. `pull -q` (or `--quiet`) shows a spinner instead of one line per file. `info` with no name uses the saved sitename.

## Library

Import `github.com/neocities/neocities`. Pass an API key, or a sitename and password.

```go
client, err := neocities.NewClient(neocities.Options{
    APIKey: os.Getenv("NEOCITIES_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}

list, err := client.List("/")
info, err := client.Info("penelope")
resp, err := client.Upload("index.html", "index.html", false)
resp, err = client.Delete("old.html")
stats, err := client.Pull(neocities.PullOptions{Dir: "."})
```

`List("")` returns every file. `Upload` sends a file only when its SHA-1 differs from the remote copy. The third argument is a dry run: `true` checks the hash and does not upload. `Delete` accepts one or more paths. `Pull` downloads into `Dir`, or into the working directory when `Dir` is empty.

## Development

```sh
make test
make vet
make fmt
```
