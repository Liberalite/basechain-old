package gateway

import (
	"runtime"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/loomnetwork/go-loom/auth"
	"github.com/loomnetwork/go-loom/client"
	log "github.com/loomnetwork/loomchain/log"
	"github.com/pkg/errors"
)

type OracleConfig struct {
	// URI of an Ethereum node
	EthereumURI string
	// Gateway contract address on Ethereum
	GatewayHexAddress string
	ChainID           string
	WriteURI          string
	ReadURI           string
	Signer            auth.Signer
}

type Oracle struct {
	cfg        OracleConfig
	solGateway *Gateway
	goGateway  *client.Contract
	startBlock uint64
	logger     log.TMLogger
	ethClient  *ethclient.Client
}

func NewOracle(cfg OracleConfig) *Oracle {
	return &Oracle{
		cfg:    cfg,
		logger: log.Root.With("module", "gateway-oracle"),
	}
}

func (orc *Oracle) Init() error {
	cfg := &orc.cfg
	var err error
	orc.ethClient, err = ethclient.Dial(cfg.EthereumURI)
	if err != nil {
		return errors.Wrap(err, "failed to connect to Ethereum")
	}

	orc.solGateway, err = NewGateway(common.HexToAddress(cfg.GatewayHexAddress), orc.ethClient)
	if err != nil {
		return errors.Wrap(err, "failed to bind Gateway Solidity contract")
	}

	dappClient := client.NewDAppChainRPCClient(cfg.ChainID, cfg.WriteURI, cfg.ReadURI)
	contractAddr, err := dappClient.Resolve("gateway")
	if err != nil {
		return errors.Wrap(err, "failed to resolve Gateway Go contract address")
	}
	orc.goGateway = client.NewContract(dappClient, contractAddr.Local)
	return nil
}

// RunWithRecovery should run in a goroutine, it will ensure the oracle keeps on running as long
// as it doesn't panic due to a runtime error.
func (orc *Oracle) RunWithRecovery() {
	defer func() {
		if r := recover(); r != nil {
			orc.logger.Error("recovered from panic in Gateway Oracle", "r", r)
			// Unless it's a runtime error restart the goroutine
			if _, ok := r.(runtime.Error); !ok {
				time.Sleep(30 * time.Second)
				orc.logger.Info("Restarting Gateway Oracle...")
				go orc.RunWithRecovery()
			}
		}
	}()
	orc.Run()
}

// TODO: Graceful shutdown
func (orc *Oracle) Run() {
	//req := &gwc.GatewayStateRequest{}
	//callerAddr := loom.RootAddress(orc.cfg.ChainID)
	skipSleep := true
	for {
		if !skipSleep {
			// TODO: should be configurable
			time.Sleep(5 * time.Second)
		} else {
			skipSleep = false
		}
		/*
			// TODO: since the oracle is running in-process we can bypass the RPC... but that's going
			// to require a bit of refactoring to avoid duplicating a bunch of QueryServer code... or
			// maybe just pass through an instance of the QueryServer?
			var resp gwc.GatewayStateResponse
			if _, err := orc.goGateway.StaticCall("GetState", req, callerAddr, &resp); err != nil {
				orc.logger.Error("failed to retrieve state from Gateway contract on DAppChain", "err", err)
				continue
			}

			startBlock := resp.State.LastEthBlock + 1
			if orc.startBlock >= startBlock {
				// We've already processed this block successfully... so sit this one out.
				// TODO: figure out if this is actually a good idea
				continue
			}

			// TODO: limit max block range per batch
			latestBlock, err := orc.getLatestEthBlockNumber()
			if err != nil {
				orc.logger.Error("failed to obtain latest Ethereum block number", "err", err)
				continue
			}

			if latestBlock < startBlock {
				// Wait for Ethereum to produce a new block...
				continue
			}

			batch, err := orc.fetchEvents(startBlock, latestBlock)
			if err != nil {
				orc.logger.Error("failed to fetch events from Ethereum", "err", err)
				continue
			}

			if _, err := orc.goGateway.Call("ProcessEventBatch", batch, orc.cfg.Signer, nil); err != nil {
				orc.logger.Error("failed to commit ProcessEventBatch tx", "err", err)
				continue
			}

			orc.startBlock = latestBlock + 1
		*/
	}
}

