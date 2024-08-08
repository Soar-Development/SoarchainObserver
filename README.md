# SoarchainObserver

SoarchainObserver is an application designed to read and analyze all Soarchain related data. It connects to a Soarchain node via WebSocket, subscribes to new block events, and processes the received data.

## Features

- Connects to Soarchain node via WebSocket.
- Subscribes to new block events.
- Logs received block data for analysis.
- Stores and indexes all data using Soarchain device pubkeys.
- Stores and calculates total volume (24h).

## Getting Started

### Prerequisites

- Go (version 1.16 or later)
- A running Soarchain node with WebSocket enabled.

### Installation

1. Clone the repository:

    ```sh
    git clone https://github.com/Soar-Robotics/SoarchainObserver.git
    cd SoarchainObserver
    ```

2. Install dependencies:

    ```sh
    go get github.com/gorilla/websocket
    go get google.golang.org/protobuf
    ```

3. Build the application:

    ```sh
    go build -o soarchainobserver cmd/soarchainobserver/main.go
    ```

### Configuration

Configure the WebSocket and API endpoints in the `config.json` file:

```json
{
    "rpc_endpoint": "ws://34.165.238.45:26657/websocket",
    "api_endpoint": "http://34.165.238.45:1317/"
}
