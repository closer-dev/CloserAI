// Package api — HTTP-адаптер контекста tenant (Gin): DTO, хендлеры, маршруты.
package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authapi "closerai/internal/auth/api"
	"closerai/internal/platform/httpx"
	"closerai/internal/tenant/app"
	"closerai/internal/tenant/domain"
)

type Handler struct {
	svc *app.Service
}

func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes регистрирует маршруты. Группа уже защищена auth Middleware.
func (h *Handler) Routes(r *gin.RouterGroup) {
	r.GET("/tenant", h.get)
}

type tenantResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *Handler) get(c *gin.Context) {
	t, err := h.svc.Get(c.Request.Context(), authapi.FromContext(c).TenantID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, tenantResponse{ID: t.ID, Name: t.Name, Plan: t.Plan, CreatedAt: t.CreatedAt})
}

func fail(c *gin.Context, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		err = httpx.ErrNotFound
	}
	httpx.Fail(c, err)
}
