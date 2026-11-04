package httpx

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Error — ошибка, которую можно показать клиенту API. Всё остальное отдаётся как 500.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func NewError(status int, msg string) *Error { return &Error{Status: status, Message: msg} }

var (
	ErrUnauthorized = NewError(http.StatusUnauthorized, "unauthorized")
	ErrForbidden    = NewError(http.StatusForbidden, "forbidden")
	ErrNotFound     = NewError(http.StatusNotFound, "not found")
)

// Fail отвечает JSON-ошибкой {"error": "..."}. Неизвестные ошибки логируются,
// а клиенту уходит общий текст, чтобы не раскрывать детали.
func Fail(c *gin.Context, err error) {
	var he *Error
	if errors.As(err, &he) {
		c.AbortWithStatusJSON(he.Status, gin.H{"error": he.Message})
		return
	}
	slog.ErrorContext(c.Request.Context(), "request failed", "path", c.FullPath(), "err", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
}

// BadRequest — короткая запись для ошибок валидации входных данных.
func BadRequest(c *gin.Context, err error) {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
