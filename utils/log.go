package utils

import (
	"fmt"
	"log/slog"
)

type LegacyLogger struct {
	Prefix string
	Level  slog.Level
}

func (logger LegacyLogger) Printf(format string, v ...any) {

	switch logger.Level {

	case slog.LevelInfo:
		slog.Info(logger.Prefix,
			slog.String("msg", fmt.Sprintf(format, v...)))

	case slog.LevelError:
		slog.Error(logger.Prefix,
			slog.String("msg", fmt.Sprintf(format, v...)))

	default:
		slog.Warn(logger.Prefix,
			slog.String("msg", fmt.Sprintf(format, v...)))
	}
}
