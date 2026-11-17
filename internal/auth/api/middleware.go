package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"closerai/internal/auth/domain"
	"closerai/internal/platform/httpx"
)

const cookieName = "closer_session"

// Middleware пропускает только запросы с валидным JWT (cookie или
// заголовок Authorization: Bearer) от пользователя с подтверждённым email
// и кладёт Identity в контекст.
func (h *Handler) Middleware() gin.HandlerFunc { return h.authenticate(true) }

// sessionMiddleware — как Middleware, но пускает и неподтверждённых:
// нужен для /me и самих эндпоинтов подтверждения.
func (h *Handler) sessionMiddleware() gin.HandlerFunc { return h.authenticate(false) }

func (h *Handler) authenticate(requireVerified bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, _ := c.Cookie(cookieName)
		if raw == "" {
			raw, _ = strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		}
		if raw == "" {
			httpx.Fail(c, httpx.ErrUnauthorized)
			return
		}
		id, err := h.svc.Authenticate(raw)
		if err != nil {
			httpx.Fail(c, httpx.ErrUnauthorized)
			return
		}
		if requireVerified && !id.EmailVerified {
			fail(c, domain.ErrEmailNotVerified)
			return
		}
		c.Set(identityKey, id)
		c.Next()
	}
}

func (h *Handler) setSession(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.cfg.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   h.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSession(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}
