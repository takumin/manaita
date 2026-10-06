package command_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/takumin/manaita/internal/command"
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
