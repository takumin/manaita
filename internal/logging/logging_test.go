package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/takumin/manaita/internal/logging"
)

func TestNew(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		level   string
		format  string
		wantErr bool
		want    string
		absent  string
	}{
		"debug text":     {"debug", "text", false, "level=DEBUG msg=debug", ""},
		"info text":      {"info", "text", false, "level=INFO msg=info", "msg=debug"},
		"warn json":      {"warn", "json", false, `"level":"WARN","msg":"warn"`, `"msg":"info"`},
		"error json":     {"error", "json", false, `"level":"ERROR","msg":"error"`, `"msg":"warn"`},
		"unknown level":  {"unknown", "text", true, "", ""},
		"unknown format": {"info", "unknown", true, "", ""},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger, err := logging.New(&buf, tt.level, tt.format)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			logger.Debug("debug")
			logger.Info("info")
			logger.Warn("warn")
			logger.Error("error")
			got := buf.String()
			if !strings.Contains(got, tt.want) {
				t.Errorf("output does not contain %q:\n%s", tt.want, got)
			}
			if tt.absent != "" && strings.Contains(got, tt.absent) {
				t.Errorf("output contains %q filtered by the level:\n%s", tt.absent, got)
			}
		})
	}
}

func TestContext(t *testing.T) {
	t.Parallel()

	if got := logging.FromContext(context.Background()); got != slog.Default() {
		t.Error("want slog.Default for a context without logger")
	}

	logger := slog.New(slog.DiscardHandler)
	if got := logging.FromContext(logging.NewContext(context.Background(), logger)); got != logger {
		t.Error("want the logger carried by the context")
	}
}
