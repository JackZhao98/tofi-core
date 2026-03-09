---
name: web-fetch
description: Fetch and read any web page, extracting clean text content from URLs
version: "1.0"
---

# Web Fetch

Read any web page and get clean text content. No API key needed.

## Tool

```bash
python3 skills/web-fetch/scripts/fetch.py "URL" [--max-chars N]
```
- `--max-chars N`: Maximum characters to return (default: 12000, max: 50000)
- Supports HTML (auto-extracts text), JSON (pretty-printed), and plain text
- Auto-detects encoding, strips scripts/styles/navigation noise

## When to Use

- Read a specific URL you already know (from search results, user-provided links, documentation pages)
- Get the full text of an article when a snippet is insufficient
- Inspect API responses or JSON endpoints
- Read documentation, changelogs, release notes at a known URL
