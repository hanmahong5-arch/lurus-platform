package temporal

import (
	"fmt"
	"log/slog"

	tlog "go.temporal.io/sdk/log"
)

// slogAdapter bridges Temporal SDK's log interface to Go's slog.
type slogAdapter struct{}

func newSlogAdapter() tlog.Logger { return &slogAdapter{} }

func (s *slogAdapter) Debug(msg string, kvs ...any) { slog.Debug(msg, kvs...) }
func (s *slogAdapter) Info(msg string, kvs ...any)  { slog.Info(msg, kvs...) }
func (s *slogAdapter) Warn(msg string, kvs ...any)  { slog.Warn(msg, kvs...) }
func (s *slogAdapter) Error(msg string, kvs ...any) { slog.Error(msg, kvs...) }

func (s *slogAdapter) With(kvs ...any) tlog.Logger {
	_ = fmt.Sprint(kvs...) // satisfy interface; slog is global
	return s
}
