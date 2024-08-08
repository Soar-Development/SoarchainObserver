package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/Soar-Robotics/SoarchainObserver/internal/blockchain"
	"github.com/Soar-Robotics/SoarchainObserver/internal/config"
	"github.com/Soar-Robotics/SoarchainObserver/internal/utils"
)

func main() {
	logger := utils.GetLogger()

	config, err := config.LoadConfig("config.json")
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	blockReader, err := blockchain.NewBlockReader(config.RPCEndpoint)
	if err != nil {
		logger.Fatalf("Failed to connect to WebSocket: %v", err)
	}

	// Channel to listen for termination signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Run BlockReader in a separate goroutine
	go func() {
		logger.Println("Connected to WebSocket, starting to read blocks...")
		if err := blockReader.ReadBlocks(logger); err != nil {
			logger.Fatalf("Error reading blocks: %v", err)
		}
	}()

	// Block until a signal is received
	<-stop

	// Handle cleanup here if needed
	logger.Println("Shutting down...")
}
