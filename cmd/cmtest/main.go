package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func main() {
	port := flag.Int("port", 0, "Port to connect to")
	count := flag.Int("n", 1, "Number of test iterations")
	delay := flag.Duration("delay", 100*time.Millisecond, "Delay between requests")
	concurrent := flag.Bool("concurrent", false, "Run requests concurrently")
	verbose := flag.Bool("v", false, "Print detailed responses")
	flag.Parse()

	if *port == 0 {
		flag.Usage()
		os.Exit(1)
	}

	addr := fmt.Sprintf("localhost:%d", *port)
	if *concurrent {
		done := make(chan bool)
		for i := range *count {
			go func(i int) {
				sendRequest(addr, i, *verbose)
				done <- true
			}(i)
			time.Sleep(*delay)
		}
		for range *count {
			<-done
		}
	} else {
		for i := range *count {
			sendRequest(addr, i, *verbose)
			time.Sleep(*delay)
		}
	}
}

func sendRequest(addr string, id int, verbose bool) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Printf("[%d] Failed to connect: %v", id, err)
		return
	}
	defer conn.Close()

	var data [52]byte
	randBytes := make([]byte, 20)
	if _, err := rand.Read(randBytes); err != nil {
		log.Printf("[%d] Failed to generate random address: %v", id, err)
		return
	}
	copy(data[:20], randBytes)

	rcdID, err := rand.Int(rand.Reader, new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil))
	if err != nil {
		log.Printf("[%d] Failed to generate RCD ID: %v", id, err)
		return
	}
	rcdBytes := rcdID.Bytes()
	copy(data[20:], rcdBytes)

	addr = common.BytesToAddress(data[:20]).Hex()
	log.Printf("[%d] Sending request with address %s and RCD ID %s", id, addr, rcdID.String())

	if _, err := conn.Write(data[:]); err != nil {
		log.Printf("[%d] Failed to send data: %v", id, err)
		return
	}

	var resp map[string]any
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		log.Printf("[%d] Failed to read response: %v", id, err)
		return
	}

	if verbose {
		log.Printf("[%d] Response: %+v", id, resp)
	} else if resp["status"] != "success" {
		log.Printf("[%d] Error: %v", id, resp["error"])
	}
}
