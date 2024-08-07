package mevcommit

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// contract addresses
const bidderRegistryAddress = "0x7ffa86fF89489Bca72Fec2a978e33f9870B2Bd25"
const blockTrackerAddress = "0x2eEbF31f5c932D51556E70235FB98bB2237d065c"
const preConfCommitmentStoreAddress = "0xCAC68D97a56b19204Dd3dbDC103CB24D47A825A3"

// CommitmentStoredEvent represents the data structure for the CommitmentStored event
type CommitmentStoredEvent struct {
	CommitmentIndex     [32]byte
	Bidder              common.Address
	Commiter            common.Address
	Bid                 uint64
	BlockNumber         uint64
	BidHash             [32]byte
	DecayStartTimeStamp uint64
	DecayEndTimeStamp   uint64
	TxnHash             string
	CommitmentHash      [32]byte
	BidSignature        []byte
	CommitmentSignature []byte
	DispatchTimestamp   uint64
	SharedSecretKey     []byte
}

// LoadABI loads the ABI from the specified file path and parses it
func LoadABI(filePath string) (abi.ABI, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Println("Failed to load ABI file:", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(data)))
	if err != nil {
		log.Println("Failed to load ABI file:", err)
	}

	return parsedABI, nil
}

// get latest window height
func WindowHeight(client *ethclient.Client) (*big.Int, error) {
	// Load blockTracker contract
	blockTrackerABI, err := LoadABI("abi/BlockTracker.abi")
	if err != nil {
		log.Println("Failed to load ABI file:", err)
	}

	blockTrackerContract := bind.NewBoundContract(common.HexToAddress(blockTrackerAddress), blockTrackerABI, client, client, client)

	// Get current bidding window
	var currentWindowResult []interface{}
	err = blockTrackerContract.Call(nil, &currentWindowResult, "getCurrentWindow")
	if err != nil {
		log.Println(err)
	}

	// Extract the current window as *big.Int
	currentWindow, ok := currentWindowResult[0].(*big.Int)
	if !ok {
		log.Println("Could not get current window", err)
	}

	return currentWindow, nil
}

func GetMinDeposit(client *ethclient.Client) (*big.Int, error) {
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

	// Call the minDeposit function
	var minDepositResult []interface{}
	err = bidderRegistryContract.Call(nil, &minDepositResult, "minDeposit")
	if err != nil {
		return nil, fmt.Errorf("failed to call minDeposit function: %v", err)
	}

	// Extract the minDeposit as *big.Int
	minDeposit, ok := minDepositResult[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("failed to convert minDeposit to *big.Int")
	}

	return minDeposit, nil
}

// Deposit minimum bid amount into the bidding window. Returns a geth Transaction type if successful.
func DepositIntoWindow(client *ethclient.Client, depositWindow *big.Int, authAcct *AuthAcct) (*types.Transaction, error) {
	// Load bidderRegistry contract
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

	minDeposit, err := GetMinDeposit(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get minDeposit: %v", err)
	}

	// Set the value to minDeposit
	authAcct.Auth.Value = minDeposit

	// Prepare the transaction
	tx, err := bidderRegistryContract.Transact(authAcct.Auth, "depositForSpecificWindow", depositWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %v", err)
	}

	// Wait for the transaction to be mined (optional)
	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		return nil, fmt.Errorf("transaction mining error: %v", err)
	}

	if receipt.Status == 1 {
		fmt.Println("Transaction successful")
		return tx, nil
	} else {
		return nil, fmt.Errorf("transaction failed")
	}
}

// GetDepositAmount retrieves the deposit amount for a given address and window
func GetDepositAmount(client *ethclient.Client, address common.Address, window big.Int) (*big.Int, error) {
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

	// Call the getDeposit function
	var depositResult []interface{}
	err = bidderRegistryContract.Call(nil, &depositResult, "minDeposit")
	if err != nil {
		return nil, fmt.Errorf("failed to call getDeposit function: %v", err)
	}

	// Extract the deposit amount as *big.Int
	depositAmount, ok := depositResult[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("failed to convert deposit amount to *big.Int")
	}

	return depositAmount, nil
}

