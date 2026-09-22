# Sci-RSS-List

An offline catalog of official scholarly RSS, Atom, and RDF feeds for later import into FeedMeDaily or similar tools.

The canonical machine-readable file is [`data/feeds.json`](data/feeds.json). Publisher pages under [`publishers/`](publishers/) are generated from that JSON with [`tools/publishers`](tools/publishers/).


## Publisher Index

| Publisher | Feeds | Page |
| --- | ---: | --- |
| AAS | 5/5 | [publishers/aas.md](publishers/aas.md) |
| ACS | 182/182 | [publishers/acs.md](publishers/acs.md) |
| AGU | 10/10 | [publishers/agu.md](publishers/agu.md) |
| AIP Publishing | 8/8 | [publishers/aip-publishing.md](publishers/aip-publishing.md) |
| APS | 106/106 | [publishers/aps.md](publishers/aps.md) |
| ASCB | 1/1 | [publishers/ascb.md](publishers/ascb.md) |
| BMC/SpringerLink | 27/27 | [publishers/bmc-springerlink.md](publishers/bmc-springerlink.md) |
| BMJ | 8/8 | [publishers/bmj.md](publishers/bmj.md) |
| Cambridge Core | 3/3 | [publishers/cambridge-core.md](publishers/cambridge-core.md) |
| Cell Press | 19/19 | [publishers/cell-press.md](publishers/cell-press.md) |
| ChemRxiv | 1/1 | [publishers/chemrxiv.md](publishers/chemrxiv.md) |
| Cold Spring Harbor Laboratory Press | 3/3 | [publishers/cold-spring-harbor-laboratory-press.md](publishers/cold-spring-harbor-laboratory-press.md) |
| Elsevier/ScienceDirect | 393/393 | [publishers/elsevier-sciencedirect.md](publishers/elsevier-sciencedirect.md) |
| Frontiers | 12/12 | [publishers/frontiers.md](publishers/frontiers.md) |
| IEEE/ACM | 8/8 | [publishers/ieee-acm.md](publishers/ieee-acm.md) |
| IOP Publishing | 17/17 | [publishers/iop-publishing.md](publishers/iop-publishing.md) |
| JAMA Network | 13/13 | [publishers/jama-network.md](publishers/jama-network.md) |
| Life Science Alliance | 1/1 | [publishers/life-science-alliance.md](publishers/life-science-alliance.md) |
| MDPI | 12/12 | [publishers/mdpi.md](publishers/mdpi.md) |
| NEJM Group | 1/1 | [publishers/nejm-group.md](publishers/nejm-group.md) |
| Nature | 141/141 | [publishers/nature.md](publishers/nature.md) |
| Optica | 19/19 | [publishers/optica.md](publishers/optica.md) |
| Oxford Academic | 9/9 | [publishers/oxford-academic.md](publishers/oxford-academic.md) |
| PLOS | 16/16 | [publishers/plos.md](publishers/plos.md) |
| PNAS | 37/37 | [publishers/pnas.md](publishers/pnas.md) |
| RSC | 55/55 | [publishers/rsc.md](publishers/rsc.md) |
| Research Square | 1/1 | [publishers/research-square.md](publishers/research-square.md) |
| SAGE | 8/8 | [publishers/sage.md](publishers/sage.md) |
| SIAM | 6/6 | [publishers/siam.md](publishers/siam.md) |
| Science/AAAS | 6/6 | [publishers/science-aaas.md](publishers/science-aaas.md) |
| Scientific American | 1/1 | [publishers/scientific-american.md](publishers/scientific-american.md) |
| Taylor & Francis | 8/8 | [publishers/taylor-francis.md](publishers/taylor-francis.md) |
| The Lancet | 9/9 | [publishers/the-lancet.md](publishers/the-lancet.md) |
| Wiley | 47/47 | [publishers/wiley.md](publishers/wiley.md) |
| arXiv | 20/20 | [publishers/arxiv.md](publishers/arxiv.md) |
| bioRxiv/medRxiv | 77/77 | [publishers/biorxiv-medrxiv.md](publishers/biorxiv-medrxiv.md) |
| eLife | 1/1 | [publishers/elife.md](publishers/elife.md) |

## Entry Format

Each `data/feeds.json` entry keeps the journal identity separate from what the feed publishes:

```json
{
  "publisher": "ACS",
  "journal": "ACS Nano (ASAP)",
  "canonical_journal": "ACS Nano",
  "issn_l": "1936-0851",
  "feed_scope": "single_journal",
  "feed_type": "asap",
  "feed_name": "ASAP",
  "collection": null,
  "url": "https://pubs.acs.org/rss/ancac3/asap.xml",
  "subjects": ["nanoscience", "materials"],
  "source": "https://pubs.acs.org/journal/ancac3",
  "method": "publisher_index",
  "status": "verified",
  "notes": "Official ACS Publications ASAP RSS feed from the publisher RSS index."
}
```

