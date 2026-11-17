package mysql

import (
	"errors"

	driver "github.com/go-sql-driver/mysql"
)

// IsDuplicateKey reports whether err or an error it wraps is MySQL error 1062
// (ER_DUP_ENTRY), a duplicate primary or unique key. Callers should keep the
// original error in the chain.
func IsDuplicateKey(err error) bool {
	var mysqlErr *driver.MySQLError

	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
