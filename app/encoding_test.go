package app_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/app"
	miningtypes "github.com/twilight-project/twilight-core/x/mining/types"
	rewardstypes "github.com/twilight-project/twilight-core/x/rewards/types"
)

// The client-side codec has to know every message the chain can execute.
//
// Block execution uses the application's own registry, which the module manager
// populates while building the App. Nothing else does: the CLI, the node's
// transaction service and offline transaction building all go through
// MakeEncodingConfig. When those two disagree the chain executes a message
// perfectly and then cannot read it back, and no consensus path fails to say so.

// TestEveryChainMsgResolvesFromTheClientCodec walks the exported type-URL manifest
// rather than a list written here.
//
// The manifest is generated from the protos and CI already fails when it drifts, so
// a module added later arrives in this test automatically. A hand-written list
// would pass forever while the surface it claims to cover grew past it — which is
// the shape of the defect this test exists for.
func TestEveryChainMsgResolvesFromTheClientCodec(t *testing.T) {
	raw, err := os.ReadFile("../docs/proto/twilight-msg-type-urls.json")
	require.NoError(t, err)

	var manifest struct {
		Modules map[string][]string `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.NotEmpty(t, manifest.Modules, "the manifest must list the chain's messages")

	encoding := app.MakeEncodingConfig()

	checked := 0
	for module, urls := range manifest.Modules {
		require.NotEmptyf(t, urls, "module %s lists no messages", module)
		for _, url := range urls {
			_, err := encoding.InterfaceRegistry.Resolve(url)
			require.NoErrorf(t, err,
				"%s is executable on this chain but unresolvable from the client codec", url)
			checked++
		}
	}
	require.GreaterOrEqual(t, checked, 16, "the manifest should cover every custom message")
}

// TestTheManifestListsExactlyTheTwilightMsgsTheChainRegisters closes the other
// direction. The test above proves every listed message resolves; it cannot see a
// message the manifest leaves out, and that is how the three authority-rotation
// messages went missing while every gate stayed green (#69).
//
// The registered set comes from the client codec; every Twilight entry must also have
// a handler in the app's message router, so the chain can execute it. The manifest's bank entries are a deliberate subset and are not compared here;
// tools/msgmanifest checks them against the bank Msg service when it generates.
func TestTheManifestListsExactlyTheTwilightMsgsTheChainRegisters(t *testing.T) {
	raw, err := os.ReadFile("../docs/proto/twilight-msg-type-urls.json")
	require.NoError(t, err)

	var manifest struct {
		Modules map[string][]string `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))

	listed := map[string]bool{}
	for module, urls := range manifest.Modules {
		for _, url := range urls {
			require.Falsef(t, listed[url], "%s is listed twice", url)
			listed[url] = true
			if module != "bank" {
				require.Truef(t, strings.HasPrefix(url, "/twilight."),
					"module %s lists %s, which is not a Twilight message", module, url)
			}
		}
	}

	registered := map[string]bool{}
	for _, url := range app.MakeEncodingConfig().InterfaceRegistry.ListImplementations(sdk.MsgInterfaceProtoName) {
		if strings.HasPrefix(url, "/twilight.") {
			registered[url] = true
		}
	}
	require.NotEmpty(t, registered, "the client codec registers no Twilight messages")

	router := newApp(t).MsgServiceRouter()
	for url := range registered {
		require.Truef(t, listed[url],
			"%s is registered but missing from the manifest; run make proto-descriptor", url)
	}
	for url := range listed {
		if !strings.HasPrefix(url, "/twilight.") {
			continue
		}
		require.Truef(t, registered[url], "%s is in the manifest but not registered", url)
		require.NotNilf(t, router.HandlerByTypeURL(url), "%s is in the manifest but the chain has no handler for it", url)
	}
}

func TestCustomMsgsMarshalThroughTheLegacyAminoCodec(t *testing.T) {
	encoding := app.MakeEncodingConfig()

	for name, msg := range map[string]sdk.Msg{
		"mining":  &miningtypes.MsgSubmitSettlementChunk{SlotId: 1},
		"rewards": &rewardstypes.MsgPauseRewards{},
	} {
		t.Run(name, func(t *testing.T) {
			held := msg
			bz, err := encoding.Amino.MarshalJSON(&held)
			require.NoError(t, err, "an interface-held message must carry its registered name")
			require.NotEmpty(t, bz)
		})
	}
}