// WithdrawFromWindow withdraws all funds from the specified window
func WithdrawFromWindow(client *ethclient.Client, authAcct *AuthAcct, window *big.Int) (*types.Transaction, error) {
	// Load bidderRegistry contract
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

	// Prepare the withdrawal transaction
	withdrawalTx, err := bidderRegistryContract.Transact(authAcct.Auth, "withdrawBidderAmountFromWindow", authAcct.Address, window)
	if err != nil {
		return nil, fmt.Errorf("failed to create withdrawal transaction: %v", err)
	}

	// Wait for the withdrawal transaction to be mined
	withdrawalReceipt, err := bind.WaitMined(context.Background(), client, withdrawalTx)
	if err != nil {
		return nil, fmt.Errorf("withdrawal transaction mining error: %v", err)
	}

	if withdrawalReceipt.Status == 1 {
		fmt.Println("Withdrawal successful")
		return withdrawalTx, nil
	} else {
		return nil, fmt.Errorf("withdrawal failed")
	}
}

// Event listener function. 
// TODO - currently not listening correctly.
func ListenForCommitmentStoredEvent(client *ethclient.Client) {
	contractAbi, err := LoadABI("abi/PreConfCommitmentStore.abi") // Update with the correct path to your ABI file
	if err != nil {
		log.Fatalf("Failed to load contract ABI: %v", err)
	}

	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(context.Background(), headers)
	if err != nil {
		log.Fatalf("Failed to subscribe to new head: %v", err)
	}

	for {
		select {
		case err := <-sub.Err():
			log.Fatalf("Error with header subscription: %v", err)
		case header := <-headers:
			query := ethereum.FilterQuery{
				Addresses: []common.Address{common.HexToAddress(preConfCommitmentStoreAddress)},
				FromBlock: header.Number,
				ToBlock:   header.Number,
			}

			logs := make(chan types.Log)
			subLogs, err := client.SubscribeFilterLogs(context.Background(), query, logs)
			if err != nil {
				log.Printf("Failed to subscribe to logs: %v", err)
				continue
			}

			for {
				select {
				case err := <-subLogs.Err():
					log.Printf("Error with log subscription: %v", err)
					break
				case vLog := <-logs:
					var event CommitmentStoredEvent

					err := contractAbi.UnpackIntoInterface(&event, "CommitmentStored", vLog.Data)
					if err != nil {
						log.Printf("Failed to unpack log data: %v", err)
						continue
					}

					fmt.Printf("CommitmentStored Event: \n")
					fmt.Printf("CommitmentIndex: %x\n", event.CommitmentIndex)
					fmt.Printf("Bidder: %s\n", event.Bidder.Hex())
					fmt.Printf("Commiter: %s\n", event.Commiter.Hex())
					fmt.Printf("Bid: %d\n", event.Bid)
					fmt.Printf("BlockNumber: %d\n", event.BlockNumber)
					fmt.Printf("BidHash: %x\n", event.BidHash)
					fmt.Printf("DecayStartTimeStamp: %d\n", event.DecayStartTimeStamp)
					fmt.Printf("DecayEndTimeStamp: %d\n", event.DecayEndTimeStamp)
					fmt.Printf("TxnHash: %s\n", event.TxnHash)
					fmt.Printf("CommitmentHash: %x\n", event.CommitmentHash)
					fmt.Printf("BidSignature: %x\n", event.BidSignature)
					fmt.Printf("CommitmentSignature: %x\n", event.CommitmentSignature)
					fmt.Printf("DispatchTimestamp: %d\n", event.DispatchTimestamp)
					fmt.Printf("SharedSecretKey: %x\n", event.SharedSecretKey)
				}
			}
		}
	}
}
