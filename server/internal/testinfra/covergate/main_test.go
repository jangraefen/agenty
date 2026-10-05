package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mod = "example.com/m"

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600), "write %s", name)
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

	require.Equal(t, 0, code, "exit code (stderr: %q)", stderr)
	assert.Contains(t, stdout, "ok", "want an ok line")
	assert.Contains(t, stdout, "90.0%", "want 90.0%")
	assert.Contains(t, stdout, mod+"/a", "want the package")
}

func TestPackageBelowThresholdFailsNamingPackageAndCoverage(t *testing.T) {
	code, stdout, stderr := gate(t, "default: 90\n", profile(
		mod+"/a/a.go:1.1,2.1 3 1",
		mod+"/a/a.go:3.1,4.1 1 0",
		mod+"/b/b.go:1.1,2.1 1 1",
	))

	require.Equal(t, 1, code, "exit code")
	assert.Contains(t, stdout, "FAIL  "+mod+"/a: 75.0% of statements, below the 90% threshold")
	assert.Contains(t, stderr, "1 package below its coverage threshold", "want a summary of failing packages")
	assert.NotContains(t, stdout, "FAIL  "+mod+"/b", "package b is fully covered and must not fail")
}

func TestCoverageIsTruncatedNotRounded(t *testing.T) {
	// 899 of 1000 statements: 89.9 %, must not be reported as 90.0 %.
	code, stdout, _ := gate(t, "default: 90\n", profile(
		mod+"/a/a.go:1.1,2.1 899 1",
		mod+"/a/a.go:3.1,4.1 101 0",
	))

	require.Equal(t, 1, code, "exit code")
	assert.Contains(t, stdout, "89.9%")
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

	require.Equal(t, 0, code, "exit code (stdout: %q, stderr: %q)", stdout, stderr)
	assert.Contains(t, stdout, "100.0%")
}

func TestAtomicProfilesFromTheRaceDetectorAreMerged(t *testing.T) {
	// go test -race forces covermode atomic, whose counts exceed 1; a block counts as covered when any
	// tier hit it, whatever the mode of each profile.
	unit := "mode: atomic\n" + mod + "/a/a.go:1.1,2.1 5 42\n" + mod + "/a/a.go:3.1,4.1 5 0\n"
	module := "mode: atomic\n" + mod + "/a/a.go:1.1,2.1 5 0\n" + mod + "/a/a.go:3.1,4.1 5 7\n"

	code, stdout, stderr := gate(t, "default: 100\n", unit, module)

	require.Equal(t, 0, code, "exit code (stdout: %q, stderr: %q)", stdout, stderr)
	assert.Contains(t, stdout, "100.0%")
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

	assert.Equal(t, 0, code, "exit code")
	assert.Contains(t, stdout, "50.0%")
}

func TestPackageThresholdOverridesDefault(t *testing.T) {
	cfg := "default: 90\nthresholds:\n  " + mod + "/critical: 100\n"

	code, stdout, _ := gate(t, cfg, profile(
		mod+"/critical/c.go:1.1,2.1 99 1",
		mod+"/critical/c.go:3.1,4.1 1 0",
		mod+"/other/o.go:1.1,2.1 99 1",
		mod+"/other/o.go:3.1,4.1 1 0",
	))

	require.Equal(t, 1, code, "exit code")
	assert.Contains(t, stdout, "FAIL  "+mod+"/critical: 99.0% of statements, below the 100% threshold",
		"want critical to fail against 100%")
	assert.NotContains(t, stdout, "FAIL  "+mod+"/other", "other must pass against the 90% default")
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
		assert.Equal(t, want, cfg.threshold(pkg), "threshold(%q)", pkg)
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

			assert.Equal(t, 2, code, "exit code")
			assert.Contains(t, stderr, tt.want)
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
		assert.Equal(t, 2, run(args, &stdout, &stderr), "%s: exit code", name)
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
		assert.Equal(t, 2, run(args, &stdout, &stderr), "%s: exit code", name)
		assert.Contains(t, stderr.String(), "Usage", "%s: want usage text", name)
	}
}

