// internal/models/epoch_earnings.go

package models

import "time"

type EpochEarnings struct {
	ID            uint      `gorm:"primaryKey"`
	ClientAddress string    `gorm:"index"`
	EpochNumber   int64     `gorm:"index"` // e.g. "33"
	StartTime     time.Time // "2025-01-16T09:04:54Z"
	EndTime       time.Time // StartTime + 86400s
	TotalEarnings int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
