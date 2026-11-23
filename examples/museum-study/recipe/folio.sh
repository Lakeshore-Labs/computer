set -eo pipefail
jq -r '.data | [.title,.creators[0].description,.creation_date,.creditline] | @tsv' /collection/cleveland/record.json > cleveland.tsv
jq -r --slurpfile year /collection/getty/year.json 'def field($label): first(.metadata[] | select(.label.en[0] == $label) | .value.en[0]); [.label.en[0],field("Artist/Maker"),$year[0],("The J. Paul Getty Museum, " + field("Accession Number"))] | @tsv' /collection/getty/record.json > getty.tsv
for museum in cleveland getty; do
    base64 "$museum.jpg" | tr -d '\n' > "$museum.b64"
done
awk -f /recipe/folio.awk /recipe/booklet.html > study.html
cat cleveland.tsv getty.tsv
