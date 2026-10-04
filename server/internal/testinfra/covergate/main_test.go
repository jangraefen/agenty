package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mod = "example.com/m"

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func profile(lines ...string) string {
	return "mode: set\n" + strings.Join(lines, "\n") + "\n"
}

func gate(t *testing.T, cfg string, profiles ...string) (code int, stdout, stderr string) {
	t.Helper()
	args := make([]string, 0, 2+len(profiles))
	args = append(args, "-config", writeFile(t, "covergate.yaml", cfg))
	for i, p := range profiles {
		args = append(args, writeFile(t, "p"+string(rune('0'+i))+".out", p))
	}
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestPackageAtThresholdPasses(t *testing.T) {
	code, stdout, stderr := gate(t, "default: 90\n", profile(
		mod+"/a/a.go:1.1,2.1 9 1",
		mod+"/a/a.go:3.1,4.1 1 0",
	))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if !strings.Contains(stdout, "ok") || !strings.Contains(stdout, "90.0%") || !strings.Contains(stdout, mod+"/a") {
		t.Errorf("stdout = %q, want an ok line with 90.0%% for %s/a", stdout, mod)
	}
}

func TestPackageBelowThresholdFailsNamingPackageAndCoverage(t *testing.T) {
	code, stdout, stderr := gate(t, "default: 90\n", profile(
		mod+"/a/a.go:1.1,2.1 3 1",
		mod+"/a/a.go:3.1,4.1 1 0",
		mod+"/b/b.go:1.1,2.1 1 1",
	))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	want := "FAIL  " + mod + "/a: 75.0% of statements, below the 90% threshold"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, want)
	}
	if !strings.Contains(stderr, "1 package below its coverage threshold") {
		t.Errorf("stderr = %q, want a summary of failing packages", stderr)
	}
	if strings.Contains(stdout, "FAIL  "+mod+"/b") {
		t.Errorf("stdout = %q, package b is fully covered and must not fail", stdout)
	}
}

func TestCoverageIsTruncatedNotRounded(t *testing.T) {
	// 899 of 1000 statements: 89.9 %, must not be reported as 90.0 %.
	code, stdout, _ := gate(t, "default: 90\n", profile(
		mod+"/a/a.go:1.1,2.1 899 1",
		mod+"/a/a.go:3.1,4.1 101 0",
	))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "89.9%") {
		t.Errorf("stdout = %q, want 89.9%%", stdout)
	}
}

func TestProfilesAreMergedAcrossTiers(t *testing.T) {
	// The unit tier covers one block, the module tier the other: together 100 %.
	unit := profile(
		mod+"/a/a.go:1.1,2.1 5 1",
		mod+"/a/a.go:3.1,4.1 5 0",
	)
	module := profile(
		mod+"/a/a.go:1.1,2.1 5 0",
		mod+"/a/a.go:3.1,4.1 5 3",
	)

	code, stdout, stderr := gate(t, "default: 100\n", unit, module)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stdout: %q, stderr: %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "100.0%") {
		t.Errorf("stdout = %q, want 100.0%%", stdout)
	}
}

func TestBlocksRepeatedWithinOneProfileCountOnce(t *testing.T) {
	// A concatenated profile may repeat a block; it must not inflate the statement count.
	code, stdout, _ := gate(t, "default: 50\n", profile(
		mod+"/a/a.go:1.1,2.1 1 1",
		mod+"/a/a.go:1.1,2.1 1 1",
		mod+"/a/a.go:3.1,4.1 1 0",
		"mode: set",
		mod+"/a/a.go:3.1,4.1 1 0",
	))

	if code != 0 || !strings.Contains(stdout, "50.0%") {
		t.Errorf("exit code = %d, stdout = %q, want 0 and 50.0%%", code, stdout)
	}
}

func TestPackageThresholdOverridesDefault(t *testing.T) {
	cfg := "default: 90\nthresholds:\n  " + mod + "/critical: 100\n"

	code, stdout, _ := gate(t, cfg, profile(
		mod+"/critical/c.go:1.1,2.1 99 1",
		mod+"/critical/c.go:3.1,4.1 1 0",
		mod+"/other/o.go:1.1,2.1 99 1",
		mod+"/other/o.go:3.1,4.1 1 0",
	))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "FAIL  "+mod+"/critical: 99.0% of statements, below the 100% threshold") {
		t.Errorf("stdout = %q, want critical to fail against 100%%", stdout)
	}
	if strings.Contains(stdout, "FAIL  "+mod+"/other") {
		t.Errorf("stdout = %q, other must pass against the 90%% default", stdout)
	}
}

func TestThresholdForPattern(t *testing.T) {
	cfg := config{Default: 90, Thresholds: map[string]int{
		mod + "/runs":                 100,
		mod + "/identity/...":         100,
		mod + "/identity/oidc/...":    95,
		mod + "/identity/oidc/exact":  97,
		mod + "/internal/legacy/...":  80,
		mod + "/internal/legacy/keep": 85,
	}}
	tests := map[string]int{
		mod + "/runs":                 100,
		mod + "/runs/sub":             90, // exact pattern does not cover subpackages
		mod + "/runsx":                90,
		mod + "/identity":             100,
		mod + "/identity/saml":        100,
		mod + "/identityx":            90,
		mod + "/identity/oidc":        95,
		mod + "/identity/oidc/deep":   95,
		mod + "/identity/oidc/exact":  97,
		mod + "/internal/legacy/x":    80,
		mod + "/internal/legacy/keep": 85,
		mod + "/elsewhere":            90,
	}
	for pkg, want := range tests {
		if got := cfg.threshold(pkg); got != want {
			t.Errorf("threshold(%q) = %d, want %d", pkg, got, want)
		}
	}
}

