# Agent Notes

## Boundaries

- Do not change Go validator behavior, request headers, user agents, or WebView2 flow unless the user explicitly asks for that code change.
- If the user says README should follow remote/GitHub, use `origin/main:README.md` as the source of truth and apply only the explicitly requested edits.
- Manual RSS Lookup and `--force` usage text in the remote README are human-facing documentation; do not move or delete them unless the user explicitly asks.

## Catalog Conventions

- Open coverage and ranking signals, including OpenAlex Sources and official publisher journal lists, may be used only to decide what to include.
- Do not copy proprietary ranking tables or store impact scores.
- Publisher grouping follows the catalog's canonical publisher/host choice.
- Nature-hosted `www.nature.com` feeds, including Nature Reviews, Communications, and npj journals, are grouped under Nature.
- Elsevier-owned journal feeds from ScienceDirect use publisher `Elsevier/ScienceDirect`; Cell Press feeds from `www.cell.com` stay under `Cell Press`; Lancet journal feeds from `www.thelancet.com` stay under `The Lancet`.
- For newly proposed feeds that are official/documented but not yet locally verified, prefer `status: "protected"` when the normal validator is likely to need WebView2; `feedcheck` will mark them `verified` after XML capture.
- Use `go run .\tools\addfeeds --dry-run <new-feeds.json>` before appending batches when practical. The addfeeds tool accepts one feed object or an array and checks duplicate canonical URLs before writing.

## Feedcheck Behavior

- `go run .\tools\feedcheck.go` validates only entries whose `data/feeds.json` status is not `verified`.
- Without `--force`, `verified` entries must be skipped without any network request.
- The WebView2 human verification window is opened for feeds already marked `protected`, `source_documented` feeds whose ordinary HTTP check returns a protected/challenge response, and `verified` feeds that return protected/challenge during `--force`.
- With `--force`, every feed is rechecked; if an otherwise `verified` feed is protected, it should go through WebView2 human verification instead of failing immediately.
- If some feeds fail ordinary HTTP checks during `--force`, still run WebView2 for any protected feeds already queued, then report all remaining errors at the end.
- The manual verification UI lives in `cmd/feedmedaily-protected-verifier`; `tools/feedcheck.go` only invokes it when the rules above queue protected feeds.
- When WebView2 captures feed XML, `feedcheck` updates that entry to `verified` and regenerates publisher pages.
- `feedcheck` also updates the README Publisher Index after regenerating publisher pages. The README `Feeds` column is `m/n`, where `m` is verified feeds and `n` is total feeds for that publisher.
- The persistent WebView2 profile is stored under `.feedcheck-webview2/`, which is ignored by Git.
- If `cmd/feedmedaily-protected-verifier` changes, rebuild `feedcheck.exe` with `go build -o feedcheck.exe .\tools\feedcheck.go` so the user-facing binary matches source.
- WebView2 can show XML while `GetContent` fails with `0x800700e8` (seen with ChemRxiv). Do not immediately skip on that error; fall back to the visible-page XML probe and let the existing wait timeout decide.

## Manual RSS Lookup

Use publisher patterns only after checking the official journal or RSS page. Then add the feed to `data/feeds.json` and run the validator.

| Publisher | Manual source or pattern |
| --- | --- |
| Nature | Open the journal page and use its RSS link. Most Nature-hosted journals also follow `https://www.nature.com/{journal-code}.rss`, for example `https://www.nature.com/nmeth.rss`. |
| Science/AAAS | Use the official RSS page: `https://www.science.org/content/page/email-alerts-and-rss-feeds`. A journal feed usually follows `https://www.science.org/action/showFeed?type=etoc&feed=rss&jc={journal-code}`. |
| ACS | Use the journal RSS link from the ACS follow/RSS page. Feeds follow `https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc={journal-code}`; the code must match the official journal page, for example `jacsat`. |
| Wiley | Open the Wiley Online Library journal page and use the RSS icon. Feeds usually follow `https://onlinelibrary.wiley.com/feed/{online-issn}/most-recent`. |
| Elsevier/ScienceDirect | Open the journal page, then `Articles & Issues`, then `RSS`; ScienceDirect feeds commonly use `https://rss.sciencedirect.com/publication/science/{issn}`. |
| Cell Press | Use the journal page or current issue feed; many feeds follow `https://www.cell.com/{journal}/current.rss`, including `matter`, `joule`, `med`, and `iscience`. Trends journals use paths like `https://www.cell.com/trends/chemistry/current.rss`. |
| The Lancet | Use Lancet current issue feeds such as `https://www.thelancet.com/rssfeed/{journal-code}_current.xml`; keep these under `The Lancet`, not `Elsevier/ScienceDirect`. |
| JAMA Network | Use the official RSS index `https://jamanetwork.com/pages/rss/`; keep the feed URLs exactly as listed there. |
| ChemRxiv | Use the latest feed `https://chemrxiv.org/action/showFeed?type=latest&format=rss`. It may require WebView2/visible XML capture even when manual browser access displays XML. |
| PNAS | Use the official RSS page: `https://www.pnas.org/about/rss`. |
| bioRxiv/medRxiv | Use the official alerts/RSS pages: `https://www.biorxiv.org/alertsrss` and `https://www.medrxiv.org/alertsrss`; subject XML feeds use the `connect.*rxiv.org/*_xml.php?subject={subject}` pattern. |
| BMJ | Use the journal page RSS link. Many BMJ specialty journals use `https://{journal}.bmj.com/rss/current.xml`; The BMJ currently redirects from `https://www.bmj.com/rss/recent.xml` to `http://feeds.bmj.com/bmj/recent`. |
| Cambridge Core | Use only the RSS alternate link exposed on the official journal page; do not guess `core/rss/product/id/...` identifiers. |
