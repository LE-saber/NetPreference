package main

import (
	"strings"
	"testing"
)

func TestRPCInputBoundaries(t *testing.T) {
	for _, tc := range []struct {
		method, raw string
		valid       bool
	}{
		{"status", `{}`, true},
		{"status", `{"ubus_rpc_session":"session"}`, true},
		{"apply", `{"config":"anything"}`, false},
		{"check_config", `{"config":"config global main"}`, true},
		{"check_config", `{"config":"config global main","ubus_rpc_session":"s"}`, true},
		{"check_config", `{"config":42}`, false},
		{"check_config", `{"path":"/etc/shadow"}`, false},
		{"check_config", `{"config":"ok","command":"true"}`, false},
		{"check_config", `{}`, false},
		{"check_config", `null`, false},
		{"check_config", `[]`, false},
		{"check_config", `{"config":"` + strings.Repeat("a", 2*1024*1024) + `"}`, false},
		{"apply", strings.Repeat(" ", 8193), false},
	} {
		_, err := readRPCInput(strings.NewReader(tc.raw), tc.method)
		if (err == nil) != tc.valid {
			t.Errorf("method=%s len=%d valid=%v error=%v", tc.method, len(tc.raw), tc.valid, err)
		}
	}
}
