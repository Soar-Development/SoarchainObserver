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

// BlockReader manages the WebSocket connection and DB
type BlockReader struct {
	Conn *websocket.Conn
	URL  string
	DB   *gorm.DB
}

// NewBlockReader initializes a BlockReader with a WebSocket connection to `url`.
func NewBlockReader(url string, db *gorm.DB) (*BlockReader, error) {
	br := &BlockReader{
		URL: url,
		DB:  db, // Assign the db parameter
	}
	if err := br.Connect(); err != nil {
		return nil, err
	}
	return br, nil
}

// Connect dials the WebSocket and subscribes to runner_challenge
func (br *BlockReader) Connect() error {
	log.Printf("Connecting to WebSocket URL: %s", br.URL)
	conn, _, err := websocket.DefaultDialer.Dial(br.URL, nil)
	if err != nil {
		log.Printf("Failed to connect to WebSocket: %v", err)
		return err
	}
	log.Println("Successfully connected to WebSocket")

	// Subscription message for runner_challenge Tx events
	subscribeMsg := `{
        "jsonrpc": "2.0",
        "method": "subscribe",
        "id": 1,
        "params": {
            "query": "tm.event='Tx' AND message.action='runner_challenge'"
        }
    }`

	if err := conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg)); err != nil {
		log.Printf("Failed to send subscription message: %v", err)
		conn.Close()
		return err
	}
	log.Println("Subscription message sent successfully")

	br.Conn = conn
	return nil
}

// ReadBlocks continuously reads from the WebSocket, processes messages, and handles reconnections
func (br *BlockReader) ReadBlocks(logger *log.Logger) {
	defer br.Conn.Close()
	for {
		// Read a new message
		_, message, err := br.Conn.ReadMessage()
		if err != nil {
			logger.Printf("Error reading message: %v", err)
			br.handleReconnection(logger)
			continue
		}
		log.Println("Received message:", string(message))
		// Process the message
		br.processMessage(message, logger)

	}
}

// handleReconnection attempts to reconnect after an error
func (br *BlockReader) handleReconnection(logger *log.Logger) {
	logger.Println("Attempting to reconnect...")
	for {
		time.Sleep(5 * time.Second)
		if err := br.Connect(); err != nil {
			logger.Printf("Reconnection failed: %v", err)
			continue
		}
		logger.Println("Reconnected successfully")
		break
	}
}

// processMessage parses the raw message, extracts clients data, upserts DB rows, etc.
func (br *BlockReader) processMessage(message []byte, logger *log.Logger) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
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

	// 1) Retrieve list of client_data from events
	clientDataList, ok := events["message.client_data"].([]interface{})
	if !ok {
		// no client_data in this event, skip
		return
	}

	// 2) Retrieve the corresponding solana_address from events if it’s in the same subEvent
	solanaAddressList, _ := events["solana_address"].([]interface{})

	log.Println("Client Data list:", clientDataList)

	for i, clientDataRaw := range clientDataList {
		clientDataJSON, ok := clientDataRaw.(string)
		if !ok {
			continue
		}

		// Parse JSON for each client
		var clientData struct {
			Address       string `json:"address"`
			Earnings      string `json:"earnings"`
			PubKey        string `json:"pubkey"`
			SolanaAddress string `json:"solanaAddress"`
		}
		if err := json.Unmarshal([]byte(clientDataJSON), &clientData); err != nil {
			logger.Printf("Error parsing client data: %v", err)
			continue
		}

		// If we expect parallel arrays for solana_address
		var solanaAddress string
		if len(solanaAddressList) > i {
			solanaAddress, _ = solanaAddressList[i].(string)
		}

		// If the chain is embedding the solana address in clientData
		if clientData.SolanaAddress != "" {
			solanaAddress = clientData.SolanaAddress
		}

		// Parse the earnings
		earningsValue := parseEarnings(clientData.Earnings)
		timestamp := time.Now().UTC()

		// Upsert logic
		tx := br.DB.Begin()
		var client models.Client

		result := tx.First(&client, "address = ?", clientData.Address)
		if result.Error != nil {
			// If record not found, create new
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				client = models.Client{
					Address:               clientData.Address,
					PubKey:                clientData.PubKey,
					SolanaAddress:         solanaAddress,
					TotalLifetimeEarnings: earningsValue,
				}
				if err := tx.Create(&client).Error; err != nil {
					tx.Rollback()
					logger.Printf("Error inserting client: %v", err)
					continue
				}
			} else {
				// Some DB error
				tx.Rollback()
				logger.Printf("Error querying client: %v", result.Error)
				continue
			}
		} else {
			// If found, update existing
			client.TotalLifetimeEarnings += earningsValue
			if solanaAddress != "" {
				client.SolanaAddress = solanaAddress
			}
			if err := tx.Save(&client).Error; err != nil {
				tx.Rollback()
				logger.Printf("Error updating client: %v", err)
				continue
			}
		}

		// Insert a new ClientEarning row
		clientEarning := models.ClientEarning{
			ClientAddress: clientData.SolanaAddress,
			Earnings:      earningsValue,
			Timestamp:     timestamp,
		}
		if err := tx.Create(&clientEarning).Error; err != nil {
			tx.Rollback()
			logger.Printf("Error inserting client earnings: %v", err)
			continue
		}

		tx.Commit()
		log.Printf("Stored client info: Address=%s, PubKey=%s, SolanaAddr=%s, Earned=%d\n",
			clientData.Address, clientData.PubKey, solanaAddress, earningsValue)
	}
}

// parseEarnings removes "usoar" suffix and converts to int64
func parseEarnings(earningsStr string) int64 {
	// If the chain is consistently "usoar", remove that suffix
	earningsStr = strings.TrimSuffix(earningsStr, "usoar")
	value, err := strconv.ParseInt(earningsStr, 10, 64)
	if err != nil {
		return 0
	}
	return value
}
