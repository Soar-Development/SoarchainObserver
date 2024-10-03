package blockchain

import (
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/Soar-Robotics/SoarchainObserver/internal/models"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type BlockReader struct {
	Conn *websocket.Conn
	URL  string
	DB   *gorm.DB
}

func NewBlockReader(url string, db *gorm.DB) (*BlockReader, error) {
	br := &BlockReader{
		URL: url,
		DB:  db, // Assign the db parameter
	}
	err := br.Connect()
	if err != nil {
		return nil, err
	}
	return br, nil
}

func (br *BlockReader) Connect() error {
	log.Printf("Connecting to WebSocket URL: %s", br.URL)
	conn, _, err := websocket.DefaultDialer.Dial(br.URL, nil)
	if err != nil {
		log.Printf("Failed to connect to WebSocket: %v", err)
		return err
	}
	log.Println("Successfully connected to WebSocket")
	subscribeMsg := `{
        "jsonrpc": "2.0",
        "method": "subscribe",
        "id": 1,
        "params": {
            "query": "tm.event='Tx' AND message.action='runner_challenge'"
        }
    }`

	err = conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg))
	if err != nil {
		log.Printf("Failed to send subscription message: %v", err)
		conn.Close()
		return err
	}
	log.Println("Subscription message sent successfully")

	br.Conn = conn
	return nil
}

func (br *BlockReader) ReadBlocks(logger *log.Logger) {
	defer br.Conn.Close()
	for {
		_, message, err := br.Conn.ReadMessage()
		if err != nil {
			logger.Printf("Error reading message: %v", err)
			// Attempt to reconnect
			br.handleReconnection(logger)
			continue
		}

		// Process the message
		br.processMessage(message, logger)
	}
}

func (br *BlockReader) handleReconnection(logger *log.Logger) {
	logger.Println("Attempting to reconnect...")
	for {
		time.Sleep(5 * time.Second)
		err := br.Connect()
		if err != nil {
			logger.Printf("Reconnection failed: %v", err)
			continue
		}
		logger.Println("Reconnected successfully")
		break
	}
}

func (br *BlockReader) processMessage(message []byte, logger *log.Logger) {
	var msg map[string]interface{}
	err := json.Unmarshal(message, &msg)
	if err != nil {
		logger.Printf("Error parsing message: %v", err)
		return
	}

	result, ok := msg["result"].(map[string]interface{})
	if !ok {
		return
	}
	events, ok := result["events"].(map[string]interface{})
	if !ok {
		return
	}

	clientDataList, ok := events["message.client_data"].([]interface{})
	if !ok {
		return
	}

	for _, clientDataRaw := range clientDataList {
		clientDataJSON, ok := clientDataRaw.(string)
		if !ok {
			continue
		}
		var clientData struct {
			Address  string `json:"address"`
			Earnings string `json:"earnings"`
			PubKey   string `json:"pubkey"`
		}
		err = json.Unmarshal([]byte(clientDataJSON), &clientData)
		if err != nil {
			logger.Printf("Error parsing client data: %v", err)
			continue
		}
		earningsValue := parseEarnings(clientData.Earnings)
		timestamp := time.Now().UTC()

		// Start a database transaction
		tx := br.DB.Begin()

		// Upsert into Clients table
		var client models.Client
		result := tx.First(&client, "address = ?", clientData.Address)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				// Insert new client
				client = models.Client{
					Address:               clientData.Address,
					PubKey:                clientData.PubKey,
					TotalLifetimeEarnings: earningsValue,
				}
				if err := tx.Create(&client).Error; err != nil {
					tx.Rollback()
					logger.Printf("Error inserting client: %v", err)
					continue
				}
			} else {
				tx.Rollback()
				logger.Printf("Error querying client: %v", result.Error)
				continue
			}
		} else {
			// Update totalLifetimeEarnings
			client.TotalLifetimeEarnings += earningsValue
			if err := tx.Save(&client).Error; err != nil {
				tx.Rollback()
				logger.Printf("Error updating client: %v", err)
				continue
			}
		}

		// Insert into ClientEarnings table
		clientEarning := models.ClientEarning{
			ClientAddress: clientData.Address,
			Earnings:      earningsValue,
			Timestamp:     timestamp,
		}
		if err := tx.Create(&clientEarning).Error; err != nil {
			tx.Rollback()
			logger.Printf("Error inserting client earnings: %v", err)
			continue
		}

		// Commit transaction
		tx.Commit()

	}
}

func parseEarnings(earningsStr string) int64 {
	earningsStr = strings.TrimSuffix(earningsStr, "utsoar")
	earningsValue, err := strconv.ParseInt(earningsStr, 10, 64)
	if err != nil {
		return 0
	}
	return earningsValue
}
