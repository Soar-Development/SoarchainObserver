package blockchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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

// EpochInfo holds relevant fields from the Soarchain epoch response.
type EpochInfo struct {
	Identifier        string
	Duration          time.Duration
	CurrentEpoch      int64
	CurrentEpochStart time.Time
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

// getCurrentEpoch fetches and parses the current epoch info from Soarchain
func getCurrentEpoch() (EpochInfo, error) {
	var epochInfo EpochInfo

	resp, err := http.Get("https://api.mainnet.soarchain.com/soarchain/epoch/day")
	if err != nil {
		return epochInfo, fmt.Errorf("failed to fetch epoch info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return epochInfo, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Expected structure (an example):
	// {
	//   "epoch": {
	//     "identifier": "day",
	//     "start_time": "2024-12-05T09:04:54.532413149Z",
	//     "duration": "86400s",
	//     "current_epoch": "33",
	//     "current_epoch_start_time": "2025-01-16T09:04:54.532413149Z",
	//     ...
	//   }
	// }

	var raw struct {
		Epoch struct {
			Identifier            string `json:"identifier"`
			StartTime             string `json:"start_time"`
			Duration              string `json:"duration"`
			CurrentEpoch          string `json:"current_epoch"`
			CurrentEpochStartTime string `json:"current_epoch_start_time"`
		} `json:"epoch"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return epochInfo, fmt.Errorf("failed to parse epoch JSON: %w", err)
	}

	// Convert current_epoch to int64
	epochNum, err := strconv.ParseInt(raw.Epoch.CurrentEpoch, 10, 64)
	if err != nil {
		return epochInfo, fmt.Errorf("failed to parse current_epoch: %w", err)
	}

	// Parse current_epoch_start_time
	epochStart, err := time.Parse(time.RFC3339Nano, raw.Epoch.CurrentEpochStartTime)
	if err != nil {
		return epochInfo, fmt.Errorf("failed to parse current_epoch_start_time: %w", err)
	}

	// Convert duration string (e.g. "86400s") to time.Duration
	durStr := raw.Epoch.Duration
	if !strings.HasSuffix(durStr, "s") {
		return epochInfo, fmt.Errorf("epoch duration does not end with 's': %s", durStr)
	}
	durVal, err := time.ParseDuration(durStr)
	if err != nil {
		return epochInfo, fmt.Errorf("failed to parse epoch duration: %w", err)
	}

	epochInfo.Identifier = raw.Epoch.Identifier
	epochInfo.Duration = durVal
	epochInfo.CurrentEpoch = epochNum
	epochInfo.CurrentEpochStart = epochStart

	return epochInfo, nil
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

	// Fetch the current epoch from Soarchain (only once per message).
	// You could also fetch it once per block or on a timer, depending on performance needs.
	epochInfo, err := getCurrentEpoch()
	if err != nil {
		logger.Printf("Error fetching epoch info: %v", err)
		// Optional: continue or skip
		return
	}

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
					LastChallengeTime:     timestamp,
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
			client.LastChallengeTime = timestamp
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

		// ------------------------------------------------------------------------
		// Upsert into epoch_earnings
		// ------------------------------------------------------------------------
		if err := upsertEpochEarnings(tx, clientData.SolanaAddress, earningsValue, epochInfo); err != nil {
			tx.Rollback()
			logger.Printf("Error upserting epoch earnings: %v", err)
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

// upsertEpochEarnings aggregates into a new or existing epoch record
func upsertEpochEarnings(
	tx *gorm.DB,
	clientAddress string,
	earningsValue int64,
	epochInfo EpochInfo,
) error {

	epochNumber := epochInfo.CurrentEpoch
	startTime := epochInfo.CurrentEpochStart
	endTime := startTime.Add(epochInfo.Duration) // e.g., start + 86400s

	var epochRecord models.EpochEarnings
	err := tx.Where("client_address = ? AND epoch_number = ?", clientAddress, epochNumber).
		First(&epochRecord).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Insert new epoch record
			epochRecord = models.EpochEarnings{
				ClientAddress: clientAddress,
				EpochNumber:   epochNumber,
				StartTime:     startTime,
				EndTime:       endTime,
				TotalEarnings: earningsValue,
				CreatedAt:     time.Now().UTC(),
				UpdatedAt:     time.Now().UTC(),
			}
			if err := tx.Create(&epochRecord).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		// Record found, update
		epochRecord.TotalEarnings += earningsValue
		epochRecord.UpdatedAt = time.Now().UTC()
		if err := tx.Save(&epochRecord).Error; err != nil {
			return err
		}
	}

	return nil
}