/*
func (orc *Oracle) getLatestEthBlockNumber() (uint64, error) {
	blockHeader, err := orc.ethClient.HeaderByNumber(context.TODO(), nil)
	if err != nil {
		return 0, err
	}
	return blockHeader.Number.Uint64(), nil
}

// Fetches all relevent events from an Ethereum node from startBlock to endBlock (inclusive)
func (orc *Oracle) fetchEvents(startBlock, endBlock uint64) (*gwc.ProcessEventBatchRequest, error) {
	// NOTE: Currently either all blocks from w.StartBlock are processed successfully or none are.
	filterOpts := &bind.FilterOpts{
		Start: startBlock,
		End:   &endBlock,
	}
	ftDeposits := []*gwc.TokenDeposit{}
	nftDeposits := []*gwc.NFTDeposit{}

	ethTokenAddr := loom.RootAddress("eth")
	// These two are just placeholders for now
	erc20TokenAddr := loom.RootAddress("erc20")
	erc721TokenAddr := loom.RootAddress("erc721")

	// TODO: Currently there are 3 separate requests being made, should just make one for all 3
	//       events but that would require more work figuring the relavant go-ethereum API
	ethIt, err := orc.solGateway.FilterETHReceived(filterOpts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get logs for ETHReceived")
	}
	for {
		ok := ethIt.Next()
		if ok {
			ev := ethIt.Event
			fromAddr, err := loom.LocalAddressFromHexString(ev.From.Hex())
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse ETHReceived from address")
			}
			// TODO: Update Solidity contract to emit the to addr
			toAddr := loom.Address{}
			ftDeposits = append(ftDeposits, &gwc.TokenDeposit{
				Token:    ethTokenAddr.MarshalPB(),
				From:     loom.Address{ChainID: "eth", Local: fromAddr}.MarshalPB(),
				To:       toAddr.MarshalPB(),
				Amount:   &ltypes.BigUInt{Value: *loom.NewBigUInt(ev.Amount)},
				EthBlock: ev.Raw.BlockNumber,
			})
		} else {
			err := ethIt.Error()
			if err != nil {
				return nil, errors.Wrap(err, "failed to get event data for ETHReceived")
			}
			ethIt.Close()
			break
		}
	}

	erc20It, err := orc.solGateway.FilterERC20Received(filterOpts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get logs for ERC20Received")
	}
	for {
		ok := erc20It.Next()
		if ok {
			ev := erc20It.Event
			fromAddr, err := loom.LocalAddressFromHexString(ev.From.Hex())
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse ERC20Received from address")
			}
			ftDeposits = append(ftDeposits, &gwc.TokenDeposit{
				// TODO: fill in the actual token address
				Token:    erc20TokenAddr.MarshalPB(),
				From:     loom.Address{ChainID: "eth", Local: fromAddr}.MarshalPB(),
				Amount:   &ltypes.BigUInt{Value: *loom.NewBigUInt(ev.Amount)},
				EthBlock: ev.Raw.BlockNumber,
			})
		} else {
			err := erc20It.Error()
			if err != nil {
				return nil, errors.Wrap(err, "failed to get event data for ERC20Received")
			}
			erc20It.Close()
			break
		}
	}

	erc721It, err := orc.solGateway.FilterERC721Received(filterOpts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get logs for ERC721Received")
	}
	for {
		ok := erc721It.Next()
		if ok {
			ev := erc721It.Event
			localAddr, err := loom.LocalAddressFromHexString(ev.From.Hex())
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse ERC721Received from address")
			}
			nftDeposits = append(nftDeposits, &gwc.NFTDeposit{
				// TODO: fill in the actual token address
				Token:    erc721TokenAddr.MarshalPB(),
				From:     loom.Address{ChainID: "eth", Local: localAddr}.MarshalPB(),
				Uid:      &ltypes.BigUInt{Value: *loom.NewBigUInt(ev.Uid)},
				EthBlock: ev.Raw.BlockNumber,
			})
		} else {
			err := erc721It.Error()
			if err != nil {
				return nil, errors.Wrap(err, "Failed to get event data for ERC721Received")
			}
			erc721It.Close()
			break
		}
	}

	return &gwc.ProcessEventBatchRequest{
		FtDeposits:  ftDeposits,
		NftDeposits: nftDeposits,
	}, nil
}
*/