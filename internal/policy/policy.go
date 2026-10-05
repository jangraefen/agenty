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

const packagePath = "agenty.tool"

var packageRef = ast.MustParseRef("data." + packagePath)

// query reads both rule sets, defaulting to empty when a layer leaves one out.
const query = `deny := object.get(data, ["agenty", "tool", "deny"], [])
require_approval := object.get(data, ["agenty", "tool", "require_approval"], [])`

// Module is one Rego source file.
type Module struct {
	Name   string
	Source string
}

// Layer is a set of modules compiled together, such as central policy or
// the policy of one harness.
type Layer struct {
	Name    string
	Modules []Module
}

var _ toolgateway.Policy = (*Engine)(nil)

// Engine evaluates tool calls against all its layers.
type Engine struct {
	layers []preparedLayer
}

type preparedLayer struct {
	name  string
	query rego.PreparedEvalQuery
}

// New compiles every layer on its own. Modules must be Rego v1 in package
// agenty.tool.
func New(ctx context.Context, layers ...Layer) (*Engine, error) {
	e := &Engine{}
	for i, l := range layers {
		if l.Name == "" {
			return nil, fmt.Errorf("policy: layer %d has no name", i)
		}
		opts := []func(*rego.Rego){
			rego.Query(query),
			rego.StrictBuiltinErrors(true),
		}
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
// gateway denies the call.
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
	switch {
	case len(deny) > 0:
		return toolgateway.Verdict{Decision: toolgateway.Deny, Reasons: sortedUnique(deny)}, nil
	case len(approval) > 0:
		return toolgateway.Verdict{Decision: toolgateway.RequireApproval, Reasons: sortedUnique(approval)}, nil
	default:
		return toolgateway.Verdict{Decision: toolgateway.Allow}, nil
	}
}

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
// as JSON.
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

func sortedUnique(s []string) []string {
	slices.Sort(s)
	return slices.Compact(s)
}

// RulesModule makes a module from Rego rules written without a package line,
// such as inline harness policy.
func RulesModule(name, rules string) Module {
	return Module{Name: name, Source: "package " + packagePath + "\n\n" + rules}
}
