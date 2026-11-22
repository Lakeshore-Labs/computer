set -eo pipefail
ls /usr/bin | sort > commands.txt
wc -l commands.txt
cat commands.txt
