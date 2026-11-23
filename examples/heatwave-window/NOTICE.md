# Attribution and licensing

Checked 22 Nov 2026 against the official pages named below. Credit is given for access and provenance; it does not imply
that Copernicus, ECMWF, the European Commission or Google endorse this page.

**Dataset.** ERA5 hourly data on single levels from 1940 to present, Copernicus Climate Change Service (C3S) Climate Data Store,
DOI [10.24381/cds.adbb2d47](https://doi.org/10.24381/cds.adbb2d47). The CDS catalogue entry
(`cds.climate.copernicus.eu/datasets/reanalysis-era5-single-levels`) lists the licence as **CC-BY-4.0**
(<https://spdx.org/licenses/CC-BY-4.0>) and the provider as ECMWF. The CDS page gives the DOI but no ready-made citation string.

**Cloud copy.** Google Research ARCO-ERA5, <https://github.com/google-research/arco-era5>, bucket
`gs://gcp-public-data-arco-era5` (store `ar/full_37-1h-0p25deg-chunk-1.zarr-v3`). The repository's *code* is Apache-2.0
("This is not an official Google product"); the *data* terms are the Copernicus licence, per its FAQ. Its README asks users to cite:

* Carver, R. W. and Merose, A. (2023): ARCO-ERA5: An Analysis-Ready Cloud-Optimized Reanalysis Dataset. 22nd Conf. on AI for
  Environmental Science, Denver, CO, AMS, 4A.1.
* Hersbach, H. et al. (2017): Complete ERA5: Fifth generation of ECMWF atmospheric reanalyses of the global climate. C3S Data Store (CDS).
  (The README text; the catalogue DOI above is for the 2023 single-levels dataset, so cite the form your venue expects and add your access date.)

**Required wording (copied from the ARCO README).** "The results contain modified Copernicus Climate Change Service information 2022.
Neither the European Commission nor ECMWF is responsible for any use that may be made of the Copernicus information or data it contains."
The year follows the README text; the Copernicus licence's own wording and year should be checked before commercial or
other redistribution (the README links its licence PDF at `cds.climate.copernicus.eu/api/v2/terms/static/…`, which returned
HTTP 404 when checked, so the licence PDF was **not** read for this package).

**Computer CLI.** <https://github.com/Lakeshore-Labs/computer>, Apache-2.0.

**This package's own files** (scripts, template, page) carry no separate licence grant yet; the owner of the parent PR should
add one. Derived values in `expected/` stay subject to the CC-BY-4.0 attribution above.
