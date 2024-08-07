package eth

// CURRENTLY THIS FILE IS NOT BEING USED FOR ANYTHING.
import (
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rs/zerolog/log"
)

var (
	executionEndpoint = flag.String("execution-endpoint", "ws://localhost:8546", "Path to RPC endpoint for execution client.")
	dataFolder        = "data"
	txDataFile        = "tx_data.json"
	blockDataFile     = "block_data.json"
	txMetricsFile     = "tx_metrics.json"
	txInclusionFile   = "tx_inclusion.json"
)

type TxData struct {
	TxHash            common.Hash
	BlobGasFeeCapGwei float64
	BlobGas           uint64
	BlobCount         int
	GasFeeCapGwei     float64
	GasTipCapGwei     float64
	Gas               uint64
	Account           common.Address
}

type BlockData struct {
	BlockHash      common.Hash
	BlockNumber    uint64
	BlockTime      uint64
	BlobBaseFeeWei uint64
	BaseFeeGwei    float64
	Builder        string
}

type TxMetricsData struct {
	Account       common.Address
	BlobCount     int
	BlobGasFeeCap uint64
	TxTime        string
}

type TxInclusionData struct {
	Account        common.Address
	BlobCount      int
	InclusionDelay float64
	GasTipGwei     float64
}

func main() {
	flag.Parse()
	log.Info().Msgf("Using RPC endpoint of %s", *executionEndpoint)

	client, err := rpc.DialWebsocket(context.Background(), *executionEndpoint, "")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to dial websocket")
	}

	ec := ethclient.NewClient(client)
	gc := gethclient.New(client)

	txChan := make(chan *gethtypes.Transaction, 100)
	pSub, err := gc.SubscribeFullPendingTransactions(context.Background(), txChan)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to subscribe to full pending transactions")
	}

	hdrChan := make(chan *gethtypes.Header, 100)
	hSub, err := ec.SubscribeNewHead(context.Background(), hdrChan)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to subscribe to new head")
	}
	chainID, err := ec.ChainID(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to get chain ID")
	}

	currBaseFee := new(big.Int)
	pendingTxs := make(map[common.Hash]*gethtypes.Transaction)
	txTime := make(map[common.Hash]time.Time)

	txDataList := loadDataFromFile[TxData](filepath.Join(dataFolder, txDataFile))
	blockDataList := loadDataFromFile[BlockData](filepath.Join(dataFolder, blockDataFile))
	txMetricsList := loadDataFromFile[TxMetricsData](filepath.Join(dataFolder, txMetricsFile))
	txInclusionList := loadDataFromFile[TxInclusionData](filepath.Join(dataFolder, txInclusionFile))

	createDataFolder()

	for {
		select {
		case err := <-pSub.Err():
			log.Error().Err(err).Msg("Pending transaction subscription error")
			ec.Close()
			client.Close()
			close(txChan)
			close(hdrChan)
			hSub.Unsubscribe()
			saveDataToFile(filepath.Join(dataFolder, txDataFile), txDataList)
			saveDataToFile(filepath.Join(dataFolder, blockDataFile), blockDataList)
			saveDataToFile(filepath.Join(dataFolder, txMetricsFile), txMetricsList)
			saveDataToFile(filepath.Join(dataFolder, txInclusionFile), txInclusionList)
			return

		case err := <-hSub.Err():
			log.Error().Err(err).Msg("New head subscription error")
			ec.Close()
			client.Close()
			close(txChan)
			close(hdrChan)
			pSub.Unsubscribe()
			saveDataToFile(filepath.Join(dataFolder, txDataFile), txDataList)
			saveDataToFile(filepath.Join(dataFolder, blockDataFile), blockDataList)
			saveDataToFile(filepath.Join(dataFolder, txMetricsFile), txMetricsList)
			saveDataToFile(filepath.Join(dataFolder, txInclusionFile), txInclusionList)
			return

		case tx := <-txChan:
			if tx.Type() == gethtypes.BlobTxType {
				tHash := tx.Hash()
				txData := txData(tx, chainID)
				log.Info().Fields(txData).Msg("Received new Transaction from Gossip")
				txTime[tHash] = time.Now()
				txMetricsList = recordTxMetrics(txMetricsList, tx, chainID, txTime[tHash])
				pendingTxs[tHash] = tx
				txDataList = append(txDataList, txData)
				saveDataToFile(filepath.Join(dataFolder, txDataFile), txDataList)
				saveDataToFile(filepath.Join(dataFolder, txMetricsFile), txMetricsList)
			}

		case h := <-hdrChan:
			if h.ExcessBlobGas != nil {
				currBaseFee = eip4844.CalcBlobFee(*h.ExcessBlobGas)
			}
			blockData := BlockData{
				BlockHash:      h.Hash(),
				BlockNumber:    h.Number.Uint64(),
				BlockTime:      h.Time,
				BlobBaseFeeWei: currBaseFee.Uint64(),
				BaseFeeGwei:    float64(h.BaseFee.Uint64()) / params.GWei,
				Builder:        strings.ToValidUTF8(string(h.Extra), ""),
			}
			log.Info().Fields(blockData).Msg("Received new block")
			blockDataList = append(blockDataList, blockData)
			saveDataToFile(filepath.Join(dataFolder, blockDataFile), blockDataList)

			currentPendingTxs := len(pendingTxs)
			blobsIncluded := 0
			viabletxs := 0
			viableBlobs := 0

			for hash, tx := range pendingTxs {
				r, err := ec.TransactionReceipt(context.Background(), hash)
				if err == nil && r.BlockHash == h.Hash() {
					txData := txData(tx, chainID)
					log.Info().Fields(txData).Msgf("Transaction was included in block %d in %s", r.BlockNumber.Uint64(), time.Since(txTime[hash]))
					txInclusionList = recordTxInclusion(txInclusionList, tx, chainID, time.Since(txTime[hash]))
					blobsIncluded += len(tx.BlobHashes())
					delete(pendingTxs, hash)
					delete(txTime, hash)
					saveDataToFile(filepath.Join(dataFolder, txInclusionFile), txInclusionList)
					continue
				}
				acc, err := gethtypes.Sender(gethtypes.NewCancunSigner(chainID), tx)
				if err != nil {
					log.Error().Err(err).Msg("Could not get sender's account address")
					continue
				}

				currNonce, err := ec.NonceAtHash(context.Background(), acc, h.Hash())
				if err != nil {
					log.Error().Err(err).Msg("Could not get sender's account nonce")
					continue
				}
				if tx.Nonce() < currNonce {
					txData := txData(tx, chainID)
					log.Info().Fields(txData).Msgf("Transaction has been successfully replaced and included on chain in %s", time.Since(txTime[hash]))
					delete(pendingTxs, hash)
					delete(txTime, hash)
					continue
				}
				if tx.Nonce() != currNonce {
					continue
				}
				if tx.BlobGasFeeCap().Cmp(currBaseFee) >= 0 {
					viabletxs++
					viableBlobs += len(tx.BlobHashes())
					txData := txData(tx, chainID)
					log.Info().Fields(txData).Msgf("Transaction was still not included after %s", time.Since(txTime[hash]))
				}
			}

			log.Info().Msgf("Pending Transactions: %d", len(pendingTxs))
			log.Info().Msgf("Viable Transactions: %d", viabletxs)
			log.Info().Msgf("Viable Blobs: %d", viableBlobs)
			log.Info().Msgf("Transaction Inclusions: %d", currentPendingTxs-len(pendingTxs))
			log.Info().Msgf("Tx Blob Inclusions by Builder %s: %d", strings.ToValidUTF8(string(h.Extra), ""), blobsIncluded)
			log.Info().Msgf("Blocks by Builder %s: %d", strings.ToValidUTF8(string(h.Extra), ""), 1)

			log.Info().Fields(map[string]interface{}{
				"previousPendingTxs": currentPendingTxs,
				"currentPendingTxs":  len(pendingTxs),
				"viableTxs":          viabletxs,
			}).Msg("Post block Summary for blob transactions")
		}
	}
}

