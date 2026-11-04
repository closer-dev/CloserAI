package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"closerai/internal/platform/httpx"
)

type Module struct {
	svc          *Service
	secureCookie bool
}

// NewModule: secureCookie=true, если панель открывается по HTTPS.
func NewModule(svc *Service, secureCookie bool) *Module {
	return &Module{svc: svc, secureCookie: secureCookie}
}

func (m *Module) Routes(r *gin.RouterGroup) {
	a := r.Group("/auth")
	a.POST("/register", m.register)
	a.POST("/login", m.login)
	a.POST("/logout", m.logout)

	// доступно и до подтверждения email
	s := r.Group("", m.sessionMiddleware())
	s.GET("/me", m.me)
	s.POST("/auth/verify", m.verify)
	s.POST("/auth/resend", m.resend)

	p := r.Group("", m.Middleware())
	p.GET("/users", m.listUsers)
	p.POST("/users", RequireRole(RoleOwner), m.createUser)
}

func (m *Module) register(c *gin.Context) {
	var in RegisterInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	u, tok, err := m.svc.Register(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m.setSession(c, tok)
	c.JSON(http.StatusCreated, u)
}

func (m *Module) login(c *gin.Context) {
	var in LoginInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	u, tok, err := m.svc.Login(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m.setSession(c, tok)
	c.JSON(http.StatusOK, u)
}

func (m *Module) logout(c *gin.Context) {
	m.clearSession(c)
	c.Status(http.StatusNoContent)
}

func (m *Module) verify(c *gin.Context) {
	var in VerifyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.NewError(http.StatusBadRequest, "код должен состоять из 6 цифр"))
		return
	}
	u, tok, err := m.svc.VerifyEmail(c.Request.Context(), FromContext(c), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	m.setSession(c, tok)
	c.JSON(http.StatusOK, u)
}

func (m *Module) resend(c *gin.Context) {
	if err := m.svc.ResendVerifyCode(c.Request.Context(), FromContext(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (m *Module) me(c *gin.Context) {
	u, err := m.svc.Me(c.Request.Context(), FromContext(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

func (m *Module) listUsers(c *gin.Context) {
	users, err := m.svc.ListUsers(c.Request.Context(), FromContext(c).TenantID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, users)
}

func (m *Module) createUser(c *gin.Context) {
	var in CreateUserInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	u, err := m.svc.CreateUser(c.Request.Context(), FromContext(c).TenantID, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, u)
}
