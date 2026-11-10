package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaults(t *testing.T) {
	t.Setenv("RTK_GO_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("RTK_GO_DISABLE", "")
	t.Setenv("RTK_GO_NO_STORE", "")
	l := Load()
	if l.Err != nil {
		t.Fatalf("unexpected error: %v", l.Err)
	}
	if l.Source != "default" {
		t.Fatalf("source = %q", l.Source)
	}
	c := l.Config
	if c.SchemaVersion != 1 || c.Profile != "aggressive" {
		t.Fatalf("bad defaults: %+v", c)
	}
	if c.Storage.RetentionHours != 24 || c.Storage.TotalMiB != 256 || !c.Storage.Enabled {
		t.Fatalf("bad storage defaults: %+v", c.Storage)
	}
	if c.Capture.MemoryMiB != 8 || c.Capture.StreamThresholdMS != 1500 || c.Capture.TestThresholdMS != 30000 {
		t.Fatalf("bad capture defaults: %+v", c.Capture)
	}
	if c.Stats.RetentionDays != 30 || !c.Stats.Enabled {
		t.Fatalf("bad stats defaults: %+v", c.Stats)
	}
}

func TestLoadFileWithBOMAndUnknownKey(t *testing.T) {
	p := writeConfig(t, "\xef\xbb\xbf{\"schema_version\":1,\"profile\":\"safe\",\"storage\":{\"retention_hours\":12},\"future\":{\"x\":1}}")
	t.Setenv("RTK_GO_CONFIG", p)
	l := Load()
	if l.Err != nil {
		t.Fatalf("unexpected error: %v", l.Err)
	}
	if l.Source != "file" || l.Config.Profile != "safe" || l.Config.Storage.RetentionHours != 12 {
		t.Fatalf("unexpected config: %+v", l)
	}
	if l.Config.Storage.TotalMiB != 256 {
		t.Fatalf("missing fields must keep defaults: %+v", l.Config.Storage)
	}
	if len(l.Warnings) != 1 || l.Warnings[0] != "unknown config key: future" {
		t.Fatalf("warnings = %v", l.Warnings)
	}
}

func TestInvalidTypeDisablesConfig(t *testing.T) {
	p := writeConfig(t, `{"schema_version":1,"storage":{"retention_hours":"24"}}`)
	t.Setenv("RTK_GO_CONFIG", p)
	l := Load()
	if l.Err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(l.Err.Error(), "retention_hours") {
		t.Fatalf("error should name field: %v", l.Err)
	}
}

func TestRangeValidation(t *testing.T) {
	cases := []struct{ body, field string }{
		{`{"schema_version":1,"profile":"turbo"}`, "profile"},
		{`{"schema_version":1,"storage":{"retention_hours":0}}`, "storage.retention_hours"},
		{`{"schema_version":1,"storage":{"total_mib":32}}`, "storage.total_mib"},
		{`{"schema_version":1,"capture":{"memory_mib":64}}`, "capture.memory_mib"},
		{`{"schema_version":1,"capture":{"stream_threshold_ms":10}}`, "capture.stream_threshold_ms"},
		{`{"schema_version":1,"capture":{"test_threshold_ms":100}}`, "capture.test_threshold_ms"},
		{`{"schema_version":1,"stats":{"retention_days":400}}`, "stats.retention_days"},
		{`{"schema_version":2}`, "schema_version"},
	}
	for _, tc := range cases {
		p := writeConfig(t, tc.body)
		t.Setenv("RTK_GO_CONFIG", p)
		l := Load()
		if l.Err == nil || !strings.Contains(l.Err.Error(), tc.field) {
			t.Errorf("body %s: err = %v, want mention of %s", tc.body, l.Err, tc.field)
		}
	}
}

func TestRelativeDataDirRejected(t *testing.T) {
	p := writeConfig(t, `{"schema_version":1,"storage":{"data_dir":"relative/dir"}}`)
	t.Setenv("RTK_GO_CONFIG", p)
	l := Load()
	if l.Err == nil || !strings.Contains(l.Err.Error(), "storage.data_dir") {
		t.Fatalf("err = %v", l.Err)
	}
}

func TestAbsoluteDataDirAccepted(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "data")
	p := writeConfig(t, `{"schema_version":1,"storage":{"data_dir":`+jsonQuote(abs)+`}}`)
	t.Setenv("RTK_GO_CONFIG", p)
	l := Load()
	if l.Err != nil {
		t.Fatalf("err = %v", l.Err)
	}
	if l.Config.Storage.DataDir != abs || !l.Config.DataDirExplicit {
		t.Fatalf("data dir = %q explicit=%v", l.Config.Storage.DataDir, l.Config.DataDirExplicit)
	}
}

func jsonQuote(s string) string {
	b := strings.ReplaceAll(s, `\`, `\\`)
	b = strings.ReplaceAll(b, `"`, `\"`)
	return `"` + b + `"`
}

func TestEnvSwitches(t *testing.T) {
	for _, v := range []string{"1", "true", "YES", "on"} {
		t.Setenv("RTK_GO_DISABLE", v)
		if !Disabled() {
			t.Errorf("Disabled() false for %q", v)
		}
	}
	t.Setenv("RTK_GO_DISABLE", "0")
	if Disabled() {
		t.Error("Disabled() true for 0")
	}
	t.Setenv("RTK_GO_NO_STORE", "1")
	if !NoStore() {
		t.Error("NoStore() false")
	}
}

func TestMissingFileWithoutOverrideIsDefault(t *testing.T) {
	// Point the user config dir at an empty temporary directory.
	dir := t.TempDir()
	t.Setenv("RTK_GO_CONFIG", "")
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	l := Load()
	if l.Err != nil || l.Source != "default" {
		t.Fatalf("loaded %+v", l)
	}
}
