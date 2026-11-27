package platformdb

import (
	"testing"

	"gorm.io/gorm/logger"
)

func TestGormConfigDisablesDefaultLogger(t *testing.T) {
	if got := gormConfig().Logger; got != logger.Discard {
		t.Fatalf("gorm logger = %#v, want logger.Discard", got)
	}
}
