#!/usr/bin/env bash
# Self-contained API-surface smoke. Spins up a throwaway four-node localnet with the
# REST API + Swagger enabled and a seeded ReservedConsensusAddress fixture, runs both
# smoke scripts against it, then tears the localnet down (on any exit). Use this to run
# the live-node smoke without the manual init/enable/start/teardown dance:
#
#   ./scripts/smoke-local.sh        # or: make api-smoke
#
# Uses node 0's localnet ports (RPC, gRPC and REST from scripts/localnet/lib/ports.sh, so
# LOCALNET_PORT_BASE applies) on a dedicated home
# ($TWILIGHT_LOCALNET_HOME, default /tmp/twilight-smoke-local). It will conflict with
# another localnet already bound to those ports — stop that first.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
. "$ROOT/scripts/localnet/lib/ports.sh"

export TWILIGHT_LOCALNET_HOME="${TWILIGHT_LOCALNET_HOME:-/tmp/twilight-smoke-local}"
export CHAIN_ID="${CHAIN_ID:-twilight-smoke-local-1}"
REST="http://localhost:$(rest_port)"

cleanup() { ./scripts/localnet/stop.sh >/dev/null 2>&1 || true; }
trap cleanup EXIT

die() { echo "ERROR: $*" >&2; exit 1; }

echo "== smoke-local: init four-node localnet ($TWILIGHT_LOCALNET_HOME) =="
./scripts/localnet/stop.sh >/dev/null 2>&1 || true
# Check the band here too, before the wipe: init.sh would refuse, but only after this
# script had already removed the home. REST is outside the band; it is enabled below.
require_free_ports || die "refusing to start"
require_ports_free "$(rest_port)" || die "refusing to start"
rm -rf "$TWILIGHT_LOCALNET_HOME"
./scripts/localnet/init.sh >/dev/null || die "localnet init failed"

echo "== seed ReservedConsensusAddress fixture =="
RES_HEX="$(./scripts/seed-reservation.sh -q)" || die "seed-reservation failed"
echo "   RESERVED_CONS_HEX=$RES_HEX"

echo "== enable REST API + Swagger on node0 =="
A="$TWILIGHT_LOCALNET_HOME/node0/config/app.toml"
sed -i.bak '/^\[api\]/,/^\[/ s/^enable = false/enable = true/; /^\[api\]/,/^\[/ s/^swagger = false/swagger = true/' "$A" \
  || die "could not edit app.toml"
sed -i.bak -E "/^\[api\]/,/^\[/ s#^address = \"tcp://([^:\"]+):1317\"#address = \"tcp://\\1:$(rest_port)\"#" "$A" || die "could not edit app.toml"
grep -q "^address = \"tcp://[^:\"]*:$(rest_port)\"" "$A" || die "app.toml [api] address was not rewritten to port $(rest_port)"
rm -f "$A.bak"

echo "== start localnet =="
./scripts/localnet/start.sh >/dev/null || die "localnet start failed"

echo "== wait for REST to warm =="
warm=0
for i in $(seq 1 60); do
  if [[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$REST/twilight/coreslot/v1/params" 2>/dev/null)" == "200" ]]; then
    echo "   warm after ${i}s"; warm=1; break
  fi
  sleep 1
done
[[ "$warm" -eq 1 ]] || die "REST did not become ready within 60s (check $TWILIGHT_LOCALNET_HOME/logs)"

rc=0
echo
echo "######## API SURFACE SMOKE ########"
RESERVED_CONS_HEX="$RES_HEX" BASE_REST="$REST" BASE_GRPC="localhost:$(grpc_port 0)" BASE_RPC="http://localhost:$(rpc_port 0)" \
  ./scripts/smoke-api-surface.sh || rc=1
echo
echo "######## SWAGGER SMOKE ########"
BASE_REST="$REST" ./scripts/smoke-swagger-api.sh || rc=1

echo
[[ "$rc" -eq 0 ]] && echo "== smoke-local: PASS ==" || echo "== smoke-local: FAIL =="
exit "$rc"
