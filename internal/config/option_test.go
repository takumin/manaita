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

func TestChdir(t *testing.T) {
	want := &config.Config{Chdir: "TEST"}
	got := &config.Config{}
	config.Chdir("TEST").Apply(got)
	if !reflect.DeepEqual(want, got) {
		t.Error("expected config struct to be equal, but got not equal")
	}
}

func TestParallel(t *testing.T) {
	want := &config.Config{Parallel: 4}
	got := &config.Config{}
	config.Parallel(4).Apply(got)
	if !reflect.DeepEqual(want, got) {
		t.Error("expected config struct to be equal, but got not equal")
	}
}
