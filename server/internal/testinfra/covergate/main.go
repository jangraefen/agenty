// Command covergate enforces per-package statement coverage thresholds
// (ARCHITECTURE §15.3).
//
// It merges Go coverage profiles (for example from the unit and module test
// tiers), computes the statement coverage of every package they contain, and
// fails when a package is below its threshold:
//
//	go run ./server/internal/testinfra/covergate -config .covergate.yaml coverage/unit.out coverage/module.out
//
// A statement counts as covered when any profile covers it. Thresholds come
// from the configuration file: a default for every package plus overrides by
// package pattern (an import path, or an import path followed by "/..." for
// the package and its subpackages; the longest matching pattern wins).
//
// Exit codes: 0 when every package meets its threshold, 1 when at least one
// does not, 2 on invalid usage or input.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

const name = "covergate"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command with the given arguments and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "Usage: %s -config <file> <profile>...\n\nFlags:\n", name)
		flags.PrintDefaults()
	}
	configPath := flags.String("config", "", "path to the threshold configuration (YAML)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" || flags.NArg() == 0 {
		flags.Usage()
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	cov, err := readProfiles(flags.Args())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}

	failed := report(stdout, cfg, cov.packages())
	if failed > 0 {
		what := "package below its coverage threshold"
		if failed > 1 {
			what = "packages below their coverage thresholds"
		}
		_, _ = fmt.Fprintf(stderr, "coverage gate failed: %d %s\n", failed, what)
		return 1
	}
	return 0
}

// config holds the coverage thresholds in percent of statements.
type config struct {
	Default    int            `yaml:"default"`
	Thresholds map[string]int `yaml:"thresholds"`
}

func loadConfig(file string) (config, error) {
	data, err := os.ReadFile(file) //nolint:gosec // The path is a command-line argument of a developer tool.
	if err != nil {
		return config{}, fmt.Errorf("read configuration: %w", err)
	}
	var raw struct {
		Default    *int           `yaml:"default"`
		Thresholds map[string]int `yaml:"thresholds"`
	}
	if err := yaml.UnmarshalWithOptions(data, &raw, yaml.DisallowUnknownField()); err != nil {
		return config{}, fmt.Errorf("invalid configuration %s: %s", file, yaml.FormatError(err, false, true))
	}
	if raw.Default == nil || !validPercent(*raw.Default) {
		return config{}, fmt.Errorf("invalid configuration %s: default must be a percentage from 0 to 100", file)
	}
	for pattern, threshold := range raw.Thresholds {
		if !validPercent(threshold) {
			return config{}, fmt.Errorf("invalid configuration %s: threshold for %q must be a percentage from 0 to 100", file, pattern)
		}
	}
	return config{Default: *raw.Default, Thresholds: raw.Thresholds}, nil
}

func validPercent(p int) bool { return p >= 0 && p <= 100 }

// threshold returns the threshold for the package with import path pkg: the
// one of the longest matching pattern, or the default.
func (c config) threshold(pkg string) int {
	best, threshold := -1, c.Default
	for pattern, t := range c.Thresholds {
		if matches(pattern, pkg) && len(pattern) > best {
			best, threshold = len(pattern), t
		}
	}
	return threshold
}

// matches reports whether pattern ("a/b" or "a/b/...") matches the package pkg.
func matches(pattern, pkg string) bool {
	if base, ok := strings.CutSuffix(pattern, "/..."); ok {
		return pkg == base || strings.HasPrefix(pkg, base+"/")
	}
	return pkg == pattern
}

// coverage is the merged coverage of all profiles, keyed by block
// ("file:start,end").
type coverage map[string]*block

type block struct {
	pkg        string
	statements int
	covered    bool
}

func readProfiles(files []string) (coverage, error) {
	cov := coverage{}
	for _, file := range files {
		if err := cov.read(file); err != nil {
			return nil, err
		}
	}
	if len(cov) == 0 {
		return nil, errors.New("no coverage data in the given profiles")
	}
	return cov, nil
}

// read merges one profile in the format written by go test -coverprofile.
// Concatenated profiles (several "mode:" lines) are accepted.
func (c coverage) read(file string) error {
	f, err := os.Open(file) //nolint:gosec // The path is a command-line argument of a developer tool.
	if err != nil {
		return fmt.Errorf("read profile: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "mode: "):
			continue
		case n == 1:
			return fmt.Errorf("profile %s: first line must be the \"mode:\" header", file)
		}
		if err := c.add(line); err != nil {
			return fmt.Errorf("profile %s line %d: %w", file, n, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read profile %s: %w", file, err)
	}
	return nil
}

// add merges one block line: "<import path>/<file>.go:<start>,<end> <statements> <count>".
func (c coverage) add(line string) error {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return fmt.Errorf("want \"<file>:<range> <statements> <count>\", got %q", line)
	}
	file, _, ok := strings.Cut(fields[0], ":")
	if !ok {
		return fmt.Errorf("no file position in %q", fields[0])
	}
	statements, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("invalid statement count: %w", err)
	}
	count, err := strconv.Atoi(fields[2])
	if err != nil {
		return fmt.Errorf("invalid hit count: %w", err)
	}

	b, seen := c[fields[0]]
	if !seen {
		b = &block{pkg: path.Dir(file), statements: statements}
		c[fields[0]] = b
	}
	if b.statements != statements {
		return fmt.Errorf("block %s has statement count %d, earlier %d", fields[0], statements, b.statements)
	}
	b.covered = b.covered || count > 0
	return nil
}

// pkgCoverage is the statement coverage of one package.
type pkgCoverage struct {
	pkg                string
	covered, statement int
}

// permille returns the coverage in tenths of a percent, truncated, so that a
// package below its threshold is never displayed as reaching it.
func (p pkgCoverage) permille() int {
	if p.statement == 0 {
		return 1000
	}
	return p.covered * 1000 / p.statement
}

func (c coverage) packages() []pkgCoverage {
	byPkg := map[string]*pkgCoverage{}
	for _, b := range c {
		p, ok := byPkg[b.pkg]
		if !ok {
			p = &pkgCoverage{pkg: b.pkg}
			byPkg[b.pkg] = p
		}
		p.statement += b.statements
		if b.covered {
			p.covered += b.statements
		}
	}
	pkgs := make([]pkgCoverage, 0, len(byPkg))
	for _, p := range byPkg {
		pkgs = append(pkgs, *p)
	}
	slices.SortFunc(pkgs, func(a, b pkgCoverage) int { return strings.Compare(a.pkg, b.pkg) })
	return pkgs
}

// report writes one line per package and returns the number of packages below
// their threshold.
func report(w io.Writer, cfg config, pkgs []pkgCoverage) int {
	failed := 0
	for _, p := range pkgs {
		threshold := cfg.threshold(p.pkg)
		pm := p.permille()
		pct := fmt.Sprintf("%d.%d%%", pm/10, pm%10)
		// Compare exact statement counts, not the truncated display value.
		if p.covered*100 >= threshold*p.statement {
			_, _ = fmt.Fprintf(w, "ok    %s: %s of statements (threshold %d%%)\n", p.pkg, pct, threshold)
			continue
		}
		failed++
		_, _ = fmt.Fprintf(w, "FAIL  %s: %s of statements, below the %d%% threshold\n", p.pkg, pct, threshold)
	}
	return failed
}
