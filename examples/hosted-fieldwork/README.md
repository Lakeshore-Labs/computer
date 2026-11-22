# A field guide to free hosted MCPs

[Open the offline survey](sample/fieldwork.html) to search 94 recorded Computer cases. It includes the four final cases that missed a check. Earlier failed runs of the same cases are summarized under [Counting notes](#counting-notes) and are not in these counts. The survey covers three November 22, 2026 cohorts. Of 94 final cases, 90 passed across 86 distinct endpoint URLs. Each run used an isolated workspace and public queries without a user account. Access policies and timetables can change.

![A journey to the Kunstmuseum Basel in the fieldwork reader](sample/desktop.png)

The first cohort passed 56 of 59 cases across 55 unique endpoints. The second passed 25 of 26 cases across 23 additional endpoints. Two of its cases combine services already counted. The third cohort passed nine final cases across eight new endpoint URLs. Its three caseyjhand.com endpoints share an operator, so the nine cases cover eight endpoint URLs and six operator groups. Initial iterations with incorrect argument names, filters or unsupported checks are summarized in the counting notes, not in the final case counts. The searchable survey includes each task, actual endpoint, timestamp and individual predicate. Some predicates check for a string or a source domain. Others compare independently obtained measurements. Open a run to see which test was applied. This survey samples the separate [MCP registry census](../registry-census).

## Read a result across two wildlife catalogs

Two third-party MCP endpoints returned 398 records in the filtered iNaturalist query and 394 in its GBIF copy. Independent calls to each underlying REST service matched its own MCP result. The filters restrict the iNaturalist query to the research-grade records and licenses accepted by the export. The counts concern records in the chosen query. They cannot quantify the size of the animal population. See the [capture and predicates](sample/wildlife-catalogs.json) and [phone view](sample/phone-lynx.png).

The dictionary case changed its matching condition after a miss. The first check required the dictionary's example sentence to appear verbatim in a Tatoeba search, and it did not. The final check compares the pinyin with Wiktionary and requires at least 50 Mandarin Tatoeba sentences with the term and one with a translation. It does not show where the example sentence came from.

## Three recipes to run

| Job | What Computer did | What the second run checked |
| --- | --- | --- |
| [Take a train to see Holbein](sample/story-holbein-basel-trip/transcript.txt) | Called Wikioo for a painting and its museum, read a Wikidata collection record over HTTPS and called Swiss public transport for a connection. | Both records identified Basel. Wikioo gave 1521 and Wikidata gave 1520. The later Zürich HB departure at 23:08 reached Basel, Kunstmuseum at 00:29, matching the quoted 81 minutes. No ticket was bought. |
| [Plan a Warsaw observing night](sample/story-warsaw-sky-night/transcript.txt) | Called two hosted astronomy services and ran independent solar and lunar approximations inside WASI Python. | The provider reported sunset at 15:35 local and our own NOAA-style arithmetic gave 15:37 (nearest minute; see [earlier output](sample/story-warsaw-sky-night/earlier-truncated-1536/transcript.txt), which truncated it to 15:36). Provider and own moon estimates both rounded to 96%. The reported Geminid peak was December 14. |
| [Check a Van Gogh catalog entry](sample/still-met-van-gogh/transcript.txt) | Called a third-party artwork resolver and independently fetched the Metropolitan Museum of Art’s public record for object 436535. | All six fields agreed, covering title, artist, date, medium, required credit and the public-domain flag. |

Transcript lines are tagged `[provider]` for data returned by a hosted server and `[own arithmetic]` for numbers computed in WASI Python. The Warsaw provider calls its sun times US Naval Observatory data. We did not verify that.

Build the CLI and provision the WASI Python runtime as described in the [project README](../../README.md). From the repository root, run one of the listed case IDs. The output directory must be new.

```sh
python3 examples/hosted-fieldwork/replay.py story-holbein-basel-trip \
  --computer ./bin/computer --python-dir ./bin/python-wasi-2 \
  --output ./bin/holbein-trip
```

The runner passes only each recipe’s declared MCP endpoints and HTTPS prefixes. A new temporary workspace holds intermediate data. It saves the fresh transcript, SHA-256 and individual check results to your chosen directory. Historical survey records and subsequent replay receipts have separate timestamps. Source content, response formatting and live transport connections may change.

The [phone view](sample/phone.png), [browser film](sample/fieldwork.webm) and [browser receipts](sample/browser.json) show the offline search and failed-check filter. Rebuild the offline page with `python3 examples/hosted-fieldwork/build.py`. With Playwright Core and Chrome available, `node examples/hosted-fieldwork/record.cjs examples/hosted-fieldwork/sample/fieldwork.html /new/capture-directory` exercises both viewport sizes.

## Counting notes

Run `python3 -I examples/hosted-fieldwork/check_counts.py` to recompute the counts from `sample/survey.json` and compare them with [sample/amendment-8.json](sample/amendment-8.json). The gallery uses the latest published transcript, timestamp and checks for each of the three replayable stories. The other rows retain their survey observations. The page stat now reads "final cases".

- **Final, not first-try.** 94 final cases, 90 passes, 86 endpoint URLs. The cohort-3 runner log has 26 runs for the 9 cases. Of the first runs, 1 passed and 8 failed. The 17 later runs include 8 passes. The failures were mostly our own script errors (argument names, `curl -G`, a Wikidata lookup), one comparison design (GBIF absence records), one blocked independent source (Art Institute API, replaced by Wikidata) and the Dengdeng condition above. The log is local, so the amendment file gives a case-by-case summary.
- **Same docs-server shape.** 36 of the 94 cases (33 of the 90 passes, so 33 of the 86 endpoint URLs) call a generated `query-docs-filesystem*` tool, usually after a `search-<site>-docs` call, on a documentation server. Three of them are among the four misses. The survey's 86 therefore overstates how varied the passing servers are. The list and predicate are in the amendment file, and `derive_docs_pattern.py` rebuilds it from local capture directories. This shows tool shape, not a platform: four rows are labelled Mintlify, and a Prefect result link resolved to a mintlify.dev host.
- **Operators.** Cohort 3 has six operator groups: caseyjhand.com (four cases, three URLs), Artnap, the Lviv city API, Sitesy, EP Votes and Dengdeng.
- **Warsaw replay.** The earlier and corrected outputs are both kept. The corrected run is 2026-11-22T22:38Z. The provider's "tonight" start moved from 22:48 to 23:38 between runs. The sunset and moon values did not change.

## Replay the nine cohort-3 cases

`cohort3/manifest.json` lists each case's servers, HTTPS prefixes, inputs, predicates and the few observed values a receipt keeps. `cohort3/scripts/` holds the scripts as run. [cohort3/receipts.json](cohort3/receipts.json) is one replay of all nine on 2026-11-22T22:38–22:41Z. All nine predicates passed, and receipts hold no full response text. These are live public read-only calls with a four-second pause between cases.

```sh
python3 examples/hosted-fieldwork/replay_manifest.py all \
  --computer ./bin/computer --python-dir ./bin/python-wasi-2 \
  --output ./bin/cohort3-replay
```

Pass one or more case IDs instead of `all` to replay fewer. The earlier [phone view](sample/phone.png), [desktop view](sample/desktop.png) and [browser film](sample/fieldwork.webm) were recorded before the label change and still show "attempted cases".
