package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Soar-Robotics/SoarchainObserver/internal/blockchain"
	"github.com/Soar-Robotics/SoarchainObserver/internal/config"
	"github.com/Soar-Robotics/SoarchainObserver/internal/models"
	"github.com/Soar-Robotics/SoarchainObserver/internal/utils"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	logger := utils.GetLogger()
	gin.SetMode(gin.ReleaseMode)

	// Load config
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: No .env file found or error loading it")
	}

	// Read database credentials from env
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		dbHost, dbUser, dbPassword, dbName, dbPort,
	)

	// Initialize database connection
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}

	// Set up DB connection pooling
	sqlDB, err := db.DB()
	if err != nil {
		logger.Fatalf("Failed to get database handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Migrate the schema
	if err := db.AutoMigrate(&models.Client{}, &models.ClientEarning{}); err != nil {
		logger.Fatalf("Failed to migrate database schema: %v", err)
	}

	// Initialize the BlockReader
	blockReader, err := blockchain.NewBlockReader(cfg.RPCEndpoint, db)
	if err != nil {
		logger.Fatalf("Failed to connect to WebSocket: %v", err)
	}

	// Channel to listen for OS signals
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

	// Graceful shutdown
	logger.Println("Shutting down observer...")

	// Close DB
	if err := sqlDB.Close(); err != nil {
		logger.Printf("Error closing DB: %v", err)
	}
}

// setupRouter defines all the endpoints
func setupRouter(db *gorm.DB) *gin.Engine {
	router := gin.Default()

	// allow CORS
	router.Use(cors.Default())

	// Inject DB into context
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})

	// query by address
	router.GET("/client/:address", getClientEarnings)

	// endpoints: query by solana address, pubkey
	router.GET("/client/solana/:solanaAddress", getClientBySolanaAddress)
	router.GET("/client/pubkey/:pubkey", getClientByPubKey)

	// average earnings over a period
	router.GET("/average", getAverageRewards)
	router.GET("/timeframe-earnings", getTimeframeEarnings)

	// New endpoints for daily aggregated status, latest rewards, and all rewards
	group := router.Group("/api/v1/miner")
	{
		group.GET("/status", GetMinerStatus)
		group.GET("/latest-rewards", GetLatestRewards)
		group.GET("/all-rewards", GetAllRewards)
	}

	return router
}

// ---------------------------------------------------------------------
// 1) /api/v1/miner/status
// ---------------------------------------------------------------------

// GetMinerStatus handles GET /api/v1/miner/status?wallet=<SOLANA_WALLET>
func GetMinerStatus(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	solanaWallet := c.Query("wallet")
	if solanaWallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'wallet' query param"})
		return
	}

	// 1) Find the most recent "challenge" from your ClientEarning table
	lastChallengeTime, err := fetchLastChallengeTime(db, solanaWallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 2) If no record found => consider it "Down"
	if lastChallengeTime.IsZero() {
		c.JSON(http.StatusOK, gin.H{
			"status": "Down",
			"issues": []string{"Offline"},
			"logs":   gin.H{"lastSeen": nil},
		})
		return
	}

	// 3) Compare with current time
	now := time.Now().UTC()
	diffMins := now.Sub(lastChallengeTime).Minutes()

	var status string
	var issues []string

	switch {
	case diffMins <= 2:
		status = "Up"
	case diffMins > 2 && diffMins < 5:
		status = "Degraded"
		issues = append(issues, "HighLatency")
	default:
		status = "Down"
		issues = append(issues, "Offline")
	}

	// 4) Build logs
	logs := gin.H{
		"lastSeen": lastChallengeTime.Format(time.RFC3339),
		"diffMins": diffMins,
	}

	c.JSON(http.StatusOK, gin.H{
		"status": status,
		"issues": issues,
		"logs":   logs,
	})
}

// fetchLastChallengeTime finds the newest (latest) `Timestamp` in ClientEarning
// for the given solanaWallet (which is presumably stored in `client_address`).
func fetchLastChallengeTime(db *gorm.DB, solanaWallet string) (time.Time, error) {
	var earning models.ClientEarning

	err := db.Where("client_address = ?", solanaWallet).
		Order("timestamp DESC").
		First(&earning).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// no challenge found
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return earning.Timestamp, nil
}

// ---------------------------------------------------------------------
// 2) /api/v1/miner/latest-rewards
// ---------------------------------------------------------------------

