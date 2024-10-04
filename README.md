
# SoarChain Observer

SoarChain Observer is a Go application designed to monitor the SoarChain blockchain for runner_challenge transactions, store client earnings data in a PostgreSQL database, and provide an API for querying client earnings over specified time periods.

## Table of Contents

- Features
- Prerequisites
- Installation
- Configuration
- Running the Application
- API Usage
    - Endpoints
    - Request Parameters
    - Response Format
    - Examples
- Database Schema
- Development
    - Project Structure
    - Testing
- Deployment
- License
- Contact

## Features

- WebSocket Observer: Connects to the SoarChain blockchain node via WebSocket to listen for runner_challenge events.
- Data Storage: Parses incoming transactions and stores client earnings data in a PostgreSQL database.
- API Server: Provides a RESTful API to query client earnings, both lifetime and over specified time periods.
- Error Handling & Reconnection Logic: Robust handling of WebSocket disconnections with automatic reconnection attempts.
- Concurrent Processing: Efficient handling of high transaction volumes using Go's concurrency features.

## Prerequisites

- **Go**: Version 1.15 or higher.
- **PostgreSQL**: Version 12 or higher.
- **Git**: For cloning the repository.
- **SoarChain Node**: Access to a running SoarChain blockchain node with WebSocket support.

## Installation

### 1. Clone the Repository

```bash
git clone https://github.com/Soar-Robotics/SoarchainObserver.git
cd SoarchainObserver
```

### 2. Install Dependencies

Ensure you have Go modules enabled. Run:

```bash
go mod download
```

### 3. Build the Application

```bash
go build -o soarchainobserver cmd/observer/main.go
```

## Configuration

### 1. Database Setup

Set up a PostgreSQL database and user:

```sql
-- Connect to PostgreSQL as the postgres user
sudo -u postgres psql

-- Create a database user
CREATE USER soaruser WITH PASSWORD 'yourpassword';

-- Create a database
CREATE DATABASE soarchain_db;

-- Grant privileges
GRANT ALL PRIVILEGES ON DATABASE soarchain_db TO soaruser;

-- Exit psql
\q
```

### 2. Configuration File

Create a `config.json` file in the root directory:

```json
{
  "rpc_endpoint": "ws://localhost:26657/websocket",
  "api_endpoint": "http://localhost:8080"
}
```

### 3. Environment Variables

Create a `.env` file in the root directory:

```env
DB_HOST=localhost
DB_PORT=5432
DB_USER=soaruser
DB_PASSWORD=yourpassword
DB_NAME=soarchain_db
```

## Running the Application

### 1. Load Environment Variables

Ensure your environment variables are loaded. If you're running the application directly, use the godotenv package or export them in your shell.

### 2. Run the Application

```bash
go run cmd/observer/main.go
```

Alternatively, if you've built the executable:

```bash
./soarchainobserver
```

## API Usage

The application exposes a RESTful API to query client earnings data.

### Base URL

```
http://localhost:8080
```

### Endpoints

#### GET /client/:address

Retrieve earnings data for a specific client.

- **URL Parameters:**
    - `:address` (string) - Client's address.

- **Query Parameters:**
    - `period` (string, optional) - Time period for earnings calculation. Defaults to `1h` (last one hour). Accepts durations like `1h`, `24h`, `7d`.

### Request Parameters

- **period (optional):**
    Specifies the time duration over which to calculate earnings.
    Format: A number followed by a time unit (e.g., 1h for one hour, 24h for twenty-four hours, 7d for seven days).
    Supported units:
    - ns - Nanoseconds
    - us - Microseconds
    - ms - Milliseconds
    - s - Seconds
    - m - Minutes
    - h - Hours
    - d - Days (custom unit, see note below)

Note: The standard Go `time.ParseDuration` does not support days (`d`), so if you wish to use days, you need to handle this conversion manually in the code.

### Response Format

