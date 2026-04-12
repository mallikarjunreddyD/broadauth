# broadauth

A playground for broadcast authentication algorithms.

## Instructions

* Install the dependencies

```bash
go get github.com/ethereum/go-ethereum
go install github.com/ethereum/go-ethereum/cmd/abigen@latest
```

* Build the smart contract along with ABI and bin information

```bash
cd contracts
forge build --extra-output-files={abi,bin}
cd ..
```

* Generate Go bindings for the smart contract

```bash
abigen --abi=contracts/out/InfTESLAPlusPlus.sol/InfTESLAplusplus.abi.json --bin=contracts/out/InfTESLAPlusPlus.sol/InfTESLAplusplus.bin --pkg=contract --type=Contract --out=internal/contract/contract.go

abigen --abi contracts/out/Counter.sol/Counter.abi.json --bin contracts/out/Counter.sol/Counter.bin --pkg contract --type Counter --out internal/contract/contract.go
```

* Compile all the binaries

```bash
make
```

### Run the following blocks each in a separate terminal instance

1. Run `anvil` with 12 seconds interval mining

```bash
anvil --block-time 12 --host 0.0.0.0 --port 8545
```

2. Deploy the smart contract on the local net

```bash
cd contracts
forge create src/InfTESLAPlusPlus.sol:InfTESLAplusplus --rpc-url 0.0.0.0:8545 --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --broadcast
cd ..
```

3. Run the consortium manager on port `10101`. Private key must be the one used to deploy the contract.

```bash
./bin/cm \
    -eth-url http://0.0.0.0:8545 \
    -contract 0x5FbDB2315678afecb367f032d93F642f64180aa3 \
    -private-key ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
    -port 10101
```

4. Run an owner on port 10102.
_(Note: You can change the `-mode` flag to `deterministic`, `probabilistic`, `adaptive`, or `probadaptive`. When using adaptive modes, specify `-t-min` and `-t-max`.)_

```bash
./bin/owner \
    -eth-url http://0.0.0.0:8545 \
    -contract 0x5FbDB2315678afecb367f032d93F642f64180aa3 \
    -private-key 59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d \
    -cm-addr 0.0.0.0:10101 \
    -disclosure-delay 2 \
    -hashchain-len 64 \
    -port 10102 \
    -mode adaptive \
    -t-min 1000 \
    -t-max 8000
```

5. Run an RCD _(Change the uuid to use from the ones printed by the owner)_

```bash
./bin/rcd \
    -contract 0x5FbDB2315678afecb367f032d93F642f64180aa3 \
    -disclosure-delay 2 \
    -eth-url http://0.0.0.0:8545 \
    -hashchain-len 64 \
    -owner-addr 0.0.0.0:10102 \
    -uuid 8bfe24ae-d641-4522-ba83-3eab387a8fb3 \
    -mode adaptive \
    -t-min 1000 \
    -t-max 8000
```

## Automated Benchmarking & Chart Generation

The project includes an automated Python suite to run all four protocol modes back-to-back, parse the output logs, and generate comparative Matplotlib charts.

1. Install Python Dependencies

Ensure you have `matplotlib` and `numpy` installed. If you are managing Python packages natively, you can grab them from the repos:

```bash
python -m venv venv
source venv/bin/activate
pip install matplotlib numpy
```

2. Start the Backend Infrastructure

Ensure `anvil` (Step 1) and the `cm` (Consortium Manager) (Step 3) are running in the background.

3. Run the Benchmark Suite

The script will automatically spawn the `owner` daemon, capture the UUIDs, and orchestrate the `rcd` nodes across all 4 modes.

```bash
python benchmarking/benchmark.py
```

_This will take several minutes to run through the iterations. It will output an aggregated `avg_benchmarks.md` report upon completion._

4. Generate Plots

Parse the generated markdown data to create visual comparisons of throughput, latency, bandwidth, and memory usage.

```bash
python benchmarking/generate_charts.py
```

Check the `/plots` directory for the resulting `.png` files!