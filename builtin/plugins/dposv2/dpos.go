package dposv2

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"sort"

	loom "github.com/loomnetwork/go-loom"
	dtypes "github.com/loomnetwork/go-loom/builtin/types/dposv2"
	types "github.com/loomnetwork/go-loom/types"
	"github.com/loomnetwork/go-loom/plugin"
	contract "github.com/loomnetwork/go-loom/plugin/contractpb"
)

var (
	decimals                  int64 = 18
	errCandidateNotRegistered       = errors.New("candidate is not registered")
)

type (
	InitRequest                = dtypes.DPOSInitRequestV2
	DelegateRequest            = dtypes.DelegateRequestV2
	UnbondRequest              = dtypes.UnbondRequestV2
	CheckDelegationRequest     = dtypes.CheckDelegationRequestV2
	CheckDelegationResponse    = dtypes.CheckDelegationResponseV2
	RegisterCandidateRequest   = dtypes.RegisterCandidateRequestV2
	UnregisterCandidateRequest = dtypes.UnregisterCandidateRequestV2
	ListCandidateRequest       = dtypes.ListCandidateRequestV2
	ListCandidateResponse      = dtypes.ListCandidateResponseV2
	ListValidatorsRequest      = dtypes.ListValidatorsRequestV2
	ListValidatorsResponse     = dtypes.ListValidatorsResponseV2
	ElectDelegationRequest     = dtypes.ElectDelegationRequestV2
	Candidate                  = dtypes.CandidateV2
	Delegation                 = dtypes.DelegationV2
	Validator                  = types.Validator
	State                      = dtypes.StateV2
	Params                     = dtypes.ParamsV2
)

type DPOS struct {
}

func (c *DPOS) Meta() (plugin.Meta, error) {
	return plugin.Meta{
		Name:    "dposV2",
		Version: "2.0.0",
	}, nil
}

func (c *DPOS) Init(ctx contract.Context, req *InitRequest) error {
	fmt.Fprintf(os.Stderr, "Init DPOS Params %#v\n", req)
	params := req.Params

	if params.CoinContractAddress == nil {
		addr, err := ctx.Resolve("coin")
		if err != nil {
			return err
		}
		params.CoinContractAddress = addr.MarshalPB()
	}

	validators := make([]*Validator, len(req.Validators), len(req.Validators))
	for i, val := range req.Validators {
		validators[i] = &Validator{
			PubKey: val.PubKey,
		}
	}

	sortedValidators := sortValidators(validators)
	state := &State{
		Params:           params,
		Validators:       sortedValidators,
		LastElectionTime: ctx.Now().Unix(),
	}

	return saveState(ctx, state)
}

func (c *DPOS) Delegate(ctx contract.Context, req *DelegateRequest) error {
	state, err := loadState(ctx)
	if err != nil {
		return err
	}

	params := state.Params
	coinAddr := loom.UnmarshalAddressPB(params.CoinContractAddress)
	coin := &ERC20{
		Context:         ctx,
		ContractAddress: coinAddr,
	}

	delegator := ctx.Message().Sender
	dposContractAddress := ctx.ContractAddress()
	err = coin.TransferFrom(delegator, dposContractAddress, &req.Amount.Value)
	if err != nil {
		return err
	}

	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return err
	}
	priorDelegation := delegations.Get(*req.ValidatorAddress, *delegator.MarshalPB())

	updatedAmount := loom.BigUInt{big.NewInt(0)}
	if priorDelegation != nil {
		updatedAmount.Add(&priorDelegation.Amount.Value, &req.Amount.Value)
	} else {
		updatedAmount = req.Amount.Value
	}

	delegation := &Delegation{
		Validator: req.ValidatorAddress,
		Delegator: delegator.MarshalPB(),
		Amount:  &types.BigUInt{updatedAmount},
		Height: uint64(ctx.Block().Height),
	}
	delegations.Set(delegation)

	return saveDelegationList(ctx, delegations)
}

func (c *DPOS) Unbond(ctx contract.Context, req *UnbondRequest) error {
	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return err
	}

	// TODO abstract this in the three places it appears
	state, err := loadState(ctx)
	if err != nil {
		return err
	}
	params := state.Params
	coinAddr := loom.UnmarshalAddressPB(params.CoinContractAddress)
	coin := &ERC20{
		Context:         ctx,
		ContractAddress: coinAddr,
	}

	delegator := ctx.Message().Sender

	delegation := delegations.Get(*req.ValidatorAddress, *delegator.MarshalPB())
	if delegation == nil {
		return errors.New(fmt.Sprintf("delegation not found: %s %s", req.ValidatorAddress, delegator.MarshalPB()))
	} else {
		if delegation.Amount.Value.Cmp(&req.Amount.Value) == 1 {
			return errors.New("unbond amount exceeds delegation amount")
		} else {
			err = coin.Transfer(delegator, &req.Amount.Value)
			updatedAmount := loom.BigUInt{big.NewInt(0)}
			updatedAmount.Sub(&delegation.Amount.Value, &req.Amount.Value)
			updatedDelegation := &Delegation{
				Delegator: delegator.MarshalPB(),
				Validator: req.ValidatorAddress,
				Amount: &types.BigUInt{updatedAmount},
				Height: uint64(ctx.Block().Height),
			}
			delegations.Set(updatedDelegation)
		}
	}

	return saveDelegationList(ctx, delegations)
}

