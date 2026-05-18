package config

import "log/slog"

// Reloader re-reads a config file and applies hot-reloadable changes to
// mutable components. Currently supports: logging.level.
//
// Other config fields (origin URL, timeouts, cache size, etc.) require a
// process restart to take effect.
type Reloader struct {
	path     string
	levelVar *slog.LevelVar
}

// NewReloader creates a Reloader bound to the given config path and level variable.
func NewReloader(path string, lv *slog.LevelVar) *Reloader {
	return &Reloader{path: path, levelVar: lv}
}

// Reload re-reads and validates the config file, then applies hot-reloadable
// changes. Returns an error if the file cannot be read or fails validation.
func (r *Reloader) Reload() error {
	cfg, err := Load(r.path)
	if err != nil {
		return err
	}
	r.levelVar.Set(LevelFromString(cfg.Logging.Level))
	return nil
}

// LevelFromString converts a logging level string (debug/info/warn/error) to
// the corresponding slog.Level. Unrecognised values default to LevelInfo.
func LevelFromString(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
