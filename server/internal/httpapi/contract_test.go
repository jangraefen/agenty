package httpapi_test

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jangraefen/agenty/server/internal/httpapi"
	"github.com/jangraefen/agenty/server/internal/roles"
)

// contractPath is the OpenAPI contract the server implements (ARCHITECTURE §13.1).
const contractPath = "../../../api/openapi.yaml"

type contractResponse struct {
	Ref     string                    `yaml:"$ref"`
	Content map[string]map[string]any `yaml:"content"`
}

type contractOperation struct {
	OperationID string                      `yaml:"operationId"`
	Responses   map[string]contractResponse `yaml:"responses"`
}

// contractPathItem is a path item: its operations by HTTP method. Path-level keys
// such as summary, description, parameters, and servers are ignored.
type contractPathItem struct {
	Get     *contractOperation `yaml:"get"`
	Put     *contractOperation `yaml:"put"`
	Post    *contractOperation `yaml:"post"`
	Delete  *contractOperation `yaml:"delete"`
	Options *contractOperation `yaml:"options"`
	Head    *contractOperation `yaml:"head"`
	Patch   *contractOperation `yaml:"patch"`
	Trace   *contractOperation `yaml:"trace"`
}

// operations returns the operations of the path item by upper-case HTTP method.
func (p contractPathItem) operations() map[string]contractOperation {
	ops := make(map[string]contractOperation)
	for method, op := range map[string]*contractOperation{
		http.MethodGet: p.Get, http.MethodPut: p.Put, http.MethodPost: p.Post, http.MethodDelete: p.Delete,
		http.MethodOptions: p.Options, http.MethodHead: p.Head, http.MethodPatch: p.Patch, http.MethodTrace: p.Trace,
	} {
		if op != nil {
			ops[method] = *op
		}
	}
	return ops
}

type contract struct {
	OpenAPI    string                      `yaml:"openapi"`
	Paths      map[string]contractPathItem `yaml:"paths"`
	Components struct {
		Responses map[string]contractResponse `yaml:"responses"`
	} `yaml:"components"`
}

func loadContract(t *testing.T) contract {
	t.Helper()
	data, err := os.ReadFile(contractPath)
	require.NoError(t, err, "read contract")
	return parseContract(t, data)
}

func parseContract(t *testing.T, data []byte) contract {
	t.Helper()
	var c contract
	require.NoError(t, yaml.Unmarshal(data, &c), "parse contract")
	require.NotEmpty(t, c.Paths, "contract has no paths")
	return c
}

// mediaTypes returns the media types the contract declares for status, falling
// back to the default response and resolving component references.
func (c contract) mediaTypes(t *testing.T, op contractOperation, status int) []string {
	t.Helper()
	resp, ok := op.Responses[strconv.Itoa(status)]
	if !ok {
		resp, ok = op.Responses["default"]
	}
	require.True(t, ok, "operation %s declares no response for status %d", op.OperationID, status)
	if ref, found := strings.CutPrefix(resp.Ref, "#/components/responses/"); found {
		resp, ok = c.Components.Responses[ref]
		require.True(t, ok, "unresolved response reference %s", ref)
	}
	types := make([]string, 0, len(resp.Content))
	for mt := range resp.Content {
		types = append(types, mt)
	}
	return types
}

func TestContractIsOpenAPI31(t *testing.T) {
	assert.Equal(t, "3.1.0", loadContract(t).OpenAPI, "openapi version")
}

// TestServerServesContractOperations requests every operation of the contract,
// with a passing and a failing readiness check, and checks that the server
// answers with a status and media type the contract declares.
func TestServerServesContractOperations(t *testing.T) {
	c := loadContract(t)
	routers := map[string]http.Handler{
		"ready":     httpapi.NewRouter(httpapi.Options{Roles: []string{roleAPI}}),
		"not ready": httpapi.NewRouter(httpapi.Options{Roles: []string{roleAPI}, Ready: func(context.Context) error { return errors.New("down") }}),
	}
	for path, item := range c.Paths {
		for method, op := range item.operations() {
			for name, h := range routers {
				t.Run(op.OperationID+"/"+name, func(t *testing.T) {
					rec := do(t, h, method, path)

					assert.NotContains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code, "operation is not served")
					mt, _, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
					require.NoError(t, err, "Content-Type")
					assert.Contains(t, c.mediaTypes(t, op, rec.Code), mt, "media type of status %d", rec.Code)
				})
			}
		}
	}
}

// TestServerServesOnlyContractRoutes checks that the router serves no route
// that the contract does not describe.
func TestServerServesOnlyContractRoutes(t *testing.T) {
	c := loadContract(t)
	engine, ok := httpapi.NewRouter(httpapi.Options{}).(*gin.Engine)
	require.True(t, ok, "router is not a *gin.Engine")

	routes := engine.Routes()
	served := make([]string, 0, len(routes))
	for _, r := range routes {
		served = append(served, r.Method+" "+r.Path)
	}
	var declared []string
	for path, item := range c.Paths {
		for method := range item.operations() {
			declared = append(declared, method+" "+path)
		}
	}
	slices.Sort(served)
	slices.Sort(declared)
	assert.Equal(t, declared, served, "served routes")
}

func TestGeneratedEnumsValidateValues(t *testing.T) {
	assert.True(t, httpapi.HealthStatusOk.Valid(), "HealthStatusOk")
	assert.False(t, httpapi.HealthStatus("degraded").Valid(), "unknown health status")
	assert.False(t, httpapi.HealthRoles("gateway").Valid(), "unknown role")
	assert.True(t, httpapi.ReadinessStatusReady.Valid(), "ReadinessStatusReady")
	assert.False(t, httpapi.ReadinessStatus("starting").Valid(), "unknown readiness status")
}

// TestHealthRolesMatchProcessRoles checks that the roles the contract
// declares for /healthz are exactly the roles the process can run.
func TestHealthRolesMatchProcessRoles(t *testing.T) {
	declared := []string{string(httpapi.HealthRolesApi), string(httpapi.HealthRolesWorker), string(httpapi.HealthRolesScheduler)}
	assert.ElementsMatch(t, roles.Strings(roles.All()), declared, "declared roles")
	for _, r := range roles.All() {
		assert.True(t, httpapi.HealthRoles(r).Valid(), "role %q is not declared in the contract", r)
	}
}

// TestContractParsingIgnoresPathItemKeys checks that keys a path item may carry
// besides its operations (summary, description, parameters, servers) do not
// break parsing and are not taken for operations.
func TestContractParsingIgnoresPathItemKeys(t *testing.T) {
	const fixture = `
openapi: 3.1.0
paths:
  /things/{id}:
    summary: One thing
    description: A thing by its ID.
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
    servers:
      - url: https://agenty.example
    get:
      operationId: getThing
      responses:
        "200":
          description: The thing.
    delete:
      operationId: deleteThing
      responses:
        "204":
          description: Deleted.
`
	c := parseContract(t, []byte(fixture))

	require.Contains(t, c.Paths, "/things/{id}")
	ops := c.Paths["/things/{id}"].operations()
	ids := make(map[string]string, len(ops))
	for method, op := range ops {
		ids[method] = op.OperationID
	}
	assert.Equal(t, map[string]string{http.MethodGet: "getThing", http.MethodDelete: "deleteThing"}, ids, "operations by method")
}