func (c *DPOS) CheckDelegation(ctx contract.Context, req *CheckDelegationRequest) (*CheckDelegationResponse, error) {
	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return nil, err
	}
	delegator := ctx.Message().Sender
	delegation := delegations.Get(*req.ValidatorAddress, *delegator.MarshalPB())
	if delegation == nil {
		return nil, errors.New(fmt.Sprintf("delegation not found: %s %s", req.ValidatorAddress, delegator.MarshalPB()))
	} else {
		return &CheckDelegationResponse{delegation}, nil
	}
}

func (c *DPOS) RegisterCandidate(ctx contract.Context, req *RegisterCandidateRequest) error {
	candidateAddress := ctx.Message().Sender
	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return err
	}

	checkAddr := loom.LocalAddressFromPublicKey(req.PubKey)
	if candidateAddress.Local.Compare(checkAddr) != 0 {
		return errors.New("public key does not match address")
	}

	newCandidate := &dtypes.CandidateV2{
		PubKey:  req.PubKey,
		Address: candidateAddress.MarshalPB(),
	}
	candidates.Set(newCandidate)
	return saveCandidateList(ctx, candidates)
}

func (c *DPOS) UnregisterCandidate(ctx contract.Context, req *dtypes.UnregisterCandidateRequestV2) error {
	candidateAddress := ctx.Message().Sender
	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return err
	}

	cand := candidates.Get(candidateAddress)
	if cand == nil {
		return errCandidateNotRegistered
	}

	candidates.Delete(candidateAddress)
	return saveCandidateList(ctx, candidates)
}

func (c *DPOS) ListCandidates(ctx contract.StaticContext, req *ListCandidateRequest) (*ListCandidateResponse, error) {
	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return nil, err
	}

	return &ListCandidateResponse{
		Candidates: candidates,
	}, nil
}

func (c *DPOS) ElectByDelegation(ctx contract.Context, req *ElectDelegationRequest) error {
	delegations, err := loadDelegationList(ctx)
	if err != nil {
		return err
	}

	counts := make(map[string]*loom.BigUInt)
	for _, delegation := range delegations {
		validatorKey := loom.UnmarshalAddressPB(delegation.Validator).String()
		if counts[validatorKey] != nil {
			counts[validatorKey].Add(counts[validatorKey], &delegation.Amount.Value)
		} else {
			counts[validatorKey] = &delegation.Amount.Value
		}
	}

	delegationResults := make([]*DelegationResult, 0, len(counts))
	for validator := range counts {
		delegationResults = append(delegationResults, &DelegationResult{
				ValidatorAddress:  loom.MustParseAddress(validator),
				DelegationTotal:   *counts[validator],
			})
	}
	sort.Sort(byDelegationTotal(delegationResults))

	state, err := loadState(ctx)
	if err != nil {
		return err
	}
	params := state.Params
	validatorCount := int(params.ValidatorCount)
	if len(delegationResults) < validatorCount {
		validatorCount = len(delegationResults)
	}

	candidates, err := loadCandidateList(ctx)
	if err != nil {
		return err
	}

	for _, validator := range state.Validators {
		ctx.SetValidatorPower(validator.PubKey, 0)
	}

	validators := make([]*Validator, 0)
	for _, res := range delegationResults[:validatorCount] {
		candidate := candidates.Get(res.ValidatorAddress)
		if candidate != nil {
			delegationTotal := res.DelegationTotal.Int
			validatorPower := delegationTotal.Div(delegationTotal, big.NewInt(1000000000)).Int64()
			validators = append(validators, &Validator{
				PubKey: candidate.PubKey,
				Power: validatorPower,
			})
			ctx.SetValidatorPower(candidate.PubKey, validatorPower)
		}
	}

	state.Validators = validators
	state.LastElectionTime = ctx.Now().Unix()
	return saveState(ctx, state)
}

func (c *DPOS) ListValidators(ctx contract.StaticContext, req *ListValidatorsRequest) (*ListValidatorsResponse, error) {
	state, err := loadState(ctx)
	if err != nil {
		return nil, err
	}

	return &ListValidatorsResponse{
		Validators: state.Validators,
	}, nil
}

var Contract plugin.Contract = contract.MakePluginContract(&DPOS{})