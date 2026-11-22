// Package api — HTTP-адаптер контекста bot (Gin): /api/bots.
package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authapi "closerai/internal/auth/api"
	"closerai/internal/bot/app"
	"closerai/internal/bot/domain"
	"closerai/internal/kernel"
	"closerai/internal/platform/httpx"
)

type Handler struct {
	svc *app.Service
}

func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes регистрирует маршруты. Группа уже защищена auth Middleware:
// смотреть ботов могут все сотрудники, менять — только владелец.
func (h *Handler) Routes(r *gin.RouterGroup) {
	g := r.Group("/bots")
	g.GET("", h.list)
	g.GET("/:id", h.get)

	owner := g.Group("", authapi.RequireRole(kernel.RoleOwner))
	owner.POST("", h.connect)
	owner.PATCH("/:id", h.update)
	owner.DELETE("/:id", h.delete)
}

// ---------- DTO ----------

// Длины проверяет домен (в символах), здесь — только грубая защита от мусора.
type connectRequest struct {
	Token            string   `json:"token" binding:"required,max=200"`
	SystemPrompt     *string  `json:"system_prompt" binding:"omitempty,max=20000"`
	Greeting         *string  `json:"greeting" binding:"omitempty,max=5000"`
	HandoffThreshold *float32 `json:"handoff_threshold"`
}

type updateRequest struct {
	Token            *string  `json:"token" binding:"omitempty,max=200"`
	SystemPrompt     *string  `json:"system_prompt" binding:"omitempty,max=20000"`
	Greeting         *string  `json:"greeting" binding:"omitempty,max=5000"`
	HandoffThreshold *float32 `json:"handoff_threshold"`
	IsActive         *bool    `json:"is_active"`
}

// botResponse никогда не содержит токен и секрет вебхука.
type botResponse struct {
	ID               uuid.UUID `json:"id"`
	TgBotID          int64     `json:"tg_bot_id"`
	Username         string    `json:"username"`
	Link             string    `json:"link"`
	SystemPrompt     string    `json:"system_prompt"`
	Greeting         string    `json:"greeting"`
	HandoffThreshold float32   `json:"handoff_threshold"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
}

func toResponse(b domain.Bot) botResponse {
	return botResponse{
		ID:               b.ID,
		TgBotID:          b.TgBotID,
		Username:         b.Username,
		Link:             "https://t.me/" + b.Username,
		SystemPrompt:     b.Settings.SystemPrompt,
		Greeting:         b.Settings.Greeting,
		HandoffThreshold: b.Settings.HandoffThreshold,
		IsActive:         b.IsActive,
		CreatedAt:        b.CreatedAt,
	}
}

// ---------- хендлеры ----------

func (h *Handler) list(c *gin.Context) {
	bots, err := h.svc.List(c.Request.Context(), authapi.FromContext(c).TenantID)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]botResponse, len(bots))
	for i, b := range bots {
		out[i] = toResponse(b)
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) get(c *gin.Context) {
	id, ok := botID(c)
	if !ok {
		return
	}
	b, err := h.svc.Get(c.Request.Context(), authapi.FromContext(c).TenantID, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(b))
}

func (h *Handler) connect(c *gin.Context) {
	var req connectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	b, err := h.svc.Connect(c.Request.Context(), authapi.FromContext(c).TenantID, app.ConnectInput{
		Token: req.Token,
		Settings: domain.SettingsPatch{
			SystemPrompt: req.SystemPrompt, Greeting: req.Greeting, HandoffThreshold: req.HandoffThreshold,
		},
	})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toResponse(b))
}

func (h *Handler) update(c *gin.Context) {
	id, ok := botID(c)
	if !ok {
		return
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, err)
		return
	}
	b, err := h.svc.Update(c.Request.Context(), authapi.FromContext(c).TenantID, id, app.UpdateInput{
		Token: req.Token,
		Settings: domain.SettingsPatch{
			SystemPrompt: req.SystemPrompt, Greeting: req.Greeting, HandoffThreshold: req.HandoffThreshold,
		},
		IsActive: req.IsActive,
	})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toResponse(b))
}

func (h *Handler) delete(c *gin.Context) {
	id, ok := botID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), authapi.FromContext(c).TenantID, id); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// botID разбирает :id; кривой id — это просто «не найдено».
func botID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		fail(c, domain.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

// ---------- ошибки ----------

var errMap = map[error]*httpx.Error{
	domain.ErrNotFound:            httpx.NewError(http.StatusNotFound, "бот не найден"),
	domain.ErrInvalidToken:        httpx.NewError(http.StatusBadRequest, "неверный формат токена: скопируйте его из @BotFather целиком"),
	domain.ErrTokenRejected:       httpx.NewError(http.StatusBadRequest, "Telegram не принял токен: проверьте, что он актуален"),
	domain.ErrTokenMismatch:       httpx.NewError(http.StatusBadRequest, "это токен другого бота"),
	domain.ErrAlreadyConnected:    httpx.NewError(http.StatusConflict, "этот бот уже подключён"),
	domain.ErrTelegramUnavailable: httpx.NewError(http.StatusBadGateway, "Telegram сейчас недоступен, попробуйте позже"),
	domain.ErrPromptTooLong:       httpx.NewError(http.StatusBadRequest, "системный промпт слишком длинный"),
	domain.ErrGreetingTooLong:     httpx.NewError(http.StatusBadRequest, "приветствие слишком длинное"),
	domain.ErrInvalidThreshold:    httpx.NewError(http.StatusBadRequest, "порог передачи оператору должен быть от 0 до 1"),
}

func fail(c *gin.Context, err error) {
	for de, he := range errMap {
		if errors.Is(err, de) {
			if de == domain.ErrTelegramUnavailable {
				// подробности (без токена) — в лог, пользователю — общий текст
				slog.WarnContext(c.Request.Context(), "bot: telegram недоступен", "err", err)
			}
			httpx.Fail(c, he)
			return
		}
	}
	var we *domain.WebhookError
	if errors.As(err, &we) {
		httpx.Fail(c, httpx.NewError(http.StatusBadGateway, "Telegram не принял вебхук: "+we.Reason))
		return
	}
	httpx.Fail(c, err)
}
