---
name: web-search
description: Search the web for real-time information using Brave Search or DuckDuckGo fallback
version: "4.0"
required_secrets: ["BRAVE_API_KEY"]
tools:
  - name: web_search
    description: "Search the web and return page content from top results. Default choice for general queries."
    script: "scripts/search.py"
    params:
      query:
        type: string
        description: "Search query"
        required: true
      count:
        type: integer
        description: "Number of sources (default 5, max 20)"
      tokens:
        type: integer
        description: "Max tokens per source (default 8192, max 32768)"
      freshness:
        type: string
        description: "Time filter: pd/pw/pm/py or YYYY-MM-DDtoYYYY-MM-DD"
      search_lang:
        type: string
        description: "Content language (default: en)"
      result_filter:
        type: string
        description: "Filter types: discussions, faq, infobox, news, web, locations"
  - name: web_news
    description: "Search recent news articles with dates and sources. Use for breaking events and time-sensitive queries."
    script: "scripts/news.py"
    params:
      query:
        type: string
        description: "News search query"
        required: true
      count:
        type: integer
        description: "Number of results (default 5)"
      freshness:
        type: string
        description: "Time filter (default: pw)"
      search_lang:
        type: string
        description: "Content language (default: en)"
      extra_snippets:
        type: boolean
        description: "Include extra excerpts per article"
  - name: web_summarize
    description: "Get AI-powered summary of web search results with sources."
    script: "scripts/summarize.py"
    params:
      query:
        type: string
        description: "Topic to summarize"
        required: true
---

Search tools for finding web content. Pick the right tool:
- **web_search**: General queries, documentation, analysis. Default choice.
- **web_news**: Breaking news, time-sensitive events, stock market. ALWAYS use for "latest/recent" queries.
- **web_summarize**: Quick factual overview.

Tips:
- Use `site:domain.com` in query to restrict to a specific website (Brave only, not DuckDuckGo).
- Decompose complex questions into multiple searches.
- For news/current events, ALWAYS use web_news, not web_search.
- Cite sources inline: `[Site Name](URL)`.
