set -eo pipefail
yq '.routes[] | select(.capacity > 60)' /source/routes.yaml
