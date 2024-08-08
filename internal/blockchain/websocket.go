package blockchain

import (
	"log"

	"github.com/gorilla/websocket"
)

type BlockReader struct {
	Conn *websocket.Conn
}

func NewBlockReader(url string) (*BlockReader, error) {
	log.Printf("Connecting to WebSocket URL: %s", url)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Printf("Failed to connect to WebSocket: %v", err)
		return nil, err
	}
	log.Println("Successfully connected to WebSocket")

	// Send subscription message
	subscribeMsg := `{
		"jsonrpc": "2.0",
		"method": "subscribe",
		"id": 0,
		"params": {
			"query": "tm.event='Tx'"
		}
	}`

	err = conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg))
	if err != nil {
		log.Printf("Failed to send subscription message: %v", err)
		conn.Close()
		return nil, err
	}
	log.Println("Subscription message sent")

	return &BlockReader{Conn: conn}, nil
}

func (br *BlockReader) ReadBlocks(logger *log.Logger) error {
	defer br.Conn.Close()
	for {
		_, message, err := br.Conn.ReadMessage()
		if err != nil {
			logger.Printf("Error reading message: %v", err)
			return err
		}
		logger.Printf("Received message: %s", message)
	}
}
