package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"closerai/internal/auth/app"
	"closerai/internal/auth/domain"
	"closerai/internal/kernel"
	"closerai/internal/platform/httpx"
)

type Config struct {
	// SecureCookie=true, если панель открывается по HTTPS.
	SecureCookie bool
	SessionTTL   time.Duration
}

type Handler struct {
	svc *app.Service
	cfg Config
}

func NewHandler(svc *app.Service, cfg Config) *Handler { return &Handler{svc: svc, cfg: cfg} }

func (h *Handler) Routes(r *gin.RouterGroup) {
	a := r.Group("/auth")
	a.POST("/register", h.register)
	a.POST("/login", h.login)
	a.POST("/logout", h.logout)

	// доступно и до подтверждения email
	s := r.Group("", h.sessionMiddleware())
	s.GET("/me", h.me)
	s.POST("/auth/verify", h.verify)
	s.POST("/auth/resend", h.resend)

	p := r.Group("", h.Middleware())
	p.GET("/users", h.listUsers)
	p.POST("/users", RequireRole(kernel.RoleOwner), h.createUser)
}

// ---------- DTO ----------

type registerRequest struct {
	CompanyName string `json:"company_name" binding:"required,max=200"`
	Name        string `json:"name" binding:"required,max=200"`
	Email       string `json:"email" binding:"required,email,max=254"`
	Password    string `json:"password" binding:"required,min=8,max=72"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type verifyRequest struct {
	Code string `json:"code" binding:"required,len=6,numeric"`
}

type createUserRequest struct {
	Name     string      `json:"name" binding:"required,max=200"`
	Email    string      `json:"email" binding:"required,email,max=254"`
	Password string      `json:"password" binding:"required,min=8,max=72"`
	Role     kernel.Role `json:"role" binding:"required,oneof=owner operator"`
}

type userResponse struct {
	ID            uuid.UUID   `json:"id"`
	TenantID      uuid.UUID   `json:"tenant_id"`
	Email         string      `json:"email"`
	Name          string      `json:"name"`
	Role          kernel.Role `json:"role"`
	EmailVerified bool        `json:"email_verified"`
	CreatedAt     time.Time   `json:"created_at"`
}

func toResponse(u domain.User) userResponse {
	return userResponse{
		ID: u.ID, TenantID: u.TenantID, Email: u.Email, Name: u.Name, Role: u.Role,
		EmailVerified: u.EmailVerified(), CreatedAt: u.CreatedAt,
	}
}

// ---------- handlers ----------

func (h *Handler) register(c *gin.Context) {
	var in registerRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	u, tok, err := h.svc.Register(c.Request.Context(), app.RegisterCmd(in))
	if err != nil {
		fail(c, err)
		return
	}
	h.setSession(c, tok)
	c.JSON(http.StatusCreated, toResponse(u))
}

func (h *Handler) login(c *gin.Context) {
	var in loginRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	u, tok, err := h.svc.Login(c.Request.Context(), app.LoginCmd(in))
	if err != nil {
		fail(c, err)
		return
	}
	h.setSession(c, tok)
	c.JSON(http.StatusOK, toResponse(u))
}

func (h *Handler) logout(c *gin.Context) {
	h.clearSession(c)
	c.Status(http.StatusNoContent)
}

func (h *Handler) verify(c *gin.Context) {
	var in verifyRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.NewError(http.StatusBadRequest, "код должен состоять из 6 цифр"))
		return
	}
	u, tok, err := h.svc.VerifyEmail(c.Request.Context(), FromContext(c), in.Code)
	if err != nil {
		fail(c, err)
		return
	}
	h.setSession(c, tok)
	c.JSON(http.StatusOK, toResponse(u))
}

func (h *Handler) resend(c *gin.Context) {
	if err := h.svc.ResendVerifyCode(c.Request.Context(), FromContext(c)); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) me(c *gin.Context) {
	u, err := h.svc.Me(c.Request.Context(), FromContext(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(u))
}

func (h *Handler) listUsers(c *gin.Context) {
	users, err := h.svc.ListUsers(c.Request.Context(), FromContext(c).TenantID)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]userResponse, len(users))
	for i, u := range users {
		out[i] = toResponse(u)
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) createUser(c *gin.Context) {
	var in createUserRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	u, err := h.svc.CreateUser(c.Request.Context(), FromContext(c).TenantID, app.CreateUserCmd(in))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toResponse(u))
}
