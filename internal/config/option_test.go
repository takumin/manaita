package config_test

import (
	"reflect"
	"testing"

	"github.com/takumin/manaita/internal/config"
)

func TestLogLevel(t *testing.T) {
	want := &config.Config{LogLevel: "TEST"}
	got := &config.Config{}
	config.LogLevel("TEST").Apply(got)
	if !reflect.DeepEqual(want, got) {
		t.Error("expected config struct to be equal, but got not equal")
	}
}

func TestLogFormat(t *testing.T) {
	want := &config.Config{LogFormat: "TEST"}
	got := &config.Config{}
	config.LogFormat("TEST").Apply(got)
	if !reflect.DeepEqual(want, got) {
		t.Error("expected config struct to be equal, but got not equal")
	}
}

func TestVariable(t *testing.T) {
	want := &config.Config{Variable: "TEST"}
	got := &config.Config{}
	config.Variable("TEST").Apply(got)
	if !reflect.DeepEqual(want, got) {
		t.Error("expected config struct to be equal, but got not equal")
	}
}
