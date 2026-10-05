package agent_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/internal/model"
	"github.com/jangraefen/agenty/internal/toolgateway"
)

const toolgatewayPath = "github.com/jangraefen/agenty/internal/toolgateway"

// TestInvariant_SideEffectsOnlyViaGateway guards trust-model guarantee 2:
// every side effect goes through the gateway; nothing else executes tools.
func TestInvariant_SideEffectsOnlyViaGateway(t *testing.T) {
	t.Run("every tool execution is a gateway call", func(t *testing.T) {
		f := newFixture(5)
		m := model.NewScripted(
			model.CallTools(call("c1", "tickets.read"), call("c2", "tickets.delete"), call("c3", "tickets.label")),
			model.CallTools(call("c4", "tickets.read")),
			model.Reply("done"),
		)

		_, err := f.run(t, m)
		require.NoError(t, err)

		decisions := recordsOf(f.audit.Records, toolgateway.EventDecision)
		assert.Len(t, decisions, 4, "every tool call the model makes reaches the gateway")
		executed := map[string]int{}
		for _, r := range recordsOf(f.audit.Records, toolgateway.EventResult) {
			executed[r.Tool]++
		}
		assert.Equal(t, executed["tickets.read"], f.read.Calls)
		assert.Equal(t, executed["tickets.label"], f.label.Calls)
		assert.Zero(t, f.del.Calls)
		assert.Equal(t, 3, f.toolCalls())
	})

	t.Run("agent code never calls a tool directly", func(t *testing.T) {
		dir, err := os.Getwd()
		require.NoError(t, err)
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		sources := map[string]string{}
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			src, err := os.ReadFile(p)
			require.NoError(t, err)
			sources[p] = string(src)
		}
		require.NotEmpty(t, sources)

		assert.Empty(t, directToolUses(t, sources),
			"tools may only be handed to the gateway, never called by the agent")
	})

	t.Run("checker detects direct tool use", func(t *testing.T) {
		dir, err := os.Getwd()
		require.NoError(t, err)
		src := `package leak

import (
	"context"

	"` + toolgatewayPath + `"
)

func bypass(ctx context.Context, tools []toolgateway.Tool, gw *toolgateway.Gateway) {
	_, _ = gw.Call(ctx, toolgateway.ToolCall{})
	_ = tools[0].Definition()
	call := tools[0].Call
	_, _ = call(ctx, nil)
}
`
		uses := directToolUses(t, map[string]string{filepath.Join(dir, "leak.go"): src})
		assert.Equal(t, []string{"leak.go:11: Definition", "leak.go:12: Call"}, uses)
	})
}

// Source importing type-checks dependencies from scratch; share one importer
// (and the file set it is bound to) so dependencies are checked only once.
var (
	checkFset     = token.NewFileSet()
	checkImporter = importer.ForCompiler(checkFset, "source", nil)
)

// directToolUses type-checks the given sources as one package and lists every
// selection of a toolgateway.Tool method, as "file:line: method".
func directToolUses(t *testing.T, sources map[string]string) []string {
	t.Helper()
	fset := checkFset
	var files []*ast.File
	for path, src := range sources {
		f, err := parser.ParseFile(fset, path, src, 0)
		require.NoError(t, err)
		files = append(files, f)
	}
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: checkImporter}
	pkg, err := conf.Check(files[0].Name.Name, fset, files, info)
	require.NoError(t, err)

	var tool *types.Interface
	for _, imp := range pkg.Imports() {
		if imp.Path() == toolgatewayPath {
			tool = imp.Scope().Lookup("Tool").Type().Underlying().(*types.Interface)
		}
	}
	require.NotNil(t, tool, "package under check must import toolgateway")

	var uses []string
	for expr, sel := range info.Selections {
		if sel.Kind() == types.FieldVal {
			continue
		}
		recv := sel.Recv()
		if !types.Implements(recv, tool) && !types.Implements(types.NewPointer(recv), tool) {
			continue
		}
		pos := fset.Position(expr.Sel.Pos())
		uses = append(uses, filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line)+": "+sel.Obj().Name())
	}
	slices.Sort(uses)
	return uses
}

// TestInvariant_ModelIsNotTrusted guards trust-model guarantee 1: whatever
// tool the model asks for, deterministic controls decide, and a refused
// call is reported back to the model instead of executing.
func TestInvariant_ModelIsNotTrusted(t *testing.T) {
	tests := []struct {
		name   string
		call   string
		reason string
	}{
		{"registered but not granted", "tickets.delete", "tool not granted"},
		{"granted but not resolvable", "tickets.ghost", "tool not resolved"},
		{"empty name", "", "tool not granted"},
		{"case variant of a granted tool", "TICKETS.READ", "tool not granted"},
		{"path-like name", "../tickets.read", "tool not granted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(3)
			m := model.NewScripted(model.CallTools(call("c1", tt.call)), model.Reply("understood"))

			res, err := f.run(t, m)

			require.NoError(t, err, "a refused call does not end the run")
			assert.Equal(t, "understood", res.Output)
			assert.Zero(t, f.toolCalls(), "a refused call executes nothing")
			require.Len(t, f.audit.Records, 1)
			assert.Equal(t, toolgateway.Deny, f.audit.Records[0].Decision)
			assert.Equal(t, tt.reason, f.audit.Records[0].Reason)

			reqs := m.Requests()
			require.Len(t, reqs, 2)
			last := reqs[1].Messages[len(reqs[1].Messages)-1]
			require.Len(t, last.ToolResults, 1)
			assert.Equal(t, "c1", last.ToolResults[0].CallID)
			assert.True(t, last.ToolResults[0].IsError)
			assert.Contains(t, last.ToolResults[0].Content, tt.reason)
		})
	}
}