// The repository's configuration must parse and require 100 % for the critical
// packages (ARCHITECTURE §15.3), including packages that do not exist yet.
func TestRepositoryConfiguration(t *testing.T) {
	cfg, err := loadConfig(filepath.Join("..", "..", "..", "..", ".covergate.yaml"))
	require.NoError(t, err, "load repository configuration")
	assert.Equal(t, 90, cfg.Default, "default")
	const server = "github.com/jangraefen/agenty/server/internal/"
	for _, pkg := range []string{"toolgateway", "policy", "credentials", "identity", "runs", "identity/oidc"} {
		assert.Equal(t, 100, cfg.threshold(server+pkg), "threshold(%s)", pkg)
	}
	assert.Equal(t, 90, cfg.threshold(server+"config"), "threshold(config)")
}

func TestSummaryCountsAllFailingPackages(t *testing.T) {
	code, _, stderr := gate(t, "default: 90\n", profile(
		mod+"/a/a.go:1.1,2.1 1 0",
		"",
		mod+"/b/b.go:1.1,2.1 1 0",
	))

	assert.Equal(t, 1, code, "exit code")
	assert.Contains(t, stderr, "2 packages below", "want a summary naming 2 packages")
}

func TestPackageWithoutStatementsPasses(t *testing.T) {
	code, stdout, _ := gate(t, "default: 100\n", profile(mod+"/a/a.go:1.1,2.1 0 0"))

	assert.Equal(t, 0, code, "exit code")
	assert.Contains(t, stdout, "100.0%")
}

func TestRejectsUnreadableProfileLine(t *testing.T) {
	code, _, stderr := gate(t, "default: 90\n", profile(strings.Repeat("x", 1<<17)))

	assert.Equal(t, 2, code, "exit code")
	assert.Contains(t, stderr, "read profile", "want a read error")
}

func TestRejectsNestedPatternThatLowersThreshold(t *testing.T) {
	ok := profile(mod + "/a/a.go:1.1,2.1 1 1")
	tests := map[string]string{
		"subtree below subtree":        "default: 90\nthresholds:\n  " + mod + "/runs/...: 100\n  " + mod + "/runs/legacy/...: 50\n",
		"package below subtree":        "default: 90\nthresholds:\n  " + mod + "/runs/...: 100\n  " + mod + "/runs/legacy: 99\n",
		"root package of subtree":      "default: 90\nthresholds:\n  " + mod + "/runs/...: 100\n  " + mod + "/runs: 90\n",
		"below a nested raise":         "default: 50\nthresholds:\n  " + mod + "/a/...: 60\n  " + mod + "/a/b/...: 80\n  " + mod + "/a/b/c: 70\n",
		"subtree lowers exact package": "default: 90\nthresholds:\n  " + mod + "/runs: 100\n  " + mod + "/runs/...: 95\n",
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := gate(t, cfg, ok)

			assert.Equal(t, 2, code, "exit code")
			assert.Contains(t, stderr, "lower than", "want a nested-threshold error")
			assert.Contains(t, stderr, "nested patterns may only raise", "want a nested-threshold error")
		})
	}
}

func TestAllowsNestedPatternThatRaisesOrKeepsThreshold(t *testing.T) {
	tests := map[string]string{
		"raise under subtree":      "default: 50\nthresholds:\n  " + mod + "/a/...: 60\n  " + mod + "/a/b/...: 80\n  " + mod + "/a/b/c: 90\n",
		"equal under subtree":      "default: 90\nthresholds:\n  " + mod + "/runs/...: 100\n  " + mod + "/runs/legacy/...: 100\n",
		"exact pattern is no tree": "default: 90\nthresholds:\n  " + mod + "/runs: 100\n  " + mod + "/runs/legacy: 50\n",
		"sibling prefix":           "default: 90\nthresholds:\n  " + mod + "/runs/...: 100\n  " + mod + "/runsx/...: 50\n",
		"lower than default":       "default: 90\nthresholds:\n  " + mod + "/legacy/...: 50\n",
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(writeFile(t, "covergate.yaml", cfg))
			assert.NoError(t, err, "loadConfig")
		})
	}
}
