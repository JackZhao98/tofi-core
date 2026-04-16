"""Shared helper for reporting successful third-party API calls back to the
Tofi server's internal usage endpoint.

The server injects three env vars when a skill's resolved secret is a known
service provider (e.g. BRAVE_API_KEY → brave_search):

    TOFI_USAGE_URL       loopback endpoint (e.g. http://127.0.0.1:8321/api/v1/internal/usage)
    TOFI_USAGE_TOKEN     opaque 64-char token valid for ~2h
    TOFI_USAGE_PROVIDER  provider id the token was issued for

report_usage() is best-effort: any failure is swallowed so a callback hiccup
never breaks the user-visible search result.
"""
from __future__ import annotations

import json
import os
import urllib.error
import urllib.request


def report_usage(units: int = 1) -> None:
    """Fire-and-forget usage callback. Safe to call even when not in a Tofi
    agent context (no-ops if the env vars aren't set)."""
    url = os.environ.get("TOFI_USAGE_URL", "")
    token = os.environ.get("TOFI_USAGE_TOKEN", "")
    provider = os.environ.get("TOFI_USAGE_PROVIDER", "")
    if not url or not token:
        return
    body = json.dumps(
        {"token": token, "provider": provider, "units": max(1, int(units))}
    ).encode("utf-8")
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    try:
        urllib.request.urlopen(req, timeout=3).read()
    except (urllib.error.URLError, OSError, ValueError):
        # Never surface to the user — the skill's primary result already
        # succeeded; failing to log usage is a silent degradation.
        pass
