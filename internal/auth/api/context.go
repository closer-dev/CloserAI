// Package api — HTTP-адаптер контекста auth (Gin): хендлеры, DTO, middleware
// и опубликованные для других контекстов хелперы FromContext / RequireRole.
package api

import (
	"slices"

	"github.com/gin-gonic/gin"

	"closerai/internal/kernel"
	"closerai/internal/platform/httpx"
)

const identityKey = "auth.identity"

// FromContext возвращает Identity, положенную Middleware. Вызывать только
// в обработчиках за Middleware: иначе это ошибка программиста и будет panic.
func FromContext(c *gin.Context) kernel.Identity {
	return c.MustGet(identityKey).(kernel.Identity)
}

// RequireRole ставится после Middleware и ограничивает доступ ролями.
func RequireRole(roles ...kernel.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !slices.Contains(roles, FromContext(c).Role) {
			httpx.Fail(c, httpx.ErrForbidden)
			return
		}
		c.Next()
	}
}
