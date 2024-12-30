package models

type Client struct {
	Address               string `gorm:"primaryKey"`
	PubKey                string
	SolanaAddress         string `gorm:"index"`
	TotalLifetimeEarnings int64
}
