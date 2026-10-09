package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// userKey is the gin context key of the signed-in user's name.
const userKey = "user"

// userToken is a user's bearer token, as a SHA-256 hash. The server keeps
// only the hash, which gives every token the same length for a
// constant-time comparison.
type userToken struct {
	hash [sha256.Size]byte
	user string
}

// hashTokens hashes the configured users' tokens, from user to token, in the
// order of the users' names, so the list, and the work of comparing against
// it, is the same at every start.
func hashTokens(tokens map[string]string) []userToken {
	out := make([]userToken, 0, len(tokens))
	for _, user := range slices.Sorted(maps.Keys(tokens)) {
		out = append(out, userToken{hash: sha256.Sum256([]byte(tokens[user])), user: user})
	}
	return out
}

// errSignIn is the error of every failed sign-in. It says nothing about why.
var errSignIn = errors.New("sign in with a bearer token: Authorization: Bearer TOKEN")

// authenticate signs the request in with its bearer token. The token is
// compared to every user's, in constant time, as hashes of equal length, so
// the time a request takes tells nothing about any token.
func (s *Server) authenticate(c *gin.Context) {
	scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		c.Header("WWW-Authenticate", "Bearer")
		s.fail(c, http.StatusUnauthorized, errSignIn)
		return
	}
	// Every token is compared, without stopping at a match, so the time
	// taken does not tell which user's token matched, or how many there are
	// before it.
	hash := sha256.Sum256([]byte(token))
	user := ""
	for _, t := range s.tokens {
		if subtle.ConstantTimeCompare(hash[:], t.hash[:]) == 1 {
			user = t.user
		}
	}
	if user == "" {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		s.fail(c, http.StatusUnauthorized, errSignIn)
		return
	}
	c.Set(userKey, user)
	c.Next()
}

// member refuses the request unless the signed-in user is a member of the
// workspace it names, if its route is in a workspace. Any other workspace is
// not found, whether it exists or not, so no one learns the names of
// workspaces they are not in. It runs in the generated routes, after their
// parameters are read; a route under /v1/workspaces/ that does not name its
// workspace {workspace} is refused.
//
// The one exception: a user who left a workspace still reads the
// conversations they started there, read-only. The routes that read a run
// let anyone through, whether the workspace exists or not, as their handlers
// find only the user's own runs, and answer as for any other non-member.
func (s *Server) member(c *gin.Context) {
	ws, ok := c.Params.Get("workspace")
	inWorkspace := strings.HasPrefix(c.FullPath(), "/v1/workspaces/")
	// The reads of a run are left to ownRun, which answers a non-member
	// exactly as this check would, unless the run is their own.
	if c.Request.Method == http.MethodGet && ownRunReads[c.FullPath()] {
		return
	}
	// A route outside any workspace, such as /v1/me or the audit routes,
	// passes; one under /v1/workspaces/ without a workspace parameter finds
	// ws empty, which no one is a member of, so it fails closed.
	if (ok || inWorkspace) && !s.isMember(c.GetString(userKey), ws) {
		s.fail(c, http.StatusNotFound, fmt.Errorf("workspace %s: not found", ws))
	}
}

// ownRunReads are the routes that read a run, which a run's owner may use
// in a workspace they left.
var ownRunReads = map[string]bool{
	"/v1/workspaces/:workspace/runs/:id":            true,
	"/v1/workspaces/:workspace/runs/:id/transcript": true,
	"/v1/workspaces/:workspace/runs/:id/events":     true,
}

// isMember reports whether user is a member of the workspace, as the
// operator config read at the start lists its members. A workspace the
// config does not have has no members.
func (s *Server) isMember(user, workspace string) bool {
	return slices.Contains(s.cfg.Operator.Workspaces[workspace].Members, user)
}