// GetLatestRewards handles GET /api/v1/miner/latest-rewards?wallet=<SOLANA_WALLET>
// Returns daily aggregated data for the last 7 days (or `ndays` if you change the code).
func GetLatestRewards(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	solanaWallet := c.Query("wallet")
	if solanaWallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'wallet' query param"})
		return
	}

	rewards, err := fetchRewardsForLastNDays(db, solanaWallet, 7)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, rewards)
}

// fetchRewardsForLastNDays aggregates by day (DATE(timestamp)) for the last `ndays` days.
func fetchRewardsForLastNDays(db *gorm.DB, solanaWallet string, ndays int) ([]map[string]interface{}, error) {
	type DailyAggregate struct {
		Day           time.Time `gorm:"column:day"`
		DailyEarnings int64     `gorm:"column:daily_earnings"`
	}

	// We'll look from `ndays` days ago up to now
	now := time.Now().UTC()
	cutoffDate := now.AddDate(0, 0, -ndays).Format("2006-01-02") // e.g. "2024-09-25"

	var aggregated []DailyAggregate

	// We GROUP BY date, summing earnings. Then only keep days >= cutoffDate.
	query := `
        SELECT DATE("timestamp") AS day,
               SUM(earnings)     AS daily_earnings
        FROM client_earnings
        WHERE client_address = ?
          AND DATE("timestamp") >= DATE(?)
        GROUP BY DATE("timestamp")
        ORDER BY DATE("timestamp") DESC
    `
	if err := db.Raw(query, solanaWallet, cutoffDate).Scan(&aggregated).Error; err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for _, row := range aggregated {
		// Example: if you store "earnings" as integer micro-something,
		// you might convert to float. e.g. float64(row.DailyEarnings)/1e6
		results = append(results, map[string]interface{}{
			"date":        row.Day.Format("2006-01-02"),
			"amount":      float64(row.DailyEarnings) / 1000000.0,
			"tokenSymbol": "SOAR",
		})
	}

	// If you'd like to ensure EXACTLY `ndays` entries, you can fill missing days or slice.
	// For now, we just return the days we have.

	return results, nil
}

// ---------------------------------------------------------------------
// 3) /api/v1/miner/all-rewards
// ---------------------------------------------------------------------

// GetAllRewards handles GET /api/v1/miner/all-rewards?wallet=<SOLANA_WALLET>
// It returns *daily aggregated* earnings for each calendar day.
func GetAllRewards(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	solanaWallet := c.Query("wallet")
	if solanaWallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'wallet' query param"})
		return
	}

	allRewards, err := fetchAllRewards(db, solanaWallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, allRewards)
}

// fetchAllRewards queries the entire ClientEarning history for a given wallet,
// but aggregates results by day (1 row per day).
func fetchAllRewards(db *gorm.DB, solanaWallet string) ([]map[string]interface{}, error) {
	type DailyAggregate struct {
		Day           time.Time `gorm:"column:day"`
		DailyEarnings int64     `gorm:"column:daily_earnings"`
	}

	var aggregated []DailyAggregate

	// GROUP BY date to get daily sums
	query := `
        SELECT DATE("timestamp") AS day,
               SUM(earnings)     AS daily_earnings
        FROM client_earnings
        WHERE client_address = ?
        GROUP BY DATE("timestamp")
        ORDER BY DATE("timestamp") ASC
    `
	if err := db.Raw(query, solanaWallet).Scan(&aggregated).Error; err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for _, row := range aggregated {
		results = append(results, map[string]interface{}{
			"date":        row.Day.Format("2006-01-02"),
			"amount":      float64(row.DailyEarnings) / 1000000.0,
			"tokenSymbol": "SOAR",
		})
	}
	return results, nil
}

// ---------------------------------------------------------------------
// Additional existing endpoints
// ---------------------------------------------------------------------

func getClientBySolanaAddress(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	solanaAddress := c.Param("solanaAddress")
	period := c.Query("period")

	var client models.Client
	result := db.First(&client, "solana_address = ?", solanaAddress)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
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
		Where("client_address = ? AND timestamp BETWEEN ? AND ?", client.SolanaAddress, startTime, endTime).
		Select("COALESCE(SUM(earnings), 0)").Scan(&earningsOverPeriod)

	c.JSON(http.StatusOK, gin.H{
		"solana_address":          client.SolanaAddress,
		"address":                 client.Address,
		"pubkey":                  client.PubKey,
		"total_lifetime_earnings": client.TotalLifetimeEarnings,
		"earnings_over_period":    earningsOverPeriod,
		"period":                  period,
	})
}

