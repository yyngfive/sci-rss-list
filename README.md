# Sci-RSS-List

An offline catalog of official scholarly RSS, Atom, and RDF feeds for later import into FeedMeDaily or similar tools.

The canonical machine-readable file is [`data/feeds.json`](data/feeds.json). Publisher pages under [`publishers/`](publishers/) are generated from that JSON with [`tools/publishers`](tools/publishers/).


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
| PNAS | 37 | [publishers/pnas.md](publishers/pnas.md) |
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
- Include `source`, `method`, `status`, and short `notes` when a feed is protected or only source-documented.
- Run:

```powershell
go test ./...
go run .\tools\feedcheck.go
go run .\tools\publishers
```

Use `--force` to re-check every entry. 
