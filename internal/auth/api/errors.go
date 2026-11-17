package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"closerai/internal/auth/domain"
	"closerai/internal/platform/httpx"
)

// Перевод доменных ошибок в HTTP. Домен ничего не знает о статусах и текстах для UI.
var errMap = map[error]*httpx.Error{
	domain.ErrInvalidCredentials: httpx.NewError(http.StatusUnauthorized, "неверный email или пароль"),
	domain.ErrEmailTaken:         httpx.NewError(http.StatusConflict, "пользователь с таким email уже существует"),
	domain.ErrUserNotFound:       httpx.ErrUnauthorized,
	domain.ErrInvalidRole:        httpx.NewError(http.StatusBadRequest, "неизвестная роль"),
	domain.ErrEmailNotVerified:   httpx.NewError(http.StatusForbidden, "email не подтверждён"),
	domain.ErrAlreadyVerified:    httpx.NewError(http.StatusConflict, "email уже подтверждён"),
	domain.ErrCodeInvalid:        httpx.NewError(http.StatusBadRequest, "неверный код"),
	domain.ErrCodeExpired:        httpx.NewError(http.StatusBadRequest, "код истёк или не запрашивался — отправьте новый"),
	domain.ErrCodeAttempts:       httpx.NewError(http.StatusTooManyRequests, "слишком много попыток — отправьте новый код"),
	domain.ErrEmptyCompanyName:   httpx.NewError(http.StatusBadRequest, "название компании не может быть пустым"),
}

func fail(c *gin.Context, err error) {
	for de, he := range errMap {
		if errors.Is(err, de) {
			httpx.Fail(c, he)
			return
		}
	}
	var early *domain.ResendTooEarlyError
	if errors.As(err, &early) {
		httpx.Fail(c, httpx.NewError(http.StatusTooManyRequests,
			fmt.Sprintf("повторно отправить код можно через %d с", int(early.Wait.Seconds())+1)))
		return
	}
	httpx.Fail(c, err)
}
