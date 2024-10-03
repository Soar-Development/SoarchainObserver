package main

import (
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Soar-Robotics/SoarchainObserver/internal/blockchain"
	"github.com/Soar-Robotics/SoarchainObserver/internal/config"
	"github.com/Soar-Robotics/SoarchainObserver/internal/models"
	"github.com/Soar-Robotics/SoarchainObserver/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	logger := utils.GetLogger()

	// Load configuration
	config, err := config.LoadConfig("config.json")
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database connection
	dsn := "host=localhost user=alp password=Alp.py1320 dbname=observer port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}

	// Set up database connection pooling
	sqlDB, err := db.DB()
	if err != nil {
		logger.Fatalf("Failed to get database handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Migrate the schema
	err = db.AutoMigrate(&models.Client{}, &models.ClientEarning{})
	if err != nil {
		logger.Fatalf("Failed to migrate database schema: %v", err)
	}

	// Initialize the BlockReader with the database connection
	blockReader, err := blockchain.NewBlockReader(config.RPCEndpoint, db)
	if err != nil {
		logger.Fatalf("Failed to connect to WebSocket: %v", err)
	}

	// Channel to listen for termination signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Start the observer in a separate goroutine
	go func() {
		logger.Println("Connected to WebSocket, starting to read blocks...")
		blockReader.ReadBlocks(logger)
	}()

	// Start the API server in a separate goroutine
	go func() {
		router := setupRouter(db)
		logger.Println("Starting API server on port 8080")
		if err := router.Run(":8080"); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Failed to run API server: %v", err)
		}
	}()

	// Block until a signal is received
	<-stop

	// Handle cleanup here if needed
	logger.Println("Shutting down...")

	// Close database connection
	sqlDB.Close()
}

func setupRouter(db *gorm.DB) *gin.Engine {
	router := gin.Default()

	// Inject the database into handlers via middleware
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})

	router.GET("/client/:address", getClientEarnings)

	return router
}

func getClientEarnings(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	address := c.Param("address")
	period := c.Query("period") // e.g., "1h", "24h"

	var client models.Client
	result := db.First(&client, "address = ?", address)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		}
		return
	}

	// Default period to last 1 hour if not specified
	if period == "" {
		period = "1h"
	}
	duration, err := time.ParseDuration(period)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid period format"})
		return
	}

	endTime := time.Now().UTC()
	startTime := endTime.Add(-duration)

	var earningsOverPeriod int64
	db.Model(&models.ClientEarning{}).
		Where("client_address = ? AND timestamp BETWEEN ? AND ?", address, startTime, endTime).
		Select("COALESCE(SUM(earnings), 0)").Scan(&earningsOverPeriod)

	c.JSON(http.StatusOK, gin.H{
		"address":                 client.Address,
		"pubkey":                  client.PubKey,
		"total_lifetime_earnings": client.TotalLifetimeEarnings,
		"earnings_over_period":    earningsOverPeriod,
		"period":                  period,
	})
}
