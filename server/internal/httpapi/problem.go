package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// problemContentType is the media type of RFC 9457 problem details.
const problemContentType = "application/problem+json"

// Problem is an RFC 9457 problem details object.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// WriteProblem aborts the request with an RFC 9457 problem details response.
// The type is "about:blank", so the title is the standard status text; detail
// explains this occurrence and must not reveal internals.
func WriteProblem(c *gin.Context, status int, detail string) {
	c.Header("Content-Type", problemContentType)
	c.AbortWithStatusJSON(status, Problem{
		Type:     "about:blank",
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Instance: c.Request.URL.Path,
	})
}
