set -eo pipefail
curl -fsS 'https://openaccess-api.clevelandart.org/api/artworks/125249' |
    jq -e '{data:(.data | select(.accession_number == "1947.209" and .share_license_status == "CC0") |
        {accession_number,title,creditline,share_license_status,creation_date,creators:[{description:.creators[0].description}]})}' > cleveland/record.json
curl -fsS 'https://openaccess-cdn.clevelandart.org/1947.209/1947.209_web.jpg' > cleveland/painting.jpg
curl -fsS 'https://media.getty.edu/iiif/manifest/53be857e-41e8-4198-b45d-2e0f52d3051b' |
    jq -e 'select(.rights | endswith("/publicdomain/zero/1.0/")) |
        {label,rights,metadata:[.metadata[] | select(.label.en[0] == "Artist/Maker" or .label.en[0] == "Accession Number")]}' > getty/record.json
curl -fsS 'https://data.getty.edu/museum/collection/object/c88b3df0-de91-4f5b-a9ef-7b2b9a6d8abb' |
    jq -e '.produced_by.timespan.identified_by[0].content' > getty/year.json
curl -fsS 'https://media.getty.edu/iiif/image/e5d29650-11f8-4897-9540-54a9dd65b04f/full/600,/0/default.jpg' > getty/painting.jpg
identify cleveland/painting.jpg getty/painting.jpg
