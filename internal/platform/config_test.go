package platform

import (
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{HTTPAddr: "127.0.0.1:8080", ShutdownTimeout: 15 * time.Second}
	if cfg != want {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	cfg, err := LoadConfig(env(map[string]string{
		"RULEFARE_HTTP_ADDR":        ":9090",
		"RULEFARE_SHUTDOWN_TIMEOUT": "2s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{HTTPAddr: ":9090", ShutdownTimeout: 2 * time.Second}
	if cfg != want {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigReportsAllErrors(t *testing.T) {
	_, err := LoadConfig(env(map[string]string{
		"RULEFARE_HTTP_ADDR":        "8080",
		"RULEFARE_SHUTDOWN_TIMEOUT": "-1s",
	}))
	if err == nil {
		t.Fatal("want error")
	}
	for _, name := range []string{"RULEFARE_HTTP_ADDR", "RULEFARE_SHUTDOWN_TIMEOUT"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	for name, vars := range map[string]map[string]string{
		"addr without port":    {"RULEFARE_HTTP_ADDR": "localhost:"},
		"timeout zero":         {"RULEFARE_SHUTDOWN_TIMEOUT": "0s"},
		"timeout not duration": {"RULEFARE_SHUTDOWN_TIMEOUT": "15"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(env(vars)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}