func createDataFolder() {
	if _, err := os.Stat(dataFolder); os.IsNotExist(err) {
		err = os.Mkdir(dataFolder, 0755)
		if err != nil {
			log.Fatal().Err(err).Msgf("Could not create data folder %s", dataFolder)
		}
	}
}

func txData(tx *gethtypes.Transaction, chainID *big.Int) TxData {
	acc, err := gethtypes.Sender(gethtypes.NewCancunSigner(chainID), tx)
	if err != nil {
		log.Error().Err(err).Msg("Could not get sender's account address")
		return TxData{}
	}

	return TxData{
		TxHash:            tx.Hash(),
		BlobGasFeeCapGwei: float64(tx.BlobGasFeeCap().Uint64()) / params.GWei,
		BlobGas:           tx.BlobGas(),
		BlobCount:         len(tx.BlobHashes()),
		GasFeeCapGwei:     float64(tx.GasFeeCap().Uint64()) / params.GWei,
		GasTipCapGwei:     float64(tx.GasTipCap().Uint64()) / params.GWei,
		Gas:               tx.Gas(),
		Account:           acc,
	}
}

func recordTxMetrics(txMetricsList []TxMetricsData, tx *gethtypes.Transaction, chainID *big.Int, txTime time.Time) []TxMetricsData {
	acc, err := gethtypes.Sender(gethtypes.NewCancunSigner(chainID), tx)
	if err != nil {
		log.Error().Err(err).Msg("Could not get sender's account address")
		return txMetricsList
	}
	data := TxMetricsData{
		Account:       acc,
		BlobCount:     len(tx.BlobHashes()),
		BlobGasFeeCap: tx.BlobGasFeeCap().Uint64(),
		TxTime:        txTime.String(),
	}
	log.Info().Msgf("Observed Transaction: Account=%s, BlobCount=%d, BlobGasFeeCap=%d, TxTime=%s", data.Account, data.BlobCount, data.BlobGasFeeCap, data.TxTime)
	return append(txMetricsList, data)
}

