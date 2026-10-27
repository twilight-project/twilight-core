package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/twilight-project/twilight-core/cmd/twilightd/cmd"
)

// rewardsTxCommands is every rewards operation an operator can perform, as the
// hand-written tree names them. All three must be reachable at `tx rewards`.
var rewardsTxCommands = []string{"pause", "resume", "update-params"}

// rewardsGeneratedTxCommands is how AutoCLI spelled the same three operations
// from the Msg service. Unlike CoreSlot's generated tree these worked; the point
// is that the chain offered two names for every rewards transaction (#144).
var rewardsGeneratedTxCommands = []string{"pause-rewards", "resume-rewards", "update-rewards-params"}

// The rewards transaction surface is asserted against the FINAL assembled tree,
// for the reason set out in upgrade_cli_test.go and coreslot_cli_test.go:
// AutoCLI composes from two independent sources, and inspecting the options that
// feed it proves nothing about what an operator ends up with.
func TestRewardsCommandSurface(t *testing.T) {
	root := cmd.NewRootCmd()
	require.NotNil(t, root)

	t.Run("every rewards operation is reachable at tx rewards", func(t *testing.T) {
		for _, name := range rewardsTxCommands {
			require.NotNil(t, find(root, "tx", "rewards", name),
				"`tx rewards %s` must exist: it is the canonical surface", name)
		}
	})

	t.Run("the generated spellings are absent", func(t *testing.T) {
		for _, name := range rewardsGeneratedTxCommands {
			require.Nil(t, find(root, "tx", "rewards", name),
				"`tx rewards %s` is the retired AutoCLI spelling and must not be reachable", name)
		}
	})

	t.Run("tx rewards exposes exactly the hand-written operations", func(t *testing.T) {
		parent := find(root, "tx", "rewards")
		require.NotNil(t, parent)
		require.Len(t, parent.Commands(), len(rewardsTxCommands),
			"`tx rewards` must expose exactly the three canonical commands")
		// An unknown subcommand must be refused here, not answered with help and
		// exit 0: a script still carrying a generated spelling has to fail.
		require.NotNil(t, parent.RunE)
		err := parent.RunE(parent, []string{"pause-rewards"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown command")
	})

	t.Run("the root-level compatibility surface remains", func(t *testing.T) {
		// scripts/localnet/rewards-soak.sh drives pause, resume and update-params
		// through this path. Moving the canonical surface must not move this one.
		for _, name := range rewardsTxCommands {
			require.NotNil(t, find(root, "rewards", name))
		}
		// Two mounts of one constructor, not one object mounted twice: cobra's
		// AddCommand only overwrites the parent pointer, so a shared object would
		// silently re-parent the root tree under `tx` (and inherit its flags).
		txTree, rootTree := find(root, "tx", "rewards"), find(root, "rewards")
		require.NotSame(t, txTree, rootTree)
		require.Equal(t, "twilightd tx rewards", txTree.CommandPath())
		require.Equal(t, "twilightd rewards", rootTree.CommandPath())
		for _, name := range rewardsTxCommands {
			require.NotSame(t, find(root, "tx", "rewards", name), find(root, "rewards", name))
		}
	})

	t.Run("the generated query tree is untouched", func(t *testing.T) {
		// Queries were never duplicated under conflicting names; the module supplies
		// no custom query command, so `query rewards` must still be generated.
		require.NotNil(t, find(root, "query", "rewards"))
		require.NotNil(t, find(root, "query", "rewards", "params"))
		require.NotNil(t, find(root, "rewards-query", "params"))
	})

	t.Run("unrelated trees are untouched", func(t *testing.T) {
		require.NotNil(t, find(root, "tx", "coreslot", "register"))
		require.NotNil(t, find(root, "tx", "bank", "send"))
		require.NotNil(t, find(root, "tx", "mining"))
	})
}
