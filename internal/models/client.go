package models

type Client struct {
	Address               string `gorm:"primaryKey"`
	PubKey                string
	TotalLifetimeEarnings int64
}
