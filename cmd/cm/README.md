## cm

1. **CLI Arguments**:
   - `--eth-url`: Ethereum node URL
   - `--contract`: Contract address
   - `--private-key`: CM private key in hex format
   - `--port`: TCP listening port

2. **TCP Server**:
   - Handles connections concurrently
   - Reads exactly 52 bytes (416 bits) per connection
   - First 20 bytes for Ethereum address
   - Last 32 bytes for RCD ID
   - Sends JSON response with transaction details

3. **Error Handling**:
   - Graceful shutdown with SIGINT/SIGTERM
   - Connection timeouts (30s for read, 10s for write)
   - Detailed error messages in JSON responses
   - Logs to stdout with timestamps

4. **Transaction Handling**:
   - Waits for transaction to be mined
   - Returns gas usage and block number
   - Context timeout of 2 minutes for transaction mining

To use the service:

```bash
./bin/cm --eth-url http://localhost:8545 --contract 0x5FbDB2315678afecb367f032d93F642f64180aa3 --private-key ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --port 42424

./bin/cmtest --port 42424 --concurrent -n 10 -v
```
