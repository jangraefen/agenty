// Package policy evaluates tool calls against OPA policy.
//
// Policy is written in Rego, in package agenty.tool, as two kinds of rules:
//
//	deny contains reason if { ... }
//	require_approval contains reason if { ... }
//
// There is no allow rule: policy only tightens what the harness grants.
// Policy comes in layers, such as central policy and a harness policy. Each
// layer is compiled on its own, so no layer can change another's rules, and
// the layers' results combine as deny > require_approval > allow.
//
// # Role in the architecture
//
// Engine implements toolgateway.Policy. internal/agent compiles one for each
// run, from the operator's central policy and, if the harness has any, the
// harness's own policy as a second layer, and hands it to the run's gateway,
// which asks it about every granted call. internal/server also compiles
// policy without using it, to refuse a broken central policy when the server
// starts and a broken harness policy when a harness is stored, rather than
// failing a run later; internal/harness wraps inline harness rules with
// RulesModule. The package depends on embedded OPA and on the toolgateway
// types; depguard in .golangci.yml keeps OPA out of every other package, so
// Rego is evaluated in one place only.
//
// # How it works
//
// New parses every module of a layer, checks it is Rego v1 in package
// agenty.tool, and prepares one query per layer that reads both rule sets.
// Evaluate turns the gateway's Request into the input document once, runs
// every layer's query against it, and combines the reasons of all layers:
// any deny reason denies, otherwise any require_approval reason requires
// approval, otherwise the call is allowed.
//
// # Trust-model guarantees
//
// Guarantee 4, strictest wins, lives here: there is no allow rule to write,
// layers are compiled apart so a harness layer cannot redefine a central
// rule, a layer whose modules would silently replace one another is refused,
// and every evaluation error is returned so the gateway denies the call
// (guarantee 3). Policy never sees ungranted calls, as the gateway checks the
// grant first.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/jangraefen/agenty/internal/toolgateway"
)

// packagePath is the one package every policy module must declare. A fixed
// package means a module cannot place rules where the query does not look,
// which would make them silently ineffective.
const packagePath = "agenty.tool"

// packageRef is packagePath as OPA names it in the data document.
var packageRef = ast.MustParseRef("data." + packagePath)

// query reads both rule sets, defaulting to empty when a layer leaves one out.
// Reading them with object.get rather than as data.agenty.tool.deny lets a
// layer define only the rules it needs: a rule it leaves out would otherwise
// be undefined, leaving the query with no result at all, which ruleSets
// treats as an error.
const query = `deny := object.get(data, ["agenty", "tool", "deny"], [])
require_approval := object.get(data, ["agenty", "tool", "require_approval"], [])`

// Module is one Rego source file. Its name is how OPA tells modules apart,
// and appears in compile errors.
type Module struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Layer is a set of modules compiled together, such as central policy or
// the policy of one harness. Modules within a layer share rules and helpers;
// modules of different layers cannot see each other.
type Layer struct {
	Name    string
	Modules []Module
}

var _ toolgateway.Policy = (*Engine)(nil)

// Engine evaluates tool calls against all its layers. It is immutable once
// New returns, so a run's policy cannot change under it; a policy change
// applies from the next engine built, such as for a resumed run.
type Engine struct {
	layers []preparedLayer
}

// preparedLayer is one compiled layer: its name, for errors, and its query,
// prepared once so evaluating a call does not compile anything.
type preparedLayer struct {
	name  string
	query rego.PreparedEvalQuery
}

// New compiles every layer on its own. Modules must be Rego v1 in package
// agenty.tool.
//
// Each layer gets a rego.Rego of its own, which is what keeps layers apart: a
// harness module defining deny adds to its own layer's set and cannot touch
// central policy's.
func New(ctx context.Context, layers ...Layer) (*Engine, error) {
	e := &Engine{}
	for i, l := range layers {
		if l.Name == "" {
			return nil, fmt.Errorf("policy: layer %d has no name", i)
		}
		// Strict builtin errors make a builtin that fails, such as a bad
		// regex, an evaluation error the gateway denies on, instead of a rule
		// that silently does not fire.
		opts := []func(*rego.Rego){
			rego.Query(query),
			rego.StrictBuiltinErrors(true),
		}
		// OPA keeps a layer's modules by name: a second module of a name would
		// silently replace the first, dropping its rules.
		named := make(map[string]int, len(l.Modules))
		for j, m := range l.Modules {
			if m.Name == "" {
				return nil, fmt.Errorf("policy: %s: module %d has no name", l.Name, j)
			}
			if k, ok := named[m.Name]; ok {
				return nil, fmt.Errorf("policy: %s: modules %d and %d are both named %q", l.Name, k, j, m.Name)
			}
			named[m.Name] = j
		}
		// Parse each module ourselves, rather than hand OPA the source, to
		// pin the Rego version and check the package before compiling.
		for _, m := range l.Modules {
			mod, err := ast.ParseModuleWithOpts(m.Name, m.Source, ast.ParserOptions{RegoVersion: ast.RegoV1})
			if err != nil {
				return nil, fmt.Errorf("policy: %s: %w", l.Name, err)
			}
			if !mod.Package.Path.Equal(packageRef) {
				return nil, fmt.Errorf("policy: %s: %s: package must be %s, not %s", l.Name, m.Name, packagePath, packageName(mod.Package.Path))
			}
			opts = append(opts, rego.ParsedModule(mod))
		}
		q, err := rego.New(opts...).PrepareForEval(ctx)
		if err != nil {
			return nil, fmt.Errorf("policy: %s: %w", l.Name, err)
		}
		e.layers = append(e.layers, preparedLayer{name: l.Name, query: q})
	}
	return e, nil
}

