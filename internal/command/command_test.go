package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takumin/manaita/internal/command"
	"github.com/takumin/manaita/internal/testutil"
)

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		stdout string
		stderr string
		stdin  string
		args   string
		exit   int
	}{
		"empty":              {"", "", "", "", command.ExitOK},
		"unknown":            {"", "", "", "a unknown", command.ExitNG},
		"log-level-debug":    {"", "", "", "a -l debug", command.ExitOK},
		"log-level-info":     {"", "", "", "a -l info", command.ExitOK},
		"log-level-warn":     {"", "", "", "a -l warn", command.ExitOK},
		"log-level-error":    {"", "", "", "a -l error", command.ExitOK},
		"log-level-unknown":  {"", "", "", "a -l unknown", command.ExitNG},
		"log-format-text":    {"", "", "", "a -f text", command.ExitOK},
		"log-format-json":    {"", "", "", "a -f json", command.ExitOK},
		"log-format-unknown": {"", "", "", "a -f unknown", command.ExitNG},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			stdin := strings.NewReader(tt.stdin)
			args := strings.Split(tt.args, " ")
			exit := command.Main(&stdout, &stderr, stdin, args)

			switch {
			case tt.exit == command.ExitOK && exit == command.ExitNG:
				t.Error("unexpected error:", stdout, stderr)
			case tt.exit == command.ExitNG && exit == command.ExitOK:
				t.Error("unexpected error:", stdout, stderr)
			}
		})
	}
}

func TestSubcommands(t *testing.T) {
	t.Parallel()

	root := testutil.Project(t)
	cases := map[string]struct {
		args   []string
		exit   int
		stdout []string
	}{
		"list":                  {[]string{"list"}, command.ExitOK, []string{"NAME", "rpi4.example.internal  2", "rpi4.other.internal    2", "empty                  0"}},
		"list outside project":  {[]string{"list", "-C", "/"}, command.ExitNG, nil},
		"show":                  {[]string{"show", "rpi4.example.internal"}, command.ExitOK, []string{"hostname: rpi4", "domain:   example.internal", "  - hosts/fqdns/example.internal/rpi4/run_list.yml", "  - nodes/fqdns/example.internal/rpi4/f.yml", "  - cookbooks/server/dnsmasq/extra.rb", "cd mitamae && sudo"}},
		"show recipe":           {[]string{"show", "-r", "cookbooks/common/sudo", "dsk"}, command.ExitOK, []string{"domain:   -", "  - cookbooks/common/sudo/default.rb"}},
		"show two hosts":        {[]string{"show", "dsk", "rpi4"}, command.ExitNG, nil},
		"show unknown host":     {[]string{"show", "missing"}, command.ExitNG, nil},
		"show empty run list":   {[]string{"show", "empty"}, command.ExitNG, nil},
		"apply outside project": {[]string{"apply", "-C", "/", "dsk"}, command.ExitNG, nil},
		"apply missing recipe":  {[]string{"apply", "-r", "missing", "dsk"}, command.ExitNG, nil},
		"apply bad parallel":    {[]string{"apply", "-j", "0", "dsk", "rpi4"}, command.ExitNG, nil},
		"apply dry-run removed": {[]string{"apply", "-n", "dsk"}, command.ExitNG, nil},
		"plan outside project":  {[]string{"plan", "-C", "/", "dsk"}, command.ExitNG, nil},
		"plan missing recipe":   {[]string{"plan", "-r", "missing", "dsk"}, command.ExitNG, nil},
		"plan bad parallel":     {[]string{"plan", "-j", "0", "dsk", "rpi4"}, command.ExitNG, nil},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			args := append([]string{"a", "-C", root}, tt.args...)
			exit := command.Main(&stdout, &stderr, strings.NewReader(""), args)
			if exit != tt.exit {
				t.Fatalf("want exit %d, got %d: %s %s", tt.exit, exit, stdout.String(), stderr.String())
			}
			for _, s := range tt.stdout {
				if !strings.Contains(stdout.String(), s) {
					t.Errorf("stdout does not contain %q:\n%s", s, stdout.String())
				}
			}
		})
	}
}

