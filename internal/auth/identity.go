// Package auth — регистрация компании, вход, JWT в httpOnly cookie, роли и
// middleware, который кладёт в контекст текущего пользователя и его tenant_id.
package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Role string

const (
	RoleOwner    Role = "owner"
	RoleOperator Role = "operator"
)

func (r Role) Valid() bool { return r == RoleOwner || r == RoleOperator }

// Identity — кто выполняет запрос. Все модули берут tenant_id только отсюда,
// а не из параметров запроса: так данные компаний не перемешиваются.
type Identity struct {
	UserID        uuid.UUID
	TenantID      uuid.UUID
	Role          Role
	EmailVerified bool
}

const identityKey = "auth.identity"

// FromContext возвращает Identity, положенную Middleware. Вызывать только
// в обработчиках за Middleware: иначе это ошибка программиста и будет panic.
func FromContext(c *gin.Context) Identity {
	return c.MustGet(identityKey).(Identity)
}
