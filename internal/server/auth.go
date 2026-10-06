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

// userToken is a user's bearer token, as a SHA-256 hash.
type userToken struct {
	hash [sha256.Size]byte
	user string
}

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
// workspace it names, if it names one. Any other workspace is not found,
// whether it exists or not, so no one learns the names of workspaces they
// are not in. It runs in the generated routes, after their parameters are
// read.
func (s *Server) member(c *gin.Context) {
	ws, ok := c.Params.Get("workspace")
	if ok && !slices.Contains(s.cfg.Operator.Workspaces[ws].Members, c.GetString(userKey)) {
		s.fail(c, http.StatusNotFound, fmt.Errorf("workspace %s: not found", ws))
	}
}