func TestRejectsInvalidInput(t *testing.T) {
	ok := profile(mod + "/a/a.go:1.1,2.1 1 1")
	tests := []struct {
		name, cfg, profile, want string
	}{
		{"unknown config field", "default: 90\ntreshold: 1\n", ok, "treshold"},
		{"missing default", "thresholds: {}\n", ok, "default"},
		{"default above 100", "default: 101\n", ok, "default"},
		{"package threshold below 0", "default: 90\nthresholds:\n  x: -1\n", ok, `"x"`},
		{"invalid YAML", "default: [\n", ok, "covergate.yaml"},
		{"empty profile", "default: 90\n", "", "no coverage data"},
		{"missing mode line", "default: 90\n", mod + "/a/a.go:1.1,2.1 1 1\n", "mode"},
		{"too few fields", "default: 90\n", profile(mod + "/a/a.go:1.1,2.1 1"), "line 2"},
		{"no file position", "default: 90\n", profile("a.go 1 1"), "line 2"},
		{"bad statement count", "default: 90\n", profile(mod + "/a/a.go:1.1,2.1 x 1"), "line 2"},
		{"bad hit count", "default: 90\n", profile(mod + "/a/a.go:1.1,2.1 1 x"), "line 2"},
		{"conflicting statement counts", "default: 90\n", profile(
			mod+"/a/a.go:1.1,2.1 1 1",
			mod+"/a/a.go:1.1,2.1 2 1",
		), "statement count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := gate(t, tt.cfg, tt.profile)

			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to mention %q", stderr, tt.want)
			}
		})
	}
}

func TestRejectsMissingFiles(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing")
	cfg := writeFile(t, "covergate.yaml", "default: 90\n")
	prof := writeFile(t, "unit.out", profile(mod+"/a/a.go:1.1,2.1 1 1"))

	for name, args := range map[string][]string{
		"missing config":  {"-config", missing, prof},
		"missing profile": {"-config", cfg, missing},
	} {
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%s: exit code = %d, want 2", name, code)
		}
	}
}

func TestRejectsBadUsage(t *testing.T) {
	cfg := writeFile(t, "covergate.yaml", "default: 90\n")
	for name, args := range map[string][]string{
		"unknown flag":   {"-nope"},
		"no config":      {"unit.out"},
		"no profiles":    {"-config", cfg},
		"nothing at all": nil,
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%s: exit code = %d, want 2", name, code)
		}
		if !strings.Contains(stderr.String(), "Usage") {
			t.Errorf("%s: stderr = %q, want usage text", name, stderr.String())
		}
	}
}

// The repository's configuration must parse and require 100 % for the critical
// packages (ARCHITECTURE §15.3), including packages that do not exist yet.
func TestRepositoryConfiguration(t *testing.T) {
	cfg, err := loadConfig(filepath.Join("..", "..", "..", "..", ".covergate.yaml"))
	if err != nil {
		t.Fatalf("load repository configuration: %v", err)
	}
	if cfg.Default != 90 {
		t.Errorf("default = %d, want 90", cfg.Default)
	}
	const server = "github.com/jangraefen/agenty/server/internal/"
	for _, pkg := range []string{"toolgateway", "policy", "credentials", "identity", "runs", "identity/oidc"} {
		if got := cfg.threshold(server + pkg); got != 100 {
			t.Errorf("threshold(%s) = %d, want 100", pkg, got)
		}
	}
	if got := cfg.threshold(server + "config"); got != 90 {
		t.Errorf("threshold(config) = %d, want 90", got)
	}
}

func TestSummaryCountsAllFailingPackages(t *testing.T) {
	code, _, stderr := gate(t, "default: 90\n", profile(
		mod+"/a/a.go:1.1,2.1 1 0",
		"",
		mod+"/b/b.go:1.1,2.1 1 0",
	))

	if code != 1 || !strings.Contains(stderr, "2 packages below") {
		t.Errorf("exit code = %d, stderr = %q, want 1 and a summary naming 2 packages", code, stderr)
	}
}

func TestPackageWithoutStatementsPasses(t *testing.T) {
	code, stdout, _ := gate(t, "default: 100\n", profile(mod+"/a/a.go:1.1,2.1 0 0"))

	if code != 0 || !strings.Contains(stdout, "100.0%") {
		t.Errorf("exit code = %d, stdout = %q, want 0 and 100.0%%", code, stdout)
	}
}

func TestRejectsUnreadableProfileLine(t *testing.T) {
	code, _, stderr := gate(t, "default: 90\n", profile(strings.Repeat("x", 1<<17)))

	if code != 2 || !strings.Contains(stderr, "read profile") {
		t.Errorf("exit code = %d, stderr = %q, want 2 and a read error", code, stderr)
	}
}