func TestPluginCommands(t *testing.T) {
	t.Parallel()

	const rev = "0123456789abcdef0123456789abcdef01234567"
	root := testutil.Project(t)
	testutil.WriteFiles(t, root, map[string]string{
		"manaita.yml": testutil.Manifest + "plugins:\n- repo: github.com/o/itamae-plugin-recipe-apt\n  rev: " + rev + "\n",
	})
	plain := testutil.Project(t)
	testutil.WriteFiles(t, plain, map[string]string{"manaita.lock": "stale\n"})

	cases := map[string]struct {
		args   []string
		exit   int
		stdout []string
	}{
		"show plugin recipe": {[]string{"-C", root, "show", "-r", "apt::source", "dsk"}, command.ExitOK, []string{
			"  - apt::source", "plugins:", "  - github.com/o/itamae-plugin-recipe-apt@" + rev,
			"--plugins=.manaita/plugins", " .manaita/recipes/include.apt.source.rb",
		}},
		"show unknown plugin recipe": {[]string{"-C", root, "show", "-r", "other::source", "dsk"}, command.ExitNG, nil},
		"lock without plugins":       {[]string{"-C", plain, "--proxy", "off", "lock"}, command.ExitOK, nil},
		"lock outside project":       {[]string{"-C", "/", "lock"}, command.ExitNG, nil},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := command.Main(&stdout, &stderr, strings.NewReader(""), append([]string{"a"}, tt.args...))
			if exit != tt.exit {
				t.Fatalf("want exit %d, got %d: %s %s", tt.exit, exit, stdout.String(), stderr.String())
			}
			for _, s := range tt.stdout {
				if !strings.Contains(stdout.String(), s) {
					t.Errorf("stdout does not contain %q:\n%s", s, stdout.String())
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(plain, "manaita.lock")); !os.IsNotExist(err) {
		t.Errorf("the lock file of a project without plugins is removed: %v", err)
	}
}

func TestConfigFile(t *testing.T) {
	t.Parallel()

	root := testutil.Project(t)
	dir := t.TempDir()
	write := func(content string) string {
		t.Helper()
		f, err := os.CreateTemp(dir, "*.yml")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close() //nolint:errcheck
		if _, err := f.WriteString(content); err != nil {
			t.Fatal(err)
		}
		return f.Name()
	}
	chdir := write("chdir: " + root + "\n")

	cases := map[string]struct {
		args []string
		exit int
	}{
		"chdir":             {[]string{"--config", chdir, "list"}, command.ExitOK},
		"after subcommand":  {[]string{"list", "--config", chdir}, command.ExitOK},
		"flag over file":    {[]string{"--config", chdir, "-C", "/", "list"}, command.ExitNG},
		"none":              {[]string{"--config", "", "-C", root, "list"}, command.ExitOK},
		"missing":           {[]string{"--config", filepath.Join(dir, "missing.yml"), "-C", root, "list"}, command.ExitNG},
		"unknown key":       {[]string{"--config", write("unknown: 1\n"), "-C", root, "list"}, command.ExitNG},
		"unknown key after": {[]string{"-C", root, "list", "--config", write("unknown: 1\n")}, command.ExitNG},
		"bad log level":     {[]string{"--config", write("log_level: unknown\n"), "-C", root, "list"}, command.ExitNG},
		"bad parallel":      {[]string{"--config", write("parallel: 0\n"), "-C", root, "apply", "dsk", "rpi4"}, command.ExitNG},
		"bad proxy":         {[]string{"--config", write("proxy: ftp://cache.internal\n"), "-C", root, "apply", "dsk"}, command.ExitNG},
		"bad proxy flag":    {[]string{"-C", root, "plan", "--proxy", "cache.internal", "dsk"}, command.ExitNG},
		"proxy for show":    {[]string{"--config", write("proxy: http://cache.internal|direct\ncache_dir: /srv/cache\n"), "-C", root, "show", "dsk"}, command.ExitOK},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			exit := command.Main(&stdout, &stderr, strings.NewReader(""), append([]string{"a"}, tt.args...))
			if exit != tt.exit {
				t.Fatalf("want exit %d, got %d: %s %s", tt.exit, exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCompletion(t *testing.T) {
	t.Parallel()

	root := testutil.Project(t)
	cases := map[string]struct {
		args   []string
		stdout string
	}{
		"show":            {[]string{"show"}, "dsk\nempty\nrpi4\nrpi4.example.internal\nrpi4.other.internal\n"},
		"show given":      {[]string{"show", "dsk"}, ""},
		"apply":           {[]string{"apply"}, "dsk\nempty\nrpi4\nrpi4.example.internal\nrpi4.other.internal\n"},
		"apply given":     {[]string{"apply", "-L", "debug", "dsk"}, "empty\nrpi4\nrpi4.example.internal\nrpi4.other.internal\n"},
		"plan":            {[]string{"plan"}, "dsk\nempty\nrpi4\nrpi4.example.internal\nrpi4.other.internal\n"},
		"plan given":      {[]string{"plan", "dsk"}, "empty\nrpi4\nrpi4.example.internal\nrpi4.other.internal\n"},
		"apply flag":      {[]string{"apply", "--lo"}, "--log-level:log level\n--log-format:log format\n"},
		"apply flag only": {[]string{"apply", "-j"}, ""},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			args := append([]string{"a", "-C", root}, tt.args...)
			args = append(args, "--generate-shell-completion")
			exit := command.Main(&stdout, &stderr, strings.NewReader(""), args)
			if exit != command.ExitOK {
				t.Fatalf("want exit %d, got %d: %s", command.ExitOK, exit, stderr.String())
			}
			if stdout.String() != tt.stdout {
				t.Errorf("want %q, got %q", tt.stdout, stdout.String())
			}
		})
	}

	t.Run("outside project", func(t *testing.T) {
		t.Parallel()

		var stdout, stderr bytes.Buffer
		args := []string{"a", "-C", t.TempDir(), "apply", "--generate-shell-completion"}
		if exit := command.Main(&stdout, &stderr, strings.NewReader(""), args); exit != command.ExitOK {
			t.Fatalf("want exit %d, got %d: %s", command.ExitOK, exit, stderr.String())
		}
		if stdout.String() != "" {
			t.Errorf("want no suggestion, got %q", stdout.String())
		}
	})
}
