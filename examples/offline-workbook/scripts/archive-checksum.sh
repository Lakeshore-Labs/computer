set -eo pipefail
tar -czf route-inputs.tar.gz -C /source day.csv routes.yaml
mkdir -p restored
tar -xzf route-inputs.tar.gz -C restored
diff /source/day.csv restored/day.csv
sha256sum restored/day.csv
tar -tzf route-inputs.tar.gz
