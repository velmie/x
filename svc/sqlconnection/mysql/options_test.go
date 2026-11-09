package mysql

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
)

func TestRequireUTC(t *testing.T) {
	for _, tc := range []struct{ params, want string }{
		{params: "", want: ""},
		{params: "parseTime=false", want: "parseTime"},
		{params: "loc=America%2FNew_York", want: "loc"},
		{params: "time_zone=%27SYSTEM%27", want: "time_zone"},
	} {
		cfg := Config{Params: tc.params, DriverOptions: []driver.Option{RequireUTC()}}
		dc, err := prepareDriverConfig(&cfg)
		if tc.want != "" {
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: %v", tc.params, err)
			}

			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !dc.ParseTime || dc.Loc != time.UTC || dc.Params["time_zone"] != "'+00:00'" || dc.Timeout != 0 {
			t.Fatalf("incorrect UTC config: %+v", dc)
		}
	}
}

func TestWithDriverLogger(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	dc := driver.NewConfig()
	if err := dc.Apply(WithDriverLogger(logger)); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("driver cause marker")
	dc.Logger.Print("driver failed: ", cause)
	for _, value := range []string{`"level":"WARN"`, `"stage":"driver"`, `"error":"driver cause marker"`, "driver failed"} {
		if !strings.Contains(out.String(), value) {
			t.Fatalf("missing %q: %s", value, out.String())
		}
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("expected one record: %s", out.String())
	}
}
