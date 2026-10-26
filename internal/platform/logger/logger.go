// Package logger настраивает slog: текстовый вывод в dev, JSON в остальных окружениях.
package logger

import (
	"log/slog"
	"os"
)

func New(dev bool) *slog.Logger {
	var h slog.Handler
	if dev {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	return slog.New(h)
}
