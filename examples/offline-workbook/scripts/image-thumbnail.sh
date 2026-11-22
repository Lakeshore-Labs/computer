set -eo pipefail
convert /source/harbor.webp -thumbnail 360x240 -quality 84 harbor-thumb.jpg
identify /source/harbor.webp harbor-thumb.jpg
