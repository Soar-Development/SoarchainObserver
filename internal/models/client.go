package models

import "time"

type Client struct {
	Address               string `gorm:"primaryKey"`
	PubKey                string
	SolanaAddress         string `gorm:"index"`
	TotalLifetimeEarnings int64
	LastChallengeTime     time.Time `gorm:"index"` // New field
}
