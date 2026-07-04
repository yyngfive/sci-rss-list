# sci-rss-list

An offline catalog of official scholarly RSS, Atom, and RDF feeds for later import into FeedMeDaily or similar tools.

The canonical machine-readable file is [`data/feeds.json`](data/feeds.json). Publisher pages under [`publishers/`](publishers/) are generated from that JSON with [`tools/publishers`](tools/publishers/).

## Scope

This dataset contains 265 feeds across broad science, medicine, engineering, chemistry, biology, environment, computing, review, and preprint subject collections.

Open coverage and ranking signals, including OpenAlex Sources and official publisher journal lists, were used only to decide what to include. Scores, ranks, and proprietary metric tables are not stored or displayed here.

Publisher grouping follows the catalog's canonical publisher/host choice. Nature-hosted `www.nature.com` feeds, including Nature Reviews, Communications, and npj journals, are grouped under Nature.

## Publisher Index

| Publisher | Feeds | Page |
| --- | ---: | --- |
| ACS | 65 | [publishers/acs.md](publishers/acs.md) |
| bioRxiv/medRxiv | 77 | [publishers/biorxiv-medrxiv.md](publishers/biorxiv-medrxiv.md) |
| BMJ | 8 | [publishers/bmj.md](publishers/bmj.md) |
| Cambridge Core | 6 | [publishers/cambridge-core.md](publishers/cambridge-core.md) |
| Cell Press | 12 | [publishers/cell-press.md](publishers/cell-press.md) |
| Elsevier/ScienceDirect | 14 | [publishers/elsevier-sciencedirect.md](publishers/elsevier-sciencedirect.md) |
| IEEE/ACM | 8 | [publishers/ieee-acm.md](publishers/ieee-acm.md) |
| Nature | 48 | [publishers/nature.md](publishers/nature.md) |
| PNAS | 1 | [publishers/pnas.md](publishers/pnas.md) |
| Science/AAAS | 6 | [publishers/science-aaas.md](publishers/science-aaas.md) |
| Taylor & Francis | 8 | [publishers/taylor-francis.md](publishers/taylor-francis.md) |
| Wiley | 12 | [publishers/wiley.md](publishers/wiley.md) |

## Entry Format

Each `data/feeds.json` entry has:

```json
{
  "publisher": "Nature",
  "journal": "Nature Methods",
  "url": "https://www.nature.com/nmeth.rss",
  "subjects": ["biology", "methods"],
  "source": "https://www.nature.com/nmeth/",
  "method": "url_pattern",
  "status": "verified",
  "notes": ""
}
```

Allowed `method` values are `publisher_index`, `url_pattern`, and `manual`.

Allowed `status` values are:

- `verified`: feed URL has returned RSS, Atom, or RDF to the Go validator or WebView2 verifier.
- `protected`: generic HTTP clients receive a challenge/block page and WebView2 verification has not yet captured XML.
- `source_documented`: official source documents the feed, but live validation did not confirm XML.

## Contributing

- Prefer official publisher RSS pages, journal pages, or documented URL patterns.
- If a publisher exposes a complete official RSS index, include the full index rather than a sample.
- Do not copy proprietary ranking tables or store impact scores.
- Include `source`, `method`, `status`, and short `notes` when a feed is protected or only source-documented.
- Run:

```powershell
go test ./...
go run .\tools\feedcheck.go
go run .\tools\publishers
```

`feedcheck` validates feeds whose `data/feeds.json` status is not `verified`. `protected` feeds, and `source_documented` feeds that return a protection/challenge page to ordinary HTTP, are sent through WebView2. When WebView2 captures feed XML, `feedcheck` updates that entry to `verified` and regenerates publisher pages. Use `--force` to re-check every entry. The persistent WebView2 profile is stored under `.feedcheck-webview2/`, which is ignored by Git.
