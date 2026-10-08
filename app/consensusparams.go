package app

import (
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	consensuskeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
)

// SetBlockParams sets block.max_bytes and block.max_gas from inside an upgrade
// handler (#170). It is the only way to change them on a running network: the
// consensus module's authority is a keyless module account, so no transaction can.
//
// It changes the block section and nothing else. The evidence, validator, version
// and ABCI sections are written back exactly as stored. That is why this does not
// call the consensus keeper's UpdateParams: that path rebuilds the version section
// from CometBFT's defaults on every call, and CometBFT copies the version it is
// handed into later block headers, so a block-params change would also have reset
// the app version. The checks are the ones UpdateParams applies, run here on the
// same values: CometBFT's ValidateBasic on the result (max_bytes positive or -1 and
// within CometBFT's hard maximum, max_gas -1 or more) and ValidateUpdate against the
// stored params.
//
// A rejected value returns an error, and an error from a handler aborts the block:
// the node halts at the upgrade height rather than run with parameters it was not
// asked for. So a value must be checked before the release that carries it, not
// discovered at the height.
//
// BaseApp reports the stored parameters to CometBFT at the end of every
// FinalizeBlock, so the new values bind from the block after the upgrade height.
func SetBlockParams(ctx sdk.Context, k consensuskeeper.Keeper, maxBytes, maxGas int64) error {
	stored, err := k.ParamsStore.Get(ctx)
	if err != nil {
		return fmt.Errorf("read consensus params: %w", err)
	}
	if stored.Block == nil || stored.Evidence == nil || stored.Validator == nil {
		return fmt.Errorf("stored consensus params are incomplete: block, evidence and validator must all be set")
	}
	if stored.Version == nil {
		// The same zero-value initialization UpdateParams performs; ConsensusParamsFromProto
		// dereferences the version section.
		stored.Version = &cmtproto.VersionParams{}
	}

	current := cmttypes.ConsensusParamsFromProto(stored)
	next := current
	next.Block = cmttypes.BlockParams{MaxBytes: maxBytes, MaxGas: maxGas}

	if err := next.ValidateBasic(); err != nil {
		return fmt.Errorf("block params max_bytes=%d max_gas=%d: %w", maxBytes, maxGas, err)
	}
	update := next.ToProto()
	if err := current.ValidateUpdate(&update, ctx.BlockHeight()); err != nil {
		return fmt.Errorf("block params max_bytes=%d max_gas=%d: %w", maxBytes, maxGas, err)
	}
	if err := k.ParamsStore.Set(ctx, update); err != nil {
		return fmt.Errorf("write consensus params: %w", err)
	}
	return nil
}
