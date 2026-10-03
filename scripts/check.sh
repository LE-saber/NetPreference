#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist/logs
if [ -n "$(gofmt -l cmd internal)" ]; then echo 'gofmt drift'; exit 1; fi
go version | tee dist/logs/compiler.txt
go test -race -coverprofile=dist/coverage.out -v ./... | tee dist/logs/go-tests.txt
go vet ./... 2>&1 | tee dist/logs/go-vet.txt
go test ./internal/netpref -run '^$' -fuzz '^FuzzDNSWire$' -fuzztime=3s -parallel=1 | tee dist/logs/fuzz-dns.txt
go test ./internal/netpref -run '^$' -fuzz '^FuzzUCITokens$' -fuzztime=3s -parallel=1 | tee dist/logs/fuzz-uci.txt
go test ./internal/netpref -run '^$' -fuzz '^FuzzDomainSetMatchesLinearReference$' -fuzztime=30s -parallel=2 | tee dist/logs/fuzz-domains.txt
python3 scripts/build_ipk.py | tee dist/logs/build.txt
python3 tests/test_package.py 2>&1 | tee dist/logs/package.txt
python3 tests/test_workflows.py 2>&1 | tee dist/logs/workflows.txt
node tests/library_model.js | tee dist/logs/library-model.txt
node tests/luci_contract.js | tee dist/logs/luci-contract.txt
NETPREFERENCE_UI_CONFIG="$(pwd)/dist/ux-generated.uci" go test ./internal/netpref -run TestGeneratedUILibraryPreference -count=1 -v | tee dist/logs/ui-backend-roundtrip.txt
python3 -m py_compile tests/*.py scripts/*.py
for script in files/etc/init.d/netpreference files/usr/libexec/rpcd/netpreference packaging/*; do sh -n "$script"; done
printf '%s\n' 'PASS: local test suite. Kernel and rootfs tests are separate.'
