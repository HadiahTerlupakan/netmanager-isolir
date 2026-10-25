package main

import (
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigNamesTheMissingVariable(t *testing.T) {
	full := map[string]string{
		"NETMANAGER_URL":   "http://netmanager",
		"NETMANAGER_TOKEN": "t",
		"TLS_CERT_FILE":    "c.pem",
		"TLS_KEY_FILE":     "k.pem",
	}
	for name := range full {
		env := map[string]string{}
		for k, v := range full {
			if k != name {
				env[k] = v
			}
		}
		_, err := loadConfig(envOf(env))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: err = %v", name, err)
		}
	}
	cfg, err := loadConfig(envOf(full))
	if err != nil || cfg.netmanagerToken != "t" {
		t.Errorf("full env: %+v, %v", cfg, err)
	}
}
