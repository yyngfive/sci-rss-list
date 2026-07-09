# Sci-RSS-List

An offline catalog of official scholarly RSS, Atom, and RDF feeds for later import into FeedMeDaily or similar tools.

The canonical machine-readable file is [`data/feeds.json`](data/feeds.json). Publisher pages under [`publishers/`](publishers/) are generated from that JSON with [`tools/publishers`](tools/publishers/).


## Publisher Index

| Publisher | Feeds | Page |
| --- | ---: | --- |
| ACS | 65/65 | [publishers/acs.md](publishers/acs.md) |
| ASCB | 1/1 | [publishers/ascb.md](publishers/ascb.md) |
| BMC/SpringerLink | 16/16 | [publishers/bmc-springerlink.md](publishers/bmc-springerlink.md) |
| BMJ | 8/8 | [publishers/bmj.md](publishers/bmj.md) |
| Cambridge Core | 3/3 | [publishers/cambridge-core.md](publishers/cambridge-core.md) |
| Cell Press | 19/19 | [publishers/cell-press.md](publishers/cell-press.md) |
| ChemRxiv | 1/1 | [publishers/chemrxiv.md](publishers/chemrxiv.md) |
| Cold Spring Harbor Laboratory Press | 3/3 | [publishers/cold-spring-harbor-laboratory-press.md](publishers/cold-spring-harbor-laboratory-press.md) |
| Elsevier/ScienceDirect | 46/46 | [publishers/elsevier-sciencedirect.md](publishers/elsevier-sciencedirect.md) |
| Frontiers | 12/12 | [publishers/frontiers.md](publishers/frontiers.md) |
| IEEE/ACM | 8/8 | [publishers/ieee-acm.md](publishers/ieee-acm.md) |
| JAMA Network | 13/13 | [publishers/jama-network.md](publishers/jama-network.md) |
| Life Science Alliance | 1/1 | [publishers/life-science-alliance.md](publishers/life-science-alliance.md) |
| MDPI | 12/12 | [publishers/mdpi.md](publishers/mdpi.md) |
| NEJM Group | 1/1 | [publishers/nejm-group.md](publishers/nejm-group.md) |
| Nature | 141/141 | [publishers/nature.md](publishers/nature.md) |
| Oxford Academic | 9/9 | [publishers/oxford-academic.md](publishers/oxford-academic.md) |
| PLOS | 16/16 | [publishers/plos.md](publishers/plos.md) |
| PNAS | 37/37 | [publishers/pnas.md](publishers/pnas.md) |
| SAGE | 0/8 | [publishers/sage.md](publishers/sage.md) |
| Science/AAAS | 6/6 | [publishers/science-aaas.md](publishers/science-aaas.md) |
| Scientific American | 1/1 | [publishers/scientific-american.md](publishers/scientific-american.md) |
| Taylor & Francis | 8/8 | [publishers/taylor-francis.md](publishers/taylor-francis.md) |
| The Lancet | 9/9 | [publishers/the-lancet.md](publishers/the-lancet.md) |
| Wiley | 47/47 | [publishers/wiley.md](publishers/wiley.md) |
| bioRxiv/medRxiv | 77/77 | [publishers/biorxiv-medrxiv.md](publishers/biorxiv-medrxiv.md) |
| eLife | 1/1 | [publishers/elife.md](publishers/elife.md) |

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
