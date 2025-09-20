# broadauth

A playground for broadcast authentication algorithms.

## Instructions

* Install the dependencies

```
go get github.com/ethereum/go-ethereum
go install github.com/ethereum/go-ethereum/cmd/abigen@latest
```

* Build the smart contract along with ABI and bin information

```
cd contracts
forge build --extra-output-files={abi,bin}
cd ..
```

* Generate Go bindings for the smart contract

```
abigen --abi=contracts/out/InfTESLAPlusPlus.sol/InfTESLAplusplus.abi.json --bin=contracts/out/InfTESLAPlusPlus.sol/InfTESLAplusplus.bin --pkg=contract --type=Contract --out=internal/contract/contract.go

abigen --abi contracts/out/Counter.sol/Counter.abi.json --bin contracts/out/Counter.sol/Counter.bin --pkg contract --type Counter --out internal/contract/contract.go
```

### Run the following blocks each in a separate terminal instance

1. Run `anvil` with 12 seconds interval mining

```
anvil --block-time 12 --host 0.0.0.0 --port 8545
```

2. Deploy the smart contract on the local net

```
cd contracts
forge create src/InfTESLAPlusPlus.sol:InfTESLAplusplus --rpc-url 0.0.0.0:8545 --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --broadcast
```

3. Run the consortium manager on port `10101`. Private key must be the one used to deploy the contract.

```
./bin/cm \
    -eth-url http://0.0.0.0:8545 \
    -contract 0x5FbDB2315678afecb367f032d93F642f64180aa3 \
    -private-key ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
    -port 10101
```

4. Run an owner on port `10102`

```
./bin/owner \
    -eth-url http://0.0.0.0:8545 \
    -contract 0x5FbDB2315678afecb367f032d93F642f64180aa3 \
    -private-key 59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d \
    -cm-addr 0.0.0.0:10101 \
    -disclosure-delay 2 \
    -hashchain-len 64 \
    -port 10102 \
```

5. Run an RCD

```
./bin/rcd \
    -contract 0x5FbDB2315678afecb367f032d93F642f64180aa3 \
    -disclosure-delay 2 \
    -eth-url http://0.0.0.0:8545 \
    -hashchain-len 64 \
    -owner-addr 0.0.0.0:10102 \
    -uuid 8bfe24ae-d641-4522-ba83-3eab387a8fb3
```
