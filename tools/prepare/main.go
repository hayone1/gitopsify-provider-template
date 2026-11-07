// Command prepare turns the template into your provider: it renames the Go
// module, the executable, the provider and its environment variables, removes
// the template's notes from the Markdown files (between template:start and
// template:end comments), and then removes itself.
//
//	go run ./tools/prepare -name keycloak -title Keycloak
//
// The module path comes from the repository's origin remote unless -module
// is given.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	templateModule = "github.com/hayone1/gitopsify-provider-template"
	templateRepo   = "gitopsify-provider-template"
	readmeStart    = "<!-- template:start -->"
	readmeEnd      = "<!-- template:end -->"
)

type options struct {
	name, title, module string
}

var providerName = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

func main() {
	var o options
	flag.StringVar(&o.name, "name", "", "the provider's name: gitopsify-provider-<name>, gitopsify export <name> (lowercase letters, digits, dashes)")
	flag.StringVar(&o.title, "title", "", "the tool's display name, such as Keycloak (default: the name, capitalized)")
	flag.StringVar(&o.module, "module", "", "the Go module path (default: from the origin remote, such as github.com/acme/gitopsify-provider-keycloak)")
	flag.Parse()
	root, err := templateRoot()
	if err != nil {
		fail(err)
	}
	if err := o.complete(root); err != nil {
		fail(err)
	}
	fmt.Printf("Renaming %s to module %s, executable gitopsify-provider-%s, title %q\n", root, o.module, o.name, o.title)
	if err := prepare(root, o); err != nil {
		fail(err)
	}
	fmt.Printf(`Done.

Next, replace the made-up notes app with your tool (AGENTS.md has a checklist):
  1. internal/provider: describe.go (settings, kinds, the upstream Terraform
     and Crossplane providers), client.go, provider.go and read.go.
  2. make upstream   # the pinned upstream schema and CRDs, for the tests
  3. make record     # answers from a throwaway instance, for the tests
  4. README.md: what it reads, its settings; then make lint test, and try it:
     make install && gitopsify export %[1]s
`, o.name)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "prepare:", err)
	os.Exit(1)
}

// templateRoot finds the repository: the nearest directory up from here with
// a go.mod, which must still be the template's.
func templateRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			if m := moduleOf(b); m != templateModule {
				return "", fmt.Errorf("%s is module %s, not the template: prepare has run already, or this isn't the template", filepath.Join(dir, "go.mod"), m)
			}
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod here or above: run prepare inside the repository made from the template")
		}
		dir = parent
	}
}

