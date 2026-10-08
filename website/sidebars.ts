import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

// Grouped by what a reader is trying to do, in the order they usually need it. A page's
// URL comes from its file path, not from its place here, so regrouping moves no URL.
const sidebars: SidebarsConfig = {
  docs: [
    { type: "doc", id: "intro", label: "Documentation Home" },
    {
      type: "category",
      label: "Start Here",
      collapsed: false,
      items: [
        { type: "doc", id: "getting-started/install", label: "Install" },
        { type: "doc", id: "getting-started/quickstart", label: "Quickstart" },
        { type: "doc", id: "getting-started/chain-concepts", label: "Chain Concepts" },
        { type: "doc", id: "chain/status-and-validation", label: "Status & Validation" },
        { type: "doc", id: "getting-started/glossary", label: "Glossary" },
      ],
    },
    {
      type: "category",
      label: "Run a Node",
      collapsed: false,
      items: [
        { type: "doc", id: "operators/node-operator-guide", label: "Node Operator Guide" },
        { type: "doc", id: "operators/monitoring", label: "Monitoring" },
        { type: "doc", id: "operators/upgrade-and-export-import", label: "Upgrade & Export/Import" },
        { type: "doc", id: "operators/keys-backup-and-recovery", label: "Keys, Backup & Recovery" },
        { type: "doc", id: "operators/incident-response", label: "Incident Response" },
      ],
    },
    {
      type: "category",
      label: "Validators",
      collapsed: false,
      items: [
        { type: "doc", id: "operators/validator-operations", label: "Validator Operations" },
        { type: "doc", id: "operators/coreslot-operator-guide", label: "CoreSlot Operator Guide" },
        { type: "doc", id: "operators/rewards-operator-guide", label: "Rewards Operator Guide" },
        { type: "doc", id: "operators/authority-and-emergency-guide", label: "Authority & Emergency" },
      ],
    },
    {
      type: "category",
      label: "How It Works",
      collapsed: true,
      items: [
        { type: "doc", id: "chain/architecture", label: "Architecture" },
        { type: "doc", id: "chain/consensus-and-coreslot", label: "Consensus & CoreSlot" },
        { type: "doc", id: "chain/lifecycle", label: "Block Lifecycle" },
        { type: "doc", id: "chain/accounts-and-denoms", label: "Accounts & Denoms" },
        { type: "doc", id: "rewards/overview", label: "Rewards Overview" },
        { type: "doc", id: "rewards/economics", label: "Economics" },
        { type: "doc", id: "rewards/epoch-lifecycle", label: "Epoch Lifecycle" },
        { type: "doc", id: "rewards/active-block-accounting", label: "Active-Block Accounting" },
        { type: "doc", id: "rewards/settlement", label: "Settlement" },
        { type: "doc", id: "rewards/invariants", label: "Invariants" },
        { type: "doc", id: "rewards/security-and-failure-modes", label: "Security & Failure Modes" },
      ],
    },
    {
      type: "category",
      label: "Integrate",
      collapsed: true,
      items: [
        { type: "doc", id: "reference/integrators", label: "Integrator Guide" },
        { type: "doc", id: "reference/events", label: "Events" },
      ],
    },
    {
      type: "category",
      label: "Reference",
      collapsed: true,
      items: [
        { type: "doc", id: "reference/cli", label: "CLI & Queries" },
        { type: "doc", id: "rewards/transactions", label: "Rewards Transactions" },
        { type: "doc", id: "rewards/params", label: "Parameters" },
        { type: "doc", id: "reference/genesis-reference", label: "Genesis" },
        { type: "doc", id: "reference/module-accounts", label: "Module Accounts" },
      ],
    },
    {
      type: "category",
      label: "Develop",
      collapsed: true,
      items: [
        { type: "doc", id: "development/localnet-drills", label: "Localnet & Drills" },
        { type: "doc", id: "development/testing", label: "Testing" },
        { type: "doc", id: "development/repo-map", label: "Repo Map" },
        { type: "doc", id: "development/contributing", label: "Contributing" },
      ],
    },
  ],
};

export default sidebars;
