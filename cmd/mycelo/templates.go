package main

import (
	"math/big"
	"math/rand"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/mycelo/env"
	"github.com/ethereum/go-ethereum/mycelo/genesis"
	"github.com/ethereum/go-ethereum/params"
)

type template interface {
	createEnv(workdir string) (*env.Environment, error)
	createGenesisConfig(*env.Environment) (*genesis.Config, error)
}

func templateFromString(templateStr string) template {
	switch templateStr {
	case "local":
		return localEnv{}
	case "loadtest":
		return loadtestEnv{}
	}
	return localEnv{}
}

type localEnv struct{}

func (e localEnv) createEnv(workdir string) (*env.Environment, error) {
	envCfg := &env.Config{
		Mnemonic:           env.MustNewMnemonic(),
		InitialValidators:  3,
		ValidatorsPerGroup: 1,
		DeveloperAccounts:  10,
		LoadTestTPS:        10,
		ChainID:            big.NewInt(1000 * (1 + rand.Int63n(9999))),
	}
	env, err := env.New(workdir, envCfg)
	if err != nil {
		return nil, err
	}

	return env, nil
}

func (e localEnv) createGenesisConfig(env *env.Environment) (*genesis.Config, error) {

	genesisConfig := genesis.BaseConfig()
	genesisConfig.ChainID = env.Config.ChainID
	genesisConfig.GenesisTimestamp = uint64(time.Now().Unix())
	genesisConfig.Istanbul = params.IstanbulConfig{
		Epoch:          10,
		ProposerPolicy: 2,
		LookbackWindow: 3,
		BlockPeriod:    1,
		RequestTimeout: 3000,
	}
	genesisConfig.Hardforks = genesis.HardforkConfig{
		ChurritoBlock: common.Big0,
		DonutBlock:    common.Big0,
	}

	genesisConfig.Blockchain.UptimeLookbackWindow = int64(genesisConfig.Istanbul.LookbackWindow)

	// Make admin account manager of Governance & Reserve
	adminMultisig := genesis.MultiSigParameters{
		Signatories:                      []common.Address{env.AdminAccount().Address},
		NumRequiredConfirmations:         1,
		NumInternalRequiredConfirmations: 1,
	}

	genesisConfig.ReserveSpenderMultiSig = adminMultisig
	genesisConfig.GovernanceApproverMultiSig = adminMultisig

	// Add balances to developer accounts
	cusdBalances := make([]genesis.Balance, len(env.DeveloperAccounts()))
	goldBalances := make([]genesis.Balance, len(env.DeveloperAccounts()))
	for i, acc := range env.DeveloperAccounts() {
		cusdBalances[i] = genesis.Balance{acc.Address, genesis.MustBigInt("50000000000000000000000")}
		goldBalances[i] = genesis.Balance{acc.Address, genesis.MustBigInt("1000000000000000000000000")}
	}

	genesisConfig.StableToken.InitialBalances = cusdBalances
	genesisConfig.GoldToken.InitialBalances = goldBalances

	// Ensure nothing is frozen
	genesisConfig.GoldToken.Frozen = false
	genesisConfig.StableToken.Frozen = false
	genesisConfig.Exchange.Frozen = false
	genesisConfig.Reserve.FrozenDays = nil
	genesisConfig.Reserve.FrozenAssetsDays = nil
	genesisConfig.EpochRewards.Frozen = false

	return genesisConfig, nil
}

type loadtestEnv struct{}

func (e loadtestEnv) createEnv(workdir string) (*env.Environment, error) {
	envCfg := &env.Config{
		Mnemonic:           "miss fire behind decide egg buyer honey seven advance uniform profit renew",
		InitialValidators:  1,
		ValidatorsPerGroup: 1,
		DeveloperAccounts:  1000,
		ChainID:            big.NewInt(9099000),
		LoadTestTPS:        50,
	}

	env, err := env.New(workdir, envCfg)
	if err != nil {
		return nil, err
	}

	return env, nil
}

func (e loadtestEnv) createGenesisConfig(env *env.Environment) (*genesis.Config, error) {
	genesisConfig := genesis.BaseConfig()

	genesisConfig.ChainID = env.Config.ChainID
	genesisConfig.GenesisTimestamp = uint64(time.Now().Unix())
	genesisConfig.Istanbul = params.IstanbulConfig{
		Epoch:          1000,
		ProposerPolicy: 2,
		LookbackWindow: 3,
		BlockPeriod:    5,
		RequestTimeout: 3000,
	}
	genesisConfig.Hardforks = genesis.HardforkConfig{
		ChurritoBlock: common.Big0,
		DonutBlock:    common.Big0,
	}

	genesisConfig.Blockchain.UptimeLookbackWindow = int64(genesisConfig.Istanbul.LookbackWindow)

	// Make admin account manager of Governance & Reserve
	adminMultisig := genesis.MultiSigParameters{
		Signatories:                      []common.Address{env.AdminAccount().Address},
		NumRequiredConfirmations:         1,
		NumInternalRequiredConfirmations: 1,
	}

	genesisConfig.ReserveSpenderMultiSig = adminMultisig
	genesisConfig.GovernanceApproverMultiSig = adminMultisig

	// Add balances to developer accounts
	cusdBalances := make([]genesis.Balance, len(env.DeveloperAccounts()))
	goldBalances := make([]genesis.Balance, len(env.DeveloperAccounts()))
	for i, acc := range env.DeveloperAccounts() {
		cusdBalances[i] = genesis.Balance{acc.Address, genesis.MustBigInt("10000000000000000000000000")}
		goldBalances[i] = genesis.Balance{acc.Address, genesis.MustBigInt("10000000000000000000000000")}
	}

	genesisConfig.StableToken.InitialBalances = cusdBalances
	genesisConfig.GoldToken.InitialBalances = goldBalances

	// Ensure nothing is frozen
	genesisConfig.GoldToken.Frozen = false
	genesisConfig.StableToken.Frozen = false
	genesisConfig.Exchange.Frozen = false
	genesisConfig.Reserve.FrozenDays = nil
	genesisConfig.Reserve.FrozenAssetsDays = nil
	genesisConfig.EpochRewards.Frozen = false

	return genesisConfig, nil
}
