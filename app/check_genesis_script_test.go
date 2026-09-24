package app_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/twilight-project/twilight-core/app"
)

// TestCheckGenesisModuleAccountListMatchesApp holds scripts/check-genesis.sh to
// the app's module-account declaration.
//
// The genesis verifier refuses a module account as a payout, settlement or
// treasury address because InitGenesis does, and it cannot ask the app which
// accounts those are: it runs against a genesis file, not a node. So it carries
// a copy of the names. A copy is a denylist that drifts the first time a module
// account is added — and a stale copy fails toward approving a genesis the chain
// then refuses at start-up. This test is what makes the copy safe to keep.
func TestCheckGenesisModuleAccountListMatchesApp(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "scripts", "check-genesis.sh"))
	require.NoError(t, err)

	match := regexp.MustCompile(`(?m)^MODULE_ACCOUNT_NAMES=\(([^)]*)\)$`).FindSubmatch(script)
	require.NotNil(t, match, "scripts/check-genesis.sh no longer declares MODULE_ACCOUNT_NAMES=(...) on one line")
	scriptNames := strings.Fields(string(match[1]))

	appNames := append([]string(nil), app.ModuleAccountNames()...)
	sort.Strings(scriptNames)
	sort.Strings(appNames)
	require.Equal(t, appNames, scriptNames,
		"scripts/check-genesis.sh MODULE_ACCOUNT_NAMES must list exactly app.ModuleAccountNames()")
}

// TestCheckGenesisModuleAccountListCoversBankBlocked pins the second claim the
// verifier makes about that list: that it also covers the bank module's blocked
// set, which the economic-address rule refuses independently. If bank ever
// blocks an address that is not one of these module accounts, the verifier
// would approve a destination the chain refuses.
func TestCheckGenesisModuleAccountListCoversBankBlocked(t *testing.T) {
	covered := map[string]bool{}
	for _, name := range app.ModuleAccountNames() {
		covered[authtypes.NewModuleAddress(name).String()] = true
	}

	a := newApp(t)
	blocked := a.BankKeeper.GetBlockedAddresses()
	addresses := make([]string, 0, len(blocked))
	for address, isBlocked := range blocked {
		if isBlocked {
			addresses = append(addresses, address)
		}
	}
	// An empty set would make the loop below pass vacuously; bank blocks the
	// module accounts by default, so there is always something to check.
	require.NotEmpty(t, addresses, "bank reports no blocked addresses")
	sort.Strings(addresses)
	for _, address := range addresses {
		require.Truef(t, covered[address],
			"bank blocks %s, which is not a declared module account; scripts/check-genesis.sh would not refuse it", address)
	}
}
