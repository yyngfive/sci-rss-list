---
name: add-rss-feeds
description: Add official RSS feeds to the sci-rss-list repository. Use when the user asks to add, batch-add, expand, or curate publisher/journal RSS feeds in data/feeds.json, including publisher-focused additions such as PLOS, Frontiers, MDPI, SpringerLink/BMC, Oxford Academic, SAGE, ACS, Wiley, Nature, Elsevier, Cell Press, Lancet, BMJ, Cambridge Core, or similar catalog work.
---

# Add RSS Feeds

## Rules

- Read `AGENTS.md` first and obey its publisher grouping and feedcheck rules.
- Add only official/documented feeds. Check the official journal page, RSS page, or exposed alternate link before using a URL pattern.
- Use open coverage/ranking signals only to choose candidates. Do not copy proprietary ranking tables or store impact scores.
- Prefer a small curated batch over a huge import unless the user explicitly asks for all journals.
- Do not change validator behavior, headers, user agents, or WebView2 flow unless explicitly asked.

## Status Choice

- Use `verified` only when the feed has already been validated by `feedcheck` or there is current local evidence equivalent to the project validator.
- Use `source_documented` when the feed is official/documented and ordinary HTTP validation is expected to work.
- Use `protected` when the feed is official/documented but normal validation is likely to hit Cloudflare, login, challenge, timeout, or needs WebView2.

## Workflow

1. Inspect current coverage:

```powershell
rg -n "PublisherName|journal-slug|feed-host" data publishers README.md
$feeds = Get-Content data\feeds.json -Raw | ConvertFrom-Json
$feeds | Group-Object publisher | Sort-Object Count -Descending
```

2. Confirm candidate feed URLs from official sources.

Use `curl.exe -L -I <url>` or a small GET check for representative endpoints. If sandboxed network fails, retry with escalation rather than guessing.

3. Create a temporary additions JSON containing one feed object or an array. Use only these fields:

```json
{
  "publisher": "Publisher",
  "journal": "Journal",
  "url": "https://example.com/feed.rss",
  "subjects": ["subject"],
  "source": "https://example.com/journal",
  "method": "publisher_index",
  "status": "source_documented",
  "notes": "Official publisher RSS feed."
}
```

Allowed `method`: `publisher_index`, `url_pattern`, `manual`.

4. Validate before writing:

```powershell
go run .\tools\addfeeds --dry-run additions.json
```

5. Append with the project tool, then delete the temporary JSON:

```powershell
go run .\tools\addfeeds additions.json
go run .\tools\publishers
go test ./...
```

6. Final check:

```powershell
git -c safe.directory=D:/Codes/Projects/sci-rss-list status --short
git -c safe.directory=D:/Codes/Projects/sci-rss-list diff --stat
```

Mention any protected feeds that still need `feedcheck`/WebView2.

## Common Patterns

- SpringerLink/BMC: `https://link.springer.com/search.rss?facet-content-type=Article&facet-journal-id={id}`. Group BMC-hosted journals as `BMC/SpringerLink` when using SpringerLink feeds.
- PLOS: `https://journals.plos.org/{journal}/feed/rss`.
- Frontiers: `https://www.frontiersin.org/journals/{journal-slug}/rss`.
- MDPI: `https://www.mdpi.com/rss/journal/{journal-code}`. Often mark `protected` if ordinary clients receive 403.
- SAGE/Atypon: `https://journals.sagepub.com/action/showFeed?type=etoc&feed=rss&jc={journal-code}`. Often mark `protected` if ordinary clients time out or are challenged.
- Oxford Academic: prefer the RSS alternate link from the official issue page, commonly under `https://academic.oup.com/rss/site_.../advanceAccess_....xml`; do not invent ids.