// Evaluate decides on one call. Any error, in any layer, is returned, and the
// gateway denies the call. Every layer is evaluated even when an earlier one
// denies, so the verdict carries every reason, which the audit log records.
func (e *Engine) Evaluate(ctx context.Context, req toolgateway.Request) (toolgateway.Verdict, error) {
	// The request's JSON form is the input document; convert it once for all
	// layers.
	doc, err := ast.InterfaceToValue(req)
	if err != nil {
		return toolgateway.Verdict{}, fmt.Errorf("policy: input: %w", err)
	}
	var deny, approval []string
	for _, l := range e.layers {
		d, a, err := l.evaluate(ctx, doc)
		if err != nil {
			return toolgateway.Verdict{}, fmt.Errorf("policy: %s: %w", l.name, err)
		}
		deny, approval = append(deny, d...), append(approval, a...)
	}
	// Strictest wins across all layers (guarantee 4). Reasons are sorted and
	// deduplicated so the same call always gets the same verdict text.
	switch {
	case len(deny) > 0:
		return toolgateway.Verdict{Decision: toolgateway.Deny, Reasons: sortedUnique(deny)}, nil
	case len(approval) > 0:
		return toolgateway.Verdict{Decision: toolgateway.RequireApproval, Reasons: sortedUnique(approval)}, nil
	default:
		return toolgateway.Verdict{Decision: toolgateway.Allow}, nil
	}
}

// evaluate runs the layer's query against the input document doc and
// returns the reasons of its deny and require_approval rules.
func (l preparedLayer) evaluate(ctx context.Context, doc ast.Value) (deny, approval []string, err error) {
	rs, err := l.query.Eval(ctx, rego.EvalParsedInput(doc))
	if err != nil {
		return nil, nil, err
	}
	return ruleSets(rs)
}

// ruleSets reads the deny and require_approval sets from a query result. The
// query always yields exactly one result; anything else is an error, never an
// empty verdict.
func ruleSets(rs rego.ResultSet) (deny, approval []string, err error) {
	if len(rs) != 1 {
		return nil, nil, fmt.Errorf("expected one result, got %d", len(rs))
	}
	if deny, err = reasons(rs[0].Bindings["deny"]); err != nil {
		return nil, nil, fmt.Errorf("deny: %w", err)
	}
	if approval, err = reasons(rs[0].Bindings["require_approval"]); err != nil {
		return nil, nil, fmt.Errorf("require_approval: %w", err)
	}
	return deny, approval, nil
}

// reasons renders a rule set's members: strings as they are, anything else
// as JSON. A reason need not be a string in Rego, and a non-string reason
// still denies; rendering it, rather than rejecting it, keeps the rule
// effective and its reason readable.
func reasons(v any) ([]string, error) {
	members, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be a set, got %T", v)
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		if s, ok := m.(string); ok {
			out = append(out, s)
			continue
		}
		b, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("reason %v: %w", m, err)
		}
		out = append(out, string(b))
	}
	return out, nil
}

// packageName renders a package path as written, without the data prefix.
func packageName(path ast.Ref) string {
	parts := make([]string, 0, len(path)-1)
	for _, term := range path[1:] {
		parts = append(parts, strings.Trim(term.String(), `"`))
	}
	return strings.Join(parts, ".")
}

// sortedUnique sorts s and removes duplicates, in place.
func sortedUnique(s []string) []string {
	slices.Sort(s)
	return slices.Compact(s)
}

// RulesModule makes a module from Rego rules written without a package line,
// such as inline harness policy. Adding the package here means a harness
// author cannot get it wrong, and cannot pick another one.
func RulesModule(name, rules string) Module {
	return Module{Name: name, Source: "package " + packagePath + "\n\n" + rules}
}
