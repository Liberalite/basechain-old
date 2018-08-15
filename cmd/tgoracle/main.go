// +build evm

package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/loomnetwork/loomchain/gateway"
	"github.com/spf13/viper"
)

type LoomConfig struct {
	ChainID         string
	RPCProxyPort    int32
	TransferGateway *gateway.TransferGatewayConfig
}

func main() {
	cfg, err := parseConfig(nil)
	if err != nil {
		panic(err)
	}

	orc, err := gateway.CreateOracle(cfg.TransferGateway, cfg.ChainID)
	if err != nil {
		panic(err)
	}

	go orc.RunWithRecovery()

	// Run forever until interrupted by SIGTERM
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	for sig := range c {
		fmt.Printf("captured %v, exiting...\n", sig)
		os.Exit(1)
	}
}

// Loads loom.yml or equivalent from one of the usual location, or if overrideCfgDirs is provided
// from one of those config directories.
func parseConfig(overrideCfgDirs []string) (*LoomConfig, error) {
	v := viper.New()
	v.SetConfigName("loom")
	if len(overrideCfgDirs) == 0 {
		// look for the loom config file in all the places loom itself does
		v.AddConfigPath(".")
		v.AddConfigPath(filepath.Join(".", "config"))
	} else {
		for _, dir := range overrideCfgDirs {
			v.AddConfigPath(dir)
		}
	}
	v.ReadInConfig()
	conf := &LoomConfig{
		ChainID:         "default",
		RPCProxyPort:    46658,
		TransferGateway: gateway.DefaultConfig(46658),
	}
	err := v.Unmarshal(conf)
	if err != nil {
		return nil, err
	}
	return conf, err
}