func getClientByPubKey(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	pubkey := c.Param("pubkey")
	period := c.Query("period")

	var client models.Client
	result := db.First(&client, "pub_key = ?", pubkey)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
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
		Where("client_address = ? AND timestamp BETWEEN ? AND ?", client.SolanaAddress, startTime, endTime).
		Select("COALESCE(SUM(earnings), 0)").Scan(&earningsOverPeriod)

	c.JSON(http.StatusOK, gin.H{
		"address":                 client.Address,
		"pubkey":                  client.PubKey,
		"solana_address":          client.SolanaAddress,
		"total_lifetime_earnings": client.TotalLifetimeEarnings,
		"earnings_over_period":    earningsOverPeriod,
		"period":                  period,
	})
}

func getAverageRewards(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	// 1) Read period from query, default to "1h"
	period := c.Query("period")
	if period == "" {
		period = "1h"
	}

	// 2) Parse it as a Go duration
	duration, err := time.ParseDuration(period)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid period format: %v", err)})
		return
	}

	// 3) Calculate the start/end times
	endTime := time.Now().UTC()
	startTime := endTime.Add(-duration)

	// 4) Query the average between startTime and endTime
	var result struct {
		AvgEarnings int64 `gorm:"column:avg_earnings"`
	}

	query := `
        SELECT AVG(earnings)::int AS avg_earnings
        FROM client_earnings
        WHERE timestamp BETWEEN ? AND ?
    `
	if err := db.Raw(query, startTime, endTime).Scan(&result).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 5) Return the JSON
	c.JSON(http.StatusOK, gin.H{
		"average":   float64(result.AvgEarnings) / 1000000.0,
		"period":    period,
		"startTime": startTime.Format(time.RFC3339),
		"endTime":   endTime.Format(time.RFC3339),
	})
}

// getTimeframeEarnings handles:
// GET /timeframe-earnings?wallet=<WALLET>&period=<duration>
// If period is not provided, it defaults to "1h".
// Interprets the sum of challenges in that window as the total if uptime is 100%.
func getTimeframeEarnings(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	wallet := c.Query("wallet")
	if wallet == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'wallet' query param"})
		return
	}

	periodStr := c.Query("period")
	if periodStr == "" {
		periodStr = "1h" // default
	}

	// Parse Go duration, e.g. "1h", "30m", "2h"
	dur, err := time.ParseDuration(periodStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid period format: %v", err)})
		return
	}

	// Define the timeframe: [startTime, endTime]
	endTime := time.Now().UTC()
	startTime := endTime.Add(-dur)

	// Query the DB to sum up all earnings in that interval
	var result struct {
		TotalEarnings int64 `gorm:"column:total_earnings"`
	}

	query := `
        SELECT COALESCE(SUM(earnings), 0) AS total_earnings
        FROM client_earnings
        WHERE client_address = ?
          AND timestamp BETWEEN ? AND ?
    `
	if err := db.Raw(query, wallet, startTime, endTime).Scan(&result).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert from integer micro-units to float if needed
	// e.g., if 1,000,000 micro = 1 token
	totalFloat := float64(result.TotalEarnings) / 1e6

	// Return JSON
	c.JSON(http.StatusOK, gin.H{
		"wallet":           wallet,
		"period":           periodStr,
		"start":            startTime.Format(time.RFC3339),
		"end":              endTime.Format(time.RFC3339),
		"estimatedEarning": totalFloat, // "if 100% uptime in this window"
		"tokenSymbol":      "SOAR",
	})
}

// getClientEarnings queries by core address
func getClientEarnings(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	address := c.Param("address")
	period := c.Query("period") // e.g., "1h", "24h"

	var client models.Client
	result := db.First(&client, "address = ?", address)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
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
		Where("client_address = ? AND timestamp BETWEEN ? AND ?", client.SolanaAddress, startTime, endTime).
		Select("COALESCE(SUM(earnings), 0)").Scan(&earningsOverPeriod)

	c.JSON(http.StatusOK, gin.H{
		"address":                 client.Address,
		"pubkey":                  client.PubKey,
		"total_lifetime_earnings": client.TotalLifetimeEarnings,
		"earnings_over_period":    earningsOverPeriod,
		"period":                  period,
	})
}