func recordTxInclusion(txInclusionList []TxInclusionData, tx *gethtypes.Transaction, chainID *big.Int, inclusionDelay time.Duration) []TxInclusionData {
	acc, err := gethtypes.Sender(gethtypes.NewCancunSigner(chainID), tx)
	if err != nil {
		log.Error().Err(err).Msg("Could not get sender's account address")
		return txInclusionList
	}

	gasTip, _ := tx.GasTipCap().Float64()
	gasTipGwei := gasTip / params.GWei
	data := TxInclusionData{
		Account:        acc,
		BlobCount:      len(tx.BlobHashes()),
		InclusionDelay: inclusionDelay.Seconds(),
		GasTipGwei:     gasTipGwei,
	}
	log.Info().Msgf("Transaction Inclusion: Account=%s, BlobCount=%d, InclusionDelay=%fs, GasTip(Gwei)=%f", data.Account, data.BlobCount, data.InclusionDelay, data.GasTipGwei)
	return append(txInclusionList, data)
}

func saveDataToFile(filename string, data interface{}) {
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal().Err(err).Msgf("Could not create file %s", filename)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		log.Fatal().Err(err).Msgf("Could not encode data to file %s", filename)
	}
}

func loadDataFromFile[T any](filename string) []T {
	var data []T
	if _, err := os.Stat(filename); err == nil {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatal().Err(err).Msgf("Could not open file %s", filename)
		}
		defer file.Close()

		byteValue, err := ioutil.ReadAll(file)
		if err != nil {
			log.Fatal().Err(err).Msgf("Could not read file %s", filename)
		}

		if err := json.Unmarshal(byteValue, &data); err != nil {
			log.Fatal().Err(err).Msgf("Could not unmarshal data from file %s", filename)
		}
	}
	return data
}
