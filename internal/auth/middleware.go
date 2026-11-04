package auth

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"closerai/internal/platform/httpx"
)

const cookieName = "closer_session"

// Middleware пропускает только запросы с валидным JWT (cookie или
// заголовок Authorization: Bearer) от пользователя с подтверждённым email
// и кладёт Identity в контекст.
func (m *Module) Middleware() gin.HandlerFunc { return m.authenticate(true) }

// sessionMiddleware — как Middleware, но пускает и неподтверждённых:
// нужен для /me и самих эндпоинтов подтверждения.
func (m *Module) sessionMiddleware() gin.HandlerFunc { return m.authenticate(false) }

func (m *Module) authenticate(requireVerified bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, _ := c.Cookie(cookieName)
		if raw == "" {
			raw, _ = strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		}
		if raw == "" {
			httpx.Fail(c, httpx.ErrUnauthorized)
			return
		}
		id, err := m.svc.Authenticate(raw)
		if err != nil {
			httpx.Fail(c, httpx.ErrUnauthorized)
			return
		}
		if requireVerified && !id.EmailVerified {
			httpx.Fail(c, ErrEmailNotVerified)
			return
		}
		c.Set(identityKey, id)
		c.Next()
	}
}

// RequireRole ставится после Middleware и ограничивает доступ ролями.
func RequireRole(roles ...Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !slices.Contains(roles, FromContext(c).Role) {
			httpx.Fail(c, httpx.ErrForbidden)
			return
		}
		c.Next()
	}
}

func (m *Module) setSession(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(tokenTTL / time.Second),
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Module) clearSession(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}
