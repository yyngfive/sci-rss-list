# Agent Notes

## Boundaries

- Do not change Go validator behavior, request headers, user agents, or WebView2 flow unless the user explicitly asks for that code change.
- If the user says README should follow remote/GitHub, use `origin/main:README.md` as the source of truth and apply only the explicitly requested edits.
- Manual RSS Lookup and `--force` usage text in the remote README are human-facing documentation; do not move or delete them unless the user explicitly asks.

## Catalog Conventions

- Identity fields are separate from the legacy `journal` subscription label. `feed_scope` is required and is one of `single_journal`, `multi_journal`, `subject_collection`, `platform_collection`.
- `single_journal` is the only scope that carries a non-null `canonical_journal`; the other scopes keep `canonical_journal: null` so an aggregate never impersonates one journal.
- `feed_type` and `feed_name` are set together or left null. A plain single-journal feed with no distinguished type uses null for both; never invent `current_issue` or `recent` from the URL shape.
- `collection` is only for `subject_collection` entries and must carry `platform`, a stable `id`, and the official `name`.
- `issn_l` stores only an ISSN-L that the ISSN Portal record and the Crossref journal record both confirm under the same title; leave it null when unconfirmed rather than guessing.
- Do not strip parentheses, colons, or subtitles from labels by rule: `Physica A: Statistical Mechanics and its Applications`, `Light: Science & Applications`, and `JNCI: Journal of the National Cancer Institute` are whole titles, while `Physical Review B: Semiconductors I: bulk` is a section feed whose journal is `Physical Review B`.
- Use `go run .\tools\migrateidentity -dry-run` to re-derive identity fields from the publisher rules in that tool, and `go run .\tools\issnlookup -apply` to fill or propagate `issn_l`. New entries should carry the identity fields from the start; the tools are for batches and repairs.
- Open coverage and ranking signals, including OpenAlex Sources and official publisher journal lists, may be used only to decide what to include. Identifier lookups read identifier fields only: Crossref, Wikidata and the publisher's own journal page may propose an ISSN, but the ISSN Portal record and the Crossref journal record must confirm the ISSN-L under the catalog title before it is stored.
- Do not copy proprietary ranking tables or store impact scores.
- Publisher grouping follows the catalog's canonical publisher/host choice.
- Nature-hosted `www.nature.com` feeds, including Nature Reviews, Communications, and npj journals, are grouped under Nature.
- Elsevier-owned journal feeds from ScienceDirect use publisher `Elsevier/ScienceDirect`; Cell Press feeds from `www.cell.com` stay under `Cell Press`; Lancet journal feeds from `www.thelancet.com` stay under `The Lancet`.
- For newly proposed feeds that are official/documented but not yet locally verified, prefer `status: "protected"` when the normal validator is likely to need WebView2; `feedcheck` will mark them `verified` after XML capture.
- `requires_proxy: true` marks feeds whose host is unreachable without a proxy (DNS poisoning plus SNI blocking, seen with `journals.sagepub.com`); omit the field for directly reachable hosts. Consumers such as FeedMeDaily must route these feeds through a proxy, and local validation runs need `HTTP_PROXY`/`HTTPS_PROXY` set.
- Use `go run .\tools\addfeeds --dry-run <new-feeds.json>` before appending batches when practical. The addfeeds tool accepts one feed object or an array and checks duplicate canonical URLs before writing.

## Feedcheck Behavior

- `go run .\tools\feedcheck.go` validates only entries whose `data/feeds.json` status is not `verified`.
- Without `--force`, `verified` entries must be skipped without any network request.
- The WebView2 human verification window is opened for feeds already marked `protected`, `source_documented` feeds whose ordinary HTTP check returns a protected/challenge response, and `verified` feeds that return protected/challenge during `--force`.
- The protected/challenge fingerprint table in `isProtected` (tools/feedcheck.go) is the shared baseline for challenge detection. It covers Cloudflare/Akamai/PerimeterX hints plus bot-manager fingerprints `perfdrive`, `radware`, `bot manager`, `incapsula`, `imperva`, `distil`, and `server:[rdwr]` (Radware's edge header; its block body carries no recognizable text). FeedMeDaily's own challenge detection must mirror this table so both front-ends queue the same feeds for human verification.
- With `--force`, every feed is rechecked; if an otherwise `verified` feed is protected, it should go through WebView2 human verification instead of failing immediately.
- If some feeds fail ordinary HTTP checks during `--force`, still run WebView2 for any protected feeds already queued, then report all remaining errors at the end.
- The manual verification UI lives in `cmd/feedmedaily-protected-verifier`; `tools/feedcheck.go` only invokes it when the rules above queue protected feeds.
- When WebView2 captures feed XML, `feedcheck` updates that entry to `verified` and regenerates publisher pages.
- `feedcheck` also updates the README Publisher Index after regenerating publisher pages. The README `Feeds` column is `m/n`, where `m` is verified feeds and `n` is total feeds for that publisher.
- The persistent WebView2 profile is stored under `.feedcheck-webview2/`, which is ignored by Git.
- If `cmd/feedmedaily-protected-verifier` or any Go source it depends on (including `internal/catalog` field changes) changes, rebuild `feedcheck.exe` with `go build -o feedcheck.exe .\tools\feedcheck.go` so the user-facing binary matches source; a stale binary fails catalog validation with `unknown field ...`.
- WebView2 can show XML while `GetContent` fails with `0x800700e8` (seen with ChemRxiv). Do not immediately skip on that error; fall back to the visible-page XML probe and let the existing wait timeout decide.

## Manual RSS Lookup

Use publisher patterns only after checking the official journal or RSS page. Then add the feed to `data/feeds.json` and run the validator.

| Publisher | Manual source or pattern |
| --- | --- |
| Nature | Open the journal page and use its RSS link. Most Nature-hosted journals also follow `https://www.nature.com/{journal-code}.rss`, for example `https://www.nature.com/nmeth.rss`. |
| Science/AAAS | Use the official RSS page: `https://www.science.org/content/page/email-alerts-and-rss-feeds`. A journal feed usually follows `https://www.science.org/action/showFeed?type=etoc&feed=rss&jc={journal-code}`. |
| ACS | Use the journal RSS link from the ACS follow/RSS page. ASAP feeds follow `https://pubs.acs.org/rss/{journal-code}/asap.xml`, and current-issue feeds follow `https://pubs.acs.org/rss/{journal-code}/currentIssue.xml`; the code must match the official RSS page, for example `jacsat`. When both feed types are cataloged, suffix their journal labels with `(ASAP)` and `(Current Issue)`. |
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
| Optica | Use the official RSS/alerts index: `https://opg.optica.org/toc_alerts_subscribe.cfm`. Journal feeds follow `https://opg.optica.org/rss/{feed-code}_feed.xml`; the feed code can differ from the journal page code (Optics Express uses `opex` while its page is `/oe`). The `.cfm` pages block ordinary clients, but `/rss/*_feed.xml` responds to the validator. |
| APS | Use the official RSS page: `https://journals.aps.org/feeds`. Feeds live on `https://feeds.aps.org/rss/` as `recent/{code}.xml`, `accepted/{code}.xml`, `{code}suggestions.xml`, and `tocsec/{JOURNAL}-{Section}.xml`. Suffix journal labels with `(Recently Published)`, `(Recently Accepted)`, or `(Editors' Suggestions)`, and use `Journal: Section` for topic feeds. Feed codes `prstab` and `prstper` differ from the journal pages `/prab/` and `/prper/`; the APS Open Science feed code is `apsos`. |
