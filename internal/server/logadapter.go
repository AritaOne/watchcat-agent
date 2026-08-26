package server

import (
	"fmt"
	"log"
	"log/slog"
	"strings"
)

// slogErrorLog adapts slog to the two logging interfaces the stdlib and
// promhttp expect: promhttp.Logger (Println) and *log.Logger (io.Writer).
type slogErrorLog struct{ log *slog.Logger }

func (s slogErrorLog) Println(v ...any) {
	s.log.Error(strings.TrimSpace(fmt.Sprintln(v...)))
}

func (s slogErrorLog) Write(p []byte) (int, error) {
	s.log.Error(strings.TrimSpace(string(p)))
	return len(p), nil
}

func (s slogErrorLog) stdLogger() *log.Logger { return log.New(s, "", 0) }
