package models

import (
	"time"
)

type ClientEarning struct {
	ID            uint   `gorm:"primaryKey"`
	ClientAddress string `gorm:"index"`
	Earnings      int64
	Timestamp     time.Time `gorm:"index"`
}
