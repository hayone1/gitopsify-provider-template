package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModulePath(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/acme/gitopsify-provider-keycloak.git":           "github.com/acme/gitopsify-provider-keycloak",
		"https://github.com/acme/gitopsify-provider-keycloak":               "github.com/acme/gitopsify-provider-keycloak",
		"https://x-access-token:abc@github.com/acme/gitopsify-provider-k/":  "github.com/acme/gitopsify-provider-k",
		"git@github.com:acme/gitopsify-provider-keycloak.git":               "github.com/acme/gitopsify-provider-keycloak",
		"ssh://git@gitlab.example.com/team/sub/gitopsify-provider-keycloak": "gitlab.example.com/team/sub/gitopsify-provider-keycloak",
		"/home/me/repo": "",
		"git@github.com-work:acme/gitopsify-provider-keycloak.git":           "",
		"git@ssh.github.com:acme/gitopsify-provider-keycloak.git":            "github.com/acme/gitopsify-provider-keycloak",
		"ssh://git@gitlab.example.com:2222/team/gitopsify-provider-keycloak": "",
		"https://github.com/acme/":                                           "",
	} {
		if got := modulePath(remote); got != want {
			t.Errorf("modulePath(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestOptions(t *testing.T) {
	for _, bad := range []options{{name: ""}, {name: "template"}, {name: "Key cloak"}, {name: "keycloak", title: `a"b`},
		{name: "keycloak", module: templateModule}} {
		if err := bad.complete(t.TempDir()); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	for _, m := range []string{"github.com/acme/x/", "not a module", "github.com-work/acme/x", "acme", "github.com/acme/../x", "github.com/acme/.x", "github.com/-acme/x"} {
		if err := (&options{name: "keycloak", module: m}).complete(t.TempDir()); err == nil {
			t.Errorf("module %q accepted", m)
		}
	}
	o := options{name: "aws-iam", module: "github.com/acme/gitopsify-provider-aws-iam"}
	if err := o.complete(t.TempDir()); err != nil || o.title != "Aws Iam" {
		t.Errorf("%+v %v", o, err)
	}
}

func TestTemplateRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "tools", "prepare")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := filepath.Join(root, "go.mod")
	if err := os.WriteFile(gomod, []byte("module "+templateModule+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	got, err := templateRoot()
	same := func(a, b string) bool { // macOS: /var is a link to /private/var
		ea, _ := filepath.EvalSymlinks(a)
		eb, _ := filepath.EvalSymlinks(b)
		return ea == eb
	}
	if err != nil || !same(got, root) {
		t.Errorf("root = %q, %v; want %q", got, err, root)
	}
	if err := os.WriteFile(gomod, []byte("module github.com/acme/gitopsify-provider-keycloak\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := templateRoot(); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("a prepared repository: %v", err)
	}
}

func TestRedact(t *testing.T) {
	for _, r := range []string{"https://oauth2:glpat-SECRET@gitlab.example.com:8443/a/b.git", "https://SECRET@github.com/a/b"} {
		if strings.Contains(redact(r), "SECRET") {
			t.Errorf("redact(%q) = %q", r, redact(r))
		}
	}
}

func TestDropTemplateNotes(t *testing.T) {
	in := "a\r\n" + readmeStart + "\r\nx\r\n" + readmeEnd + "\r\nb\n" + readmeStart + "\ny\n" + readmeEnd + "\n\nc\n"
	if got := string(dropTemplateNotes([]byte(in))); got != "a\r\nb\nc\n" {
		t.Errorf("%q", got)
	}
}

func TestPrepare(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                        "module github.com/hayone1/gitopsify-provider-template\n\nreplace github.com/hayone1/gitopsify/sdk => ../gitopsify/sdk\n",
		"main.go":                       "// gitopsify export template\nimport \"github.com/hayone1/gitopsify-provider-template/internal/provider\"\n",
		"internal/provider/describe.go": "\tname = \"template\"\n\ttitle = \"Template\"\nEnv: \"TEMPLATE_URL\"\n",
		".goreleaser.yaml":              "project_name: gitopsify-provider-template\n    name_template: \"{{ .ProjectName }}\"\n",
		"README.md":                     "# gitopsify-provider-template\n\n" + readmeStart + "\nabout the template\n" + readmeEnd + "\n\nInstall: gitopsify provider install hayone1/template\nSee https://github.com/hayone1/gitopsify.\n",
		"tools/prepare/main.go":         "package main // gitopsify-provider-template\n",
		"testdata/api/x.json":           "{\"name\": \"template\"}\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o := options{name: "aws-iam", title: "AWS IAM", module: "github.com/acme/gitopsify-provider-aws-iam"}
	if err := prepare(dir, o); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"go.mod":                        "module github.com/acme/gitopsify-provider-aws-iam\n\nreplace github.com/hayone1/gitopsify/sdk => ../gitopsify/sdk\n",
		"main.go":                       "// gitopsify export aws-iam\nimport \"github.com/acme/gitopsify-provider-aws-iam/internal/provider\"\n",
		"internal/provider/describe.go": "\tname = \"aws-iam\"\n\ttitle = \"AWS IAM\"\nEnv: \"AWS_IAM_URL\"\n",
		".goreleaser.yaml":              "project_name: gitopsify-provider-aws-iam\n    name_template: \"{{ .ProjectName }}\"\n",
		"README.md":                     "# gitopsify-provider-aws-iam\n\nInstall: gitopsify provider install acme/aws-iam\nSee https://github.com/hayone1/gitopsify.\n",
		"testdata/api/x.json":           "{\"name\": \"template\"}\n",
	}
	for name, w := range want {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(b) != w {
			t.Errorf("%s = %q (%v), want %q", name, b, err, w)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "tools", "prepare")); !os.IsNotExist(err) {
		t.Errorf("tools/prepare is still there: %v", err)
	}
	// Nothing of the template's names is left.
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "provider-template") || strings.Contains(string(b), "TEMPLATE_") {
			t.Errorf("%s still has the template's names:\n%s", p, b)
		}
		return nil
	})
}
