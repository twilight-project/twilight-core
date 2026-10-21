#!/usr/bin/env bash
# The localnet port scheme, in one place. Sourced, not executed.
#
# Every node is addressed by index: node i listens on
#
#   rpc  = 26657 + LOCALNET_PORT_BASE + i*100   on 127.0.0.1
#   p2p  = 26656 + LOCALNET_PORT_BASE + i*100   on every interface (CometBFT's default)
#   grpc = 9090  + LOCALNET_PORT_BASE + i*100   on localhost
#
# and the two scripts that enable the REST API do so on node 0 at
#
#   rest = 1317 + LOCALNET_PORT_BASE
#
# LOCALNET_PORT_BASE shifts every family by the same amount and defaults to 0,
# so a caller that sets nothing gets exactly the ports these scripts have always
# used. A host that already carries a node on the default ports runs its drills
# on an unused band instead of editing scripts:
#
#   LOCALNET_PORT_BASE=3000 make localnet-smoke     # 29657, 29656, 12090 ...
#
# A base of 4810 or less keeps every port this tooling binds, the restore band
# included, below the Linux ephemeral range (32768 and up), where an outbound
# connection can briefly hold a port. CometBFT's pprof listener, localhost:6060
# in every generated config.toml, is outside the scheme.
#
# The export-restore drill starts a SECOND chain beside the first; its band sits
# a fixed distance above the ordinary one and moves with the same base, so
# within one run the two never collide (for up to ten nodes). Two localnets on
# DIFFERENT bases are another matter: keep bases at least 1000 apart from each
# other's restore band, or just run one localnet at a time, as the docs say.
#
# require_free_ports and require_ports_free are the other half (#155): a script
# that is about to bind ports refuses to start when one of them is already in
# use, naming the port. Nothing else arbitrates these ports, so a drill winning
# the race is as likely as a live node losing it. LOCALNET_SKIP_PORT_CHECK=1
# turns the check off; it exists for a network namespace or container where
# nothing else can be on these ports, and for nowhere else.

# An explicitly empty value is refused rather than read as 0: an empty base is a
# mistake, and 0 is the band a live node is most likely to be on.
LOCALNET_PORT_BASE="${LOCALNET_PORT_BASE-0}"
# The restored chain in export-restore-drill.sh: rpc/p2p one thousand above the
# ordinary band, grpc ten thousand above. 1000 would put the restored gRPC
# series at 10090, an ordinary-band value (node index 10), and 19090 is the band
# that drill has always used.
LOCALNET_RESTORE_OFFSET=1000
LOCALNET_RESTORE_GRPC_OFFSET=10000
# The highest port anything here binds is the restored chain's node-3 RPC,
# 27957 + base; a base past this would push it beyond 65535.
LOCALNET_PORT_BASE_MAX=37578

# A leading zero is refused rather than tolerated: bash reads 010 as octal 8 and
# aborts on 08, and either would move the band silently. The digit count is
# capped so that a value too large for bash arithmetic cannot wrap around to a
# plausible one.
#
# A bad value ends the script that sourced this file, whether or not it runs
# under set -e: a bare `return` would let a non-errexit script carry on with the
# bad value, which is the one thing this must not do. An interactive shell that
# sources this file directly gets its prompt back instead (one that sources a
# drill library, which sets -e first, still exits, which is also safe).
if [[ ! "$LOCALNET_PORT_BASE" =~ ^(0|[1-9][0-9]{0,4})$ ]]; then
  echo "LOCALNET_PORT_BASE must be a non-negative integer of at most five digits without leading zeros, got '$LOCALNET_PORT_BASE'" >&2
  [[ $- == *i* ]] && return 2
  exit 2
fi
if (( LOCALNET_PORT_BASE > LOCALNET_PORT_BASE_MAX )); then
  echo "LOCALNET_PORT_BASE=$LOCALNET_PORT_BASE is too large: the highest port these scripts bind (27957 + base) would exceed 65535; the maximum is $LOCALNET_PORT_BASE_MAX" >&2
  [[ $- == *i* ]] && return 2
  exit 2
fi
LOCALNET_PORT_GRACE_SECONDS="${LOCALNET_PORT_GRACE_SECONDS:-5}"
if [[ ! "$LOCALNET_PORT_GRACE_SECONDS" =~ ^(0|[1-9][0-9]{0,3})$ ]]; then
  echo "LOCALNET_PORT_GRACE_SECONDS must be a non-negative integer of at most four digits, got '$LOCALNET_PORT_GRACE_SECONDS'" >&2
  [[ $- == *i* ]] && return 2
  exit 2
fi

rpc_port()  { echo $((26657 + LOCALNET_PORT_BASE + $1 * 100)); }
p2p_port()  { echo $((26656 + LOCALNET_PORT_BASE + $1 * 100)); }
grpc_port() { echo $((9090 + LOCALNET_PORT_BASE + $1 * 100)); }
rest_port() { echo $((1317 + LOCALNET_PORT_BASE)); }
restore_rpc_port()  { echo $((26657 + LOCALNET_RESTORE_OFFSET + LOCALNET_PORT_BASE + $1 * 100)); }
restore_p2p_port()  { echo $((26656 + LOCALNET_RESTORE_OFFSET + LOCALNET_PORT_BASE + $1 * 100)); }
restore_grpc_port() { echo $((9090 + LOCALNET_RESTORE_GRPC_OFFSET + LOCALNET_PORT_BASE + $1 * 100)); }

