package match

import (
	"log/slog"
	"os"
)

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

// SetLogger allows setting a custom logger
func SetLogger(l *slog.Logger) {
	logger = l
}
