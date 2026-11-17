package mysql

import (
	"errors"

	driver "github.com/go-sql-driver/mysql"
)

// IsDuplicateKey reports whether err or an error it wraps is a
// [driver.MySQLError] with number 1062 (ER_DUP_ENTRY): the statement would
// duplicate a primary or unique key value. Error text is not inspected. When a
// duplicate becomes an application error, wrap err so the driver error stays
// available to [errors.As] and logs.
func IsDuplicateKey(err error) bool {
	var mysqlErr *driver.MySQLError

	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
