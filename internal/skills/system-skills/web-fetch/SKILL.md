---
name: web-fetch
description: Fetch and read any web page using headless Chrome, extracting clean text content from URLs including JavaScript-rendered pages
version: "2.1"
tools:
  - name: web_fetch
    description: "Fetch a URL and return clean text content (no HTML). Uses headless Chrome for JavaScript-rendered pages."
    script: "scripts/fetch.py"
    params:
      url:
        type: string
        description: "URL to fetch"
        required: true
      max_chars:
        type: integer
        description: "Max characters to return (default 8000, max 50000)"
---

Fetch clean text from any URL. Use when you need the actual content of a specific page.
Returns extracted text only — no HTML, no CSS, no scripts.