- **Status Codes:**
    - `200 OK` - Successful response.
    - `400 Bad Request` - Invalid request parameters.
    - `404 Not Found` - Client not found.
    - `500 Internal Server Error` - Server error.

- **Response Body (JSON):**

```json
{
  "address": "soar1...",
  "pubkey": "026c28e2efdf...",
  "total_lifetime_earnings": 123456789,
  "earnings_over_period": 100000,
  "period": "1h"
}
```

### Examples

#### Retrieve Client Earnings Over the Last Hour

**Request:**

```bash
GET http://localhost:8080/client/soar1exampleaddress?period=1h
```

**Response:**

```json
{
  "address": "soar1exampleaddress",
  "pubkey": "026c28e2efdf...",
  "total_lifetime_earnings": 5000000,
  "earnings_over_period": 150000,
  "period": "1h"
}
```

#### Retrieve Client Earnings Over the Last 24 Hours

**Request:**

```bash
GET http://localhost:8080/client/soar1exampleaddress?period=24h
```

**Response:**

```json
{
  "address": "soar1exampleaddress",
  "pubkey": "026c28e2efdf...",
  "total_lifetime_earnings": 5000000,
  "earnings_over_period": 1200000,
  "period": "24h"
}
```

#### Retrieve Client Earnings Without Specifying Period

Defaults to the last 1 hour.

**Request:**

```bash
GET http://localhost:8080/client/soar1exampleaddress
```

## Database Schema

### Table: `clients`

Stores client information and total lifetime earnings.

- **Columns:**
    - `address` (TEXT, PRIMARY KEY)
    - `pub_key` (TEXT)
    - `total_lifetime_earnings` (BIGINT)

### Table: `client_earnings`

Stores individual earnings transactions with timestamps.

- **Columns:**
    - `id` (SERIAL PRIMARY KEY)
    - `client_address` (TEXT, FOREIGN KEY to clients.address)
    - `earnings` (BIGINT)
    - `timestamp` (TIMESTAMP WITH TIME ZONE)

## Development

### Project Structure

```
SoarchainObserver/
├── cmd/
│   └── observer/
│       └── main.go
├── internal/
│   ├── blockchain/
│   │   └── websocket.go
│   ├── config/
│   │   └── config.go
│   ├── models/
│   │   ├── client.go
│   │   └── client_earning.go
│   └── utils/
│       └── logger.go
├── config.json
├── .env
├── go.mod
├── go.sum
└── README.md
```

### Testing

- **Unit Tests:** Implement unit tests for critical functions.
- **Integration Tests:** Test the observer and API server with a running SoarChain node and PostgreSQL database.
- **API Testing:** Use tools like Postman or curl to test API endpoints.

## Deployment

### Running as a Systemd Service

Create a service file at `/etc/systemd/system/soarchainobserver.service`:

```ini
[Unit]
Description=SoarChain Observer Service
After=network.target

[Service]
User=www-data
WorkingDirectory=/path/to/SoarchainObserver
EnvironmentFile=/path/to/SoarchainObserver/.env
ExecStart=/path/to/SoarchainObserver/soarchainobserver
Restart=always

[Install]
WantedBy=multi-user.target
```

Commands:

```bash
sudo systemctl daemon-reload
sudo systemctl start soarchainobserver
sudo systemctl enable soarchainobserver
```

### Reverse Proxy with Nginx

Set up Nginx to proxy requests to your API server and handle SSL termination.

- **Install Nginx:**

```bash
sudo apt install nginx
```

- **Configure Nginx:**

```nginx
server {
    listen 80;
    server_name observer.soarchain.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
    }
}
```

- **Obtain SSL Certificates:**

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d observer.soarchain.com
```

## License

This project is licensed under the MIT License.

## Contact

For questions or support, please contact:

- **Name:** Soar Robotics Team
- **Email:** alperen@soarrobotics.com
- **Website:** https://soarrobotics.com
