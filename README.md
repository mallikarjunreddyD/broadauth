# broadauth

A playground for broadcast authentication algorithms.

## Instructions

```
go get github.com/ethereum/go-ethereum
go install github.com/ethereum/go-ethereum/cmd/abigen@latest

# Build the smart contract along with ABI and bin information
cd contracts
forge build --extra-output-files={abi,bin}
cd ..

# Generate Go bindings for the smart contract
abigen --abi=contracts/out/InfTESLAPlusPlus.sol/InfTESLAplusplus.abi.json --bin=contracts/out/InfTESLAPlusPlus.sol/InfTESLAplusplus.bin --pkg=contract --out=internal/contract/contract.go
```
