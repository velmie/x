package mysql

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

const utcTimeZone = "'+00:00'"

var (
	errParseTimeUTC = errors.New("params or driver options: parseTime must remain true")
	errLocationUTC  = errors.New("params or driver options: loc must remain UTC")
	errTimeZoneUTC  = errors.New("params or driver options: time_zone must remain '+00:00'")
)

// WithDriverLogger routes driver diagnostics to logger as warning records.
func WithDriverLogger(logger *slog.Logger) driver.Option {
	return func(cfg *driver.Config) error {
		cfg.Logger = driverLogger{logger: logger}

		return nil
	}
}

// RequireUTC enforces UTC time parsing and session timezone. Apply it after
// options that customize time settings. The connection constructor starts with
// parseTime enabled and UTC as the driver's location.
func RequireUTC() driver.Option {
	return func(cfg *driver.Config) error {
		if !cfg.ParseTime {
			return errParseTimeUTC
		}
		if cfg.Loc != time.UTC {
			return errLocationUTC
		}
		if zone, present := cfg.Params["time_zone"]; present && zone != utcTimeZone {
			return errTimeZoneUTC
		}
		if cfg.Params == nil {
			cfg.Params = make(map[string]string)
		}
		cfg.Params["time_zone"] = utcTimeZone

		return nil
	}
}

type driverLogger struct {
	logger *slog.Logger
}

func (l driverLogger) Print(values ...any) {
	var causes []error
	for _, value := range values {
		if err, ok := value.(error); ok {
			causes = append(causes, err)
		}
	}
	l.logger.Warn(
		"Database driver diagnostic",
		slog.String("operation", "manage_database"),
		slog.String("component", "database"),
		slog.String("dependency", "mysql"),
		slog.String("stage", "driver"),
		slog.String("outcome", "failed"),
		slog.String("diagnostic", fmt.Sprint(values...)),
		slog.Any("error", errors.Join(causes...)),
	)
}
