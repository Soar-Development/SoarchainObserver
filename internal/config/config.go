package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	RPCEndpoint string `json:"rpc_endpoint"`
	APIEndpoint string `json:"api_endpoint"`
}

func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
