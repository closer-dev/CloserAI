package tenant

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"closerai/internal/auth"
	"closerai/internal/platform/httpx"
)

type Module struct {
	svc *Service
}

func NewModule(svc *Service) *Module { return &Module{svc: svc} }

// Routes регистрирует маршруты модуля. Группа уже защищена auth.Middleware.
func (m *Module) Routes(r *gin.RouterGroup) {
	r.GET("/tenant", m.get)
}

func (m *Module) get(c *gin.Context) {
	t, err := m.svc.Get(c.Request.Context(), auth.FromContext(c).TenantID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}
