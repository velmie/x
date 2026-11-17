package mysql_test

import (
	"errors"
	"fmt"
	"testing"

	driver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"

	"github.com/velmie/x/svc/sqlconnection/mysql"
)

func TestIsDuplicateKey(t *testing.T) {
	duplicate := &driver.MySQLError{
		Number: 1062,
	}
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
		},
		{
			name: "plain error",
			err:  errors.New("write failed"),
		},
		{
			name: "duplicate key",
			err:  duplicate,
			want: true,
		},
		{
			name: "wrapped duplicate key",
			err:  fmt.Errorf("write: %w", duplicate),
			want: true,
		},
		{
			name: "joined duplicate key",
			err:  errors.Join(errors.New("write failed"), duplicate),
			want: true,
		},
		{
			name: "other mysql error",
			err: &driver.MySQLError{
				Number: 1452,
			},
		},
		{
			name: "message only",
			err:  errors.New("Duplicate entry"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, mysql.IsDuplicateKey(test.err))
		})
	}
}