// moduleOf returns the module path a go.mod declares.
func moduleOf(gomod []byte) string {
	for _, line := range strings.Split(string(gomod), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// complete checks the options and fills in the defaults.
func (o *options) complete(dir string) error {
	if !providerName.MatchString(o.name) || o.name == "template" {
		return fmt.Errorf("-name %q: give your provider's name, at least two lowercase letters, digits and dashes, such as keycloak", o.name)
	}
	if o.title == "" {
		words := strings.Split(o.name, "-")
		for i, w := range words {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		o.title = strings.Join(words, " ")
	}
	if strings.ContainsAny(o.title, "\"\\\n\r") {
		return fmt.Errorf("-title %q: no quotes, backslashes or line breaks", o.title)
	}
	if o.module == "" {
		out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
		if err != nil {
			return errors.New("no origin remote to take the module path from; pass -module github.com/<owner>/gitopsify-provider-" + o.name)
		}
		remote := strings.TrimSpace(string(out))
		if o.module = modulePath(remote); o.module == "" {
			return fmt.Errorf("can't make a module path of origin %s (an SSH host alias, a port?); pass -module github.com/<owner>/gitopsify-provider-%s", redact(remote), o.name)
		}
	}
	if err := checkModule(o.module); err != nil {
		return fmt.Errorf("-module %q: %v", o.module, err)
	}
	if o.module == templateModule {
		return errors.New("the module is still the template's: create your own repository from the template first (see README.md)")
	}
	if repo := o.module[strings.LastIndex(o.module, "/")+1:]; repo != "gitopsify-provider-"+o.name {
		fmt.Fprintf(os.Stderr, "warning: the repository is %s; gitopsify provider install <owner>/%s looks for gitopsify-provider-%s\n", repo, o.name, o.name)
	}
	if !strings.HasPrefix(o.module, "github.com/") {
		fmt.Fprintln(os.Stderr, "warning: gitopsify provider install finds providers in GitHub releases; edit the install line in README.md")
	}
	return nil
}

var (
	hostName   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	pathPart   = regexp.MustCompile(`^[A-Za-z0-9_.~-]+$`)
	knownHosts = map[string]string{"ssh.github.com": "github.com", "altssh.gitlab.com": "gitlab.com"}
)

// checkModule says why m can't be a module path: a host name, then at least
// one path element, with no empty elements.
func checkModule(m string) error {
	parts := strings.Split(m, "/")
	if len(parts) < 2 {
		return errors.New("want <host>/<path>, such as github.com/acme/gitopsify-provider-keycloak")
	}
	if !hostName.MatchString(parts[0]) {
		return fmt.Errorf("%q isn't a host name", parts[0])
	}
	for _, p := range parts[1:] {
		if !pathPart.MatchString(p) || strings.HasPrefix(p, ".") || strings.HasPrefix(p, "-") || strings.HasSuffix(p, ".") {
			return fmt.Errorf("%q isn't a valid path element", p)
		}
	}
	return nil
}

// modulePath turns a Git remote URL into a module path:
// https://github.com/acme/x.git and git@github.com:acme/x.git become
// github.com/acme/x. It returns "" for what it can't map (local paths, ports,
// SSH host aliases such as github.com-work).
func modulePath(remote string) string {
	s := strings.TrimSuffix(strings.TrimSuffix(remote, "/"), ".git")
	if rest, ok := strings.CutPrefix(s, "git@"); ok && !strings.Contains(rest, "://") {
		s = strings.Replace(rest, ":", "/", 1)
	} else if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if at := strings.LastIndex(s, "@"); at >= 0 && at < strings.Index(s+"/", "/") {
			s = s[at+1:] // user@ or token@
		}
	} else {
		return ""
	}
	host, rest, _ := strings.Cut(s, "/")
	if h, ok := knownHosts[host]; ok {
		host = h
	}
	s = host + "/" + rest
	if strings.Count(s, "/") < 2 || checkModule(s) != nil {
		return "" // a forge needs <host>/<owner>/<repository>
	}
	return s
}

// redact hides credentials in a remote URL (user:token@ or token@).
func redact(remote string) string {
	i := strings.Index(remote, "://")
	if i < 0 {
		return remote
	}
	hostAndPath := remote[i+3:]
	slash := strings.Index(hostAndPath+"/", "/")
	if at := strings.LastIndex(hostAndPath[:slash], "@"); at >= 0 {
		return remote[:i+3] + "xxxxx" + hostAndPath[at:]
	}
	return remote
}

// replacements returns what to rename, longest first.
func (o options) replacements() []string {
	env := strings.ToUpper(strings.ReplaceAll(o.name, "-", "_")) + "_"
	r := []string{
		templateModule, o.module,
		templateRepo, "gitopsify-provider-" + o.name,
		"gitopsify export template", "gitopsify export " + o.name,
		"TEMPLATE_", env,
		`name = "template"`, fmt.Sprintf("name = %q", o.name),
		`title = "Template"`, fmt.Sprintf("title = %q", o.title),
		"provider for Template", "provider for " + o.title,
	}
	// Install by source: <owner>/<name>, for modules on github.com.
	if parts := strings.Split(o.module, "/"); len(parts) == 3 && parts[0] == "github.com" {
		r = append(r, "hayone1/template", parts[1]+"/"+o.name)
	} else {
		r = append(r, "hayone1/template", "<owner>/"+o.name)
	}
	return r
}

// prepare renames everything in dir and removes the template's own parts.
func prepare(dir string, o options) error {
	repl := strings.NewReplacer(o.replacements()...)
	self := filepath.Join(dir, "tools", "prepare")
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", "bin", "dist":
				return filepath.SkipDir
			}
			if p == self {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return err // binary files stay as they are
		}
		out := []byte(repl.Replace(string(b)))
		if strings.EqualFold(filepath.Ext(p), ".md") {
			out = dropTemplateNotes(out)
		}
		if bytes.Equal(out, b) {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(p, out, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(self)
}

// dropTemplateNotes removes the notes about the template from a Markdown
// file: every block between the start and end comments.
func dropTemplateNotes(b []byte) []byte {
	s := string(b)
	for {
		start := strings.Index(s, readmeStart)
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], readmeEnd)
		if end < 0 {
			break
		}
		rest := strings.TrimLeft(s[start+end+len(readmeEnd):], "\r\n")
		s = s[:start] + rest
	}
	return []byte(s)
}
