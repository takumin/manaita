package command_test

import (
	"bytes"
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
		"list":                  {[]string{"list"}, command.ExitOK, []string{"NAME", "rpi    rpi.example  rpi4      example.internal  1"}},
		"list outside project":  {[]string{"list", "-C", "/"}, command.ExitNG, nil},
		"show":                  {[]string{"show", "rpi"}, command.ExitOK, []string{"ssh:      rpi.example", "  - nodes/fqdns/example.internal/rpi4/f.yml", "  - cookbooks/server/dnsmasq/extra.rb", "cd mitamae && sudo"}},
		"show recipe":           {[]string{"show", "-r", "cookbooks/common/sudo", "dsk"}, command.ExitOK, []string{"domain:   -", "  - cookbooks/common/sudo/default.rb"}},
		"show no host":          {[]string{"show"}, command.ExitNG, nil},
		"show unknown host":     {[]string{"show", "missing"}, command.ExitNG, nil},
		"show empty run list":   {[]string{"show", "empty"}, command.ExitNG, nil},
		"apply no host":         {[]string{"apply"}, command.ExitNG, nil},
		"apply unknown host":    {[]string{"apply", "missing"}, command.ExitNG, nil},
		"apply missing recipe":  {[]string{"apply", "-r", "missing", "dsk"}, command.ExitNG, nil},
		"apply local two hosts": {[]string{"apply", "--local", "dsk", "rpi"}, command.ExitNG, nil},
		"apply bad parallel":    {[]string{"apply", "-j", "0", "dsk", "rpi"}, command.ExitNG, nil},
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

func TestCompletion(t *testing.T) {
	t.Parallel()

	root := testutil.Project(t)
	cases := map[string]struct {
		args   []string
		stdout string
	}{
		"show":            {[]string{"show"}, "dsk\nempty\nrpi\n"},
		"show given":      {[]string{"show", "dsk"}, ""},
		"apply":           {[]string{"apply"}, "dsk\nempty\nrpi\n"},
		"apply given":     {[]string{"apply", "-n", "dsk"}, "empty\nrpi\n"},
		"apply flag":      {[]string{"apply", "--lo"}, "--log-level:log level\n--log-format:log format\n--local:apply to this machine instead of over ssh (HOST defaults to the short hostname)\n"},
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