- `journal` stays the legacy subscription label older clients read, so it keeps suffixes such as `(ASAP)` and section wording.
- `canonical_journal` is the journal's official full title, shared by every feed of that journal, with no feed type, volume, or issue text. It is non-null only for `feed_scope: "single_journal"`.
- `issn_l` is the optional linking ISSN that ties the feeds of one journal together. Fill it only after the ISSN Portal record and the Crossref journal record agree on it under the same title; otherwise leave it `null`.
- `feed_scope` is required: `single_journal`, `multi_journal` (one feed spanning several journals), `subject_collection` (a platform's subject classification), or `platform_collection`.
- `feed_type` and `feed_name` are set together or left `null`. `feed_type` is a stable lowercase token such as `asap`, `current_issue`, `recently_published`, `recently_accepted`, `editors_suggestions`, `toc_section`, `subject_collection`, or `latest_preprints`; `feed_name` is what the reader shows for this feed. A plain single-journal feed has no type, so both stay `null`.
- `collection` is only for `subject_collection` entries and carries the `platform`, a stable `id`, and the official classification `name`.

Allowed `method` values are `publisher_index`, `url_pattern`, and `manual`.

Allowed `status` values are:

- `verified`: feed URL has returned RSS, Atom, or RDF to the Go validator or WebView2 verifier.
- `protected`: generic HTTP clients receive a challenge/block page and WebView2 verification has not yet captured XML.
- `source_documented`: official source documents the feed, but live validation did not confirm XML.

### Reading the catalog

1. `single_journal`: use `canonical_journal` for the journal, even when an item's own citation metadata carries volume or issue text.
2. `multi_journal`: prefer each item's own journal name; the feed label only covers what the item metadata is missing.
3. `subject_collection`: identify the classification with `collection`, and display it as platform plus `collection.name`. It is not a journal.
4. `platform_collection`: use the platform feed identity, and present an item's own journal name where the product wants it.
5. Do not infer any of this from publisher URL patterns; every current entry states `feed_scope` explicitly, and only an imported entry that predates the field falls back to limited compatibility logic.

Publisher pages under [`publishers/`](publishers/) show the `Feed Type` of each entry. `go run .\tools\migrateidentity` re-derives the identity fields for the whole catalog from the publisher rules in that tool, and `go run .\tools\issnlookup -apply` fills or shares `issn_l` for the journals that have more than one feed.

## Manual RSS Lookup

Use these publisher patterns only after checking the official journal or RSS page. Then add the feed to `data/feeds.json` and run the validator.

| Publisher | Manual source or pattern |
| --- | --- |
| Nature | Open the journal page and use its RSS link. Most Nature-hosted journals also follow `https://www.nature.com/{journal-code}.rss`, for example `https://www.nature.com/nmeth.rss`. |
| Cell Press | Open the journal page. Most feeds follow `https://www.cell.com/{journal}/current.rss`, for example `https://www.cell.com/chem/current.rss`. |
| Wiley | Open the Wiley Online Library journal page and use the RSS icon. Feeds usually follow `https://onlinelibrary.wiley.com/feed/{online-issn}/most-recent`. |
| Elsevier/ScienceDirect | Open the journal page, then `Articles & Issues`, then `RSS`; ScienceDirect feeds commonly use `https://rss.sciencedirect.com/publication/science/{issn}`. |
| BMJ | Use the journal page RSS link. Many BMJ specialty journals use `https://{journal}.bmj.com/rss/current.xml`; The BMJ currently redirects from `https://www.bmj.com/rss/recent.xml` to `http://feeds.bmj.com/bmj/recent`. |

## Contributing

- Prefer official publisher RSS pages, journal pages, or documented URL patterns.
- If a publisher exposes a complete official RSS index, include the full index rather than a sample.
- State `feed_scope` on every new entry, with `canonical_journal` for a single journal and `feed_type` plus `feed_name` when the feed is one of several for that journal.
- Include `source`, `method`, `status`, and short `notes` when a feed is protected or only source-documented.
- Run:

```powershell
go test ./...
go run .\tools\feedcheck.go
go run .\tools\publishers
```

Add `go run .\tools\issnlookup -apply` after introducing another feed for a journal whose ISSN-L is already known, or to resolve the journals still missing one.

Use `--force` to re-check every entry. 