# port_answers_on_loopback <port> — succeeds when a TCP connect to
# 127.0.0.1:<port> succeeds. Sees a loopback listener whoever owns it. The
# connect runs in the background and is given one second: a listener whose
# accept queue is full drops the SYN, and a foreground connect would then wait
# minutes. A connect that neither succeeds nor fails in time reads as "no
# answer"; lsof still covers the caller's own processes. Overridable, so the
# fast suite can exercise the guard with stubs alone.
port_answers_on_loopback() {
  local port="$1" pid i
  ( exec 3<>"/dev/tcp/127.0.0.1/$port" ) 2>/dev/null &
  pid=$!
  for (( i = 0; i < 10; i++ )); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null
    return 1
  fi
  wait "$pid"
}

# port_in_use <port> — succeeds when something is listening on the port.
#
# Two inspectors, because each misses what the other sees. lsof lists LISTEN
# sockets, but as an ordinary user it sees only that user's processes, so a node
# running under a service account is invisible to it. The loopback connect sees
# any listener on 127.0.0.1 whoever owns it, but only there (P2P binds every
# interface, and its loopback side still answers). A port is busy when either
# says so.
port_in_use() {
  local port="$1"
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then return 0; fi
  port_answers_on_loopback "$port"
}

# require_ports_free <port...> — refuse (return 1) when any of the ports is in
# use, printing every busy port rather than the first.
#
# A port a node released a moment ago can still show as listening while the
# process exits, so a busy port is re-checked for about
# LOCALNET_PORT_GRACE_SECONDS (default 5) before it counts as a collision: a
# live node stays busy for the whole grace and is refused; a node mid-exit frees
# up and is not. The grace never delays a start on free ports.
require_ports_free() {
  local port busy=() deadline
  # Nothing to check is refused, not passed: an empty list means the caller
  # computed no ports, and "no ports were busy" would be true for the wrong
  # reason. The same goes for a port that is not a plain number: a leading
  # zero would reach the arithmetic as octal, or not at all.
  if (( $# == 0 )); then
    echo "refusing to start: no ports to check (the node count or the port arithmetic produced nothing)" >&2
    return 1
  fi
  for port in "$@"; do
    if [[ ! "$port" =~ ^[1-9][0-9]{0,4}$ ]] || (( port > 65535 )); then
      echo "refusing to start: port '$port' is not within 1-65535 (too many nodes, or too large a LOCALNET_PORT_BASE, for this script)" >&2
      return 1
    fi
  done
  if [[ "${LOCALNET_SKIP_PORT_CHECK:-0}" == "1" ]]; then
    echo "LOCALNET_SKIP_PORT_CHECK=1: not checking whether ports $* are free" >&2
    return 0
  fi
  # An inspector that is ABSENT must not read as "every port is free". lsof
  # returning non-zero is how a free port looks, and a missing lsof returns
  # non-zero for every port — so without this the guard would report a clean
  # bill of health on exactly the machine where it could not look.
  if ! command -v lsof >/dev/null 2>&1; then
    echo "refusing to start: lsof is required to check that the localnet ports are free" >&2
    echo "  without it this guard cannot distinguish a free port from an uninspectable one," >&2
    echo "  and a collision would hit a live node. Install lsof, or, inside a network" >&2
    echo "  namespace or container where nothing else can be on these ports, set" >&2
    echo "  LOCALNET_SKIP_PORT_CHECK=1." >&2
    return 1
  fi
  deadline=$((SECONDS + LOCALNET_PORT_GRACE_SECONDS))
  while :; do
    busy=()
    for port in "$@"; do
      port_in_use "$port" && busy+=("$port")
    done
    (( ${#busy[@]} == 0 )) && return 0
    (( SECONDS >= deadline )) && break
    sleep 0.5
  done
  echo "refusing to start: ports already in use: ${busy[*]}" >&2
  echo "  another localnet or a live node is running. Stop it, or move this localnet to an" >&2
  echo "  unused band with a different LOCALNET_PORT_BASE (3000, say), or run it inside a" >&2
  echo "  network namespace. See website/docs/development/localnet-drills.md." >&2
  return 1
}

# require_free_ports [node_count] — the ordinary band for nodes 0..count-1.
# Reads NODE_COUNT (default 4) when no count is given. A drill that adds nodes
# after init passes the count it will END with, because init.sh only checks
# the nodes it creates. A count that is not a positive integer is refused,
# because it would build an empty list and pass for the wrong reason.
require_free_ports() {
  local count="${1:-${NODE_COUNT:-4}}" n ports=()
  if [[ ! "$count" =~ ^[1-9][0-9]{0,3}$ ]]; then
    echo "refusing to start: node count must be a positive integer, got '$count'" >&2
    return 1
  fi
  for ((n = 0; n < count; n++)); do
    ports+=("$(rpc_port "$n")" "$(p2p_port "$n")" "$(grpc_port "$n")")
  done
  require_ports_free "${ports[@]}"
}
