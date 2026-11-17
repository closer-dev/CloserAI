// Package kernel — общее ядро (shared kernel): маленький набор доменных типов,
// которые разделяют все ограниченные контексты. Сюда попадает только то, без
// чего контексты не могут договориться между собой; никакой инфраструктуры.
package kernel

import "github.com/google/uuid"

type Role string

const (
	RoleOwner    Role = "owner"
	RoleOperator Role = "operator"
)

func (r Role) Valid() bool { return r == RoleOwner || r == RoleOperator }

// Identity — кто выполняет действие. Все контексты берут tenant_id только
// отсюда, а не из параметров запроса: так данные компаний не перемешиваются.
type Identity struct {
	UserID        uuid.UUID
	TenantID      uuid.UUID
	Role          Role
	EmailVerified bool
}
