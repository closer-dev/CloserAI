// Package api — HTTP-адаптер контекста telegram: приём вебхуков Telegram
// (POST /tg/webhook/:botID). Маршрут публичный: подлинность запроса
// проверяется по секрету в заголовке X-Telegram-Bot-Api-Secret-Token.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"closerai/internal/telegram/app"
	"closerai/internal/telegram/domain"
	"closerai/internal/telegram/infra/tgapi"
)

const (
	SecretHeader   = "X-Telegram-Bot-Api-Secret-Token"
	maxWebhookBody = 1 << 20
)

type Handler struct {
	svc *app.Service
}

func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes регистрирует вебхук на корневом роутере (вне /api и без auth).
func (h *Handler) Routes(r gin.IRouter) {
	r.POST("/tg/webhook/:botID", h.webhook)
}

// webhook отвечает 200, когда апдейт принят или сознательно пропущен; 5xx —
// когда его надо прислать повторно (Telegram повторит сам).
func (h *Handler) webhook(c *gin.Context) {
	botID, err := uuid.Parse(c.Param("botID"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBody))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	var u models.Update
	if err := json.Unmarshal(body, &u); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	err = h.svc.HandleWebhook(c.Request.Context(), botID, c.GetHeader(SecretHeader), tgapi.ToDomain(&u))
	switch {
	case err == nil:
		c.Status(http.StatusOK)
	case errors.Is(err, domain.ErrBotNotFound):
		c.Status(http.StatusNotFound)
	case errors.Is(err, domain.ErrBadSecret):
		c.Status(http.StatusUnauthorized)
	default:
		slog.ErrorContext(c.Request.Context(), "telegram: вебхук не обработан",
			"bot_id", botID, "update_id", u.ID, "err", err)
		c.Status(http.StatusInternalServerError)
	}
}
