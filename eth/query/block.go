// +build evm

package query

import (
	"bytes"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/loomnetwork/go-loom/plugin/types"
	"github.com/loomnetwork/loomchain"
	"github.com/loomnetwork/loomchain/eth/utils"
	"github.com/loomnetwork/loomchain/store"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	"github.com/tendermint/tendermint/rpc/lib/client"
)

var (
	searchBlockSize = uint64(100)
	RpcPort         = 46657
	RpcHost         = fmt.Sprintf("tcp://0.0.0.0:%d", RpcPort) //"tcp://0.0.0.0:46657"
)

func GetBlockByNumber(state loomchain.ReadOnlyState, height uint64, full bool) ([]byte, error) {
	params := map[string]interface{}{}
	params["heightPtr"] = &height
	var blockresult ctypes.ResultBlock
	rclient := rpcclient.NewJSONRPCClient(RpcHost)
	_, err := rclient.Call("block", params, &blockresult)
	if err != nil {
		// todo Tendermint returns an
		// "Error unmarshalling rpc response result: Unregistered interface crypto.Signature"
		// error plus the correct ResultBlock object.
		// The Block function itself returns the correct ctypes.ResultBlock with no issues
		// So something to do with the RPC interface, ignoring it for the moment.
		// Likely some version mismatch.
		//
		// return nil, err
	}

	blockinfo := types.EthBlockInfo{
		Hash:       blockresult.BlockMeta.BlockID.Hash,
		ParentHash: blockresult.Block.Header.LastBlockID.Hash,

		Timestamp: int64(blockresult.Block.Header.Time.Unix()),
	}
	if uint64(state.Block().Height) == height {
		blockinfo.Number = 0
	} else {
		blockinfo.Number = int64(height)
	}

	txHashState := store.PrefixKVReader(utils.TxHashPrefix, state)
	txHash := txHashState.Get(utils.BlockHeightToBytes(height))
	if len(txHash) > 0 {
		receiptState := store.PrefixKVReader(utils.ReceiptPrefix, state)
		txReceiptProto := receiptState.Get(txHash)
		txReceipt := types.EvmTxReceipt{}
		if err := proto.Unmarshal(txReceiptProto, &txReceipt); err != nil {
			return nil, err
		}
		blockinfo.LogsBloom = txReceipt.LogsBloom
		if full {
			blockinfo.Transactions = append(blockinfo.Transactions, txReceiptProto)
		} else {
			blockinfo.Transactions = append(blockinfo.Transactions, txHash)
		}
	}

	return proto.Marshal(&blockinfo)
}

func GetBlockByHash(state loomchain.ReadOnlyState, hash []byte, full bool) ([]byte, error) {
	/*return nil, fmt.Errorf("not implemented")*/
	start := uint64(state.Block().Height)
	var end uint64
	if uint64(start) > searchBlockSize {
		end = uint64(start) - searchBlockSize
	} else {
		end = 1
	}

	for start > 0 {
		params := map[string]interface{}{}
		params["minHeight"] = end
		params["maxHeight"] = start
		var info ctypes.ResultBlockchainInfo
		rclient := rpcclient.NewJSONRPCClient(RpcHost)
		_, err := rclient.Call("blockchain", params, &info)
		if err != nil {
			return nil, err
		}
		for i := uint64(len(info.BlockMetas) - 1); i >= 0; i-- {
			if 0 == bytes.Compare(hash, info.BlockMetas[i].BlockID.Hash) {
				return GetBlockByNumber(state, end+i, full)
			}
		}

		if end == 1 {
			return nil, fmt.Errorf("can't find block to match hash")
		}

		start = end
		if uint64(start) > searchBlockSize {
			end = uint64(start) - searchBlockSize
		} else {
			end = 1
		}
	}
	return nil, fmt.Errorf("can't find block to match hash")
}