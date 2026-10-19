# 0003. Read-only, no account features

Date: 2026-10-19
Status: accepted

## Decision

The cli reads stores, categories, search results, products and stock at the user's store. It never adds to a cart, checks out, writes reviews or lists, or changes account settings. MCP tools mirror the read commands only.

## Why

The project exists to answer "where can I buy this material, and is it in stock at my store". Write paths add risk (money, a session scored more strictly) and give that question nothing.
