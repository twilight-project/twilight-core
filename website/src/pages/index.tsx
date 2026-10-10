import React from "react";
import Link from "@docusaurus/Link";
import Layout from "@theme/Layout";
import styles from "./index.module.css";

// The site documents `main`. The release named here is the latest tag; update it as
// part of cutting a release (see CONTRIBUTING.md, "Building a release").
const latestRelease = "v0.3.1";
const releasesUrl = "https://github.com/twilight-project/twilight-core/releases";

type Card = { title: string; desc: string; to: string; tag: string };

const cards: Card[] = [
  {
    tag: "Operators",
    title: "Run a node",
    desc: "Build, initialize, configure, and run twilightd; then a localnet for multi-node tests.",
    to: "/operators/node-operator-guide",
  },
  {
    tag: "Validators",
    title: "Become a validator",
    desc: "Admission by the authority, the slot lifecycle, key rotation, and what you can change about your slot.",
    to: "/operators/validator-operations",
  },
  {
    tag: "Integrators",
    title: "Build on the chain",
    desc: "Endpoints, reading state, submitting transactions, transfer rules, and the events to index.",
    to: "/reference/integrators",
  },
  {
    tag: "Concepts",
    title: "Rewards economics",
    desc: "Supply-threshold halving, epoch finalization, and uniform active-block distribution.",
    to: "/rewards/economics",
  },
  {
    tag: "Developers",
    title: "Build & contribute",
    desc: "Repo map, module map, testing layers, and contribution conventions.",
    to: "/development/repo-map",
  },
  {
    tag: "Status",
    title: "Status & validation",
    desc: "Current validation evidence, known limitations, and what has not yet been done.",
    to: "/chain/status-and-validation",
  },
];

export default function Home(): React.ReactElement {
  return (
    <Layout
      title="Twilight Chain Docs"
      description="Documentation for Twilight Chain, a CoreSlot Proof-of-Authority chain with utwlt rewards, and for the public Twilight Testnet."
    >
      <header className={styles.hero}>
        <div className={styles.heroInner}>
          <span className={styles.eyebrow}>CoreSlot PoA · utwlt rewards</span>
          <h1 className={styles.title}>Twilight Chain</h1>
          <p className={styles.tagline}>
            A Cosmos SDK / CometBFT Proof-of-Authority chain. Validators are
            authority-admitted CoreSlots; each epoch mints a bounded{" "}
            <code>utwlt</code> reward that is held as a per-slot entitlement and
            released by settlement.
          </p>
          <div className={styles.ctaRow}>
            <Link className={styles.ctaPrimary} to="/intro">
              Get started
            </Link>
            <Link className={styles.ctaSecondary} to="/operators/node-operator-guide">
              Run a node
            </Link>
            <Link className={styles.ctaSecondary} to="/chain/architecture">
              Understand the design
            </Link>
          </div>
          <p className={styles.statusNote}>
            <strong>Twilight Testnet</strong> is a public, pre-1.0 network: not
            externally audited, and its tokens have no value.{" "}
            <Link to="/chain/status-and-validation">Status &amp; validation</Link>.
          </p>
          <p className={styles.statusNote}>
            This site documents the <code>main</code> branch. The latest release is{" "}
            <a href={releasesUrl}>{latestRelease}</a>; a command documented here may
            be newer than that binary.
          </p>
        </div>
      </header>

      <main className={styles.main}>
        <div className={styles.cards}>
          {cards.map((c) => (
            <Link key={c.title} className={styles.card} to={c.to}>
              <span className={styles.cardTag}>{c.tag}</span>
              <h2 className={styles.cardTitle}>{c.title}</h2>
              <p className={styles.cardDesc}>{c.desc}</p>
              <span className={styles.cardArrow}>→</span>
            </Link>
          ))}
        </div>
      </main>
    </Layout>
  );
}
