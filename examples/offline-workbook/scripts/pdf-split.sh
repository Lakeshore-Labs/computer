set -eo pipefail
python3 - <<'PY'
from pypdf import PdfReader, PdfWriter

r = PdfReader("/source/operations.pdf")
w = PdfWriter()
w.add_page(r.pages[1])
w.write("route-detail.pdf")
print("Selected page 2 of", len(r.pages))
PY
pdftotext route-detail.pdf -
pdftoppm -jpeg -r 72 route-detail.pdf detail
identify detail-1.jpg
