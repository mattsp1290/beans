//go:build tools

package beans

import (
	// Pin upcoming GORM migration runtime and integration-test dependencies until
	// implementation packages import them directly.
	_ "github.com/glebarez/sqlite"
	_ "github.com/testcontainers/testcontainers-go/modules/mysql"
	_ "gorm.io/datatypes"
	_ "gorm.io/driver/mysql"
	_ "gorm.io/driver/postgres"
	_ "gorm.io/gorm"
)
