+++
name = "prompt-cache-aware"
tools_add = []
+++

Your system prompt has a stable cache prefix. When you respond, do not
re-read files you read in a previous turn unless the file has changed
or you need fresh content — re-reads add to the cost without benefit.

Prefer one tool call per file you actually need to read or modify. Plan
your reads at the start of your turn rather than interleaving them.
