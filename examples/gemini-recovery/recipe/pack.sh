set -eu
printf '%s\n' 'LF-042' > /work/in-progress.txt
if [ ! -f /work/desk-open.txt ]; then
  sleep 30
fi
printf '%s\n' 'LF-042' > /work/packed.txt
