#!/usr/bin/env python3
"""Says what an artifact or a card is made of, so a deficiency can be named.

  measure.py <file> [<file> ...]

Give it the content an agent showed (items.sh dumps it). It counts what the
agent used; it does not judge the result. Compare the counts with what the
person wanted: see the table in SKILL.md.
"""
import re
import sys


def measure(path: str) -> None:
    text = open(path, encoding="utf-8").read()
    count = lambda pattern: len(re.findall(pattern, text, re.S))
    inner = lambda tag: sum(len(body) for body in re.findall(rf"<{tag}[^>]*>(.*?)</{tag}>", text, re.S))
    is_html = bool(re.search(r"<(div|section|main|style|script|pre|svg|p|h[1-6])\b", text))
    # What a page's own code does, as opposed to what its prose mentions.
    code = "\n".join(re.findall(r"<script[^>]*>(.*?)</script>", text, re.S))
    print(f"{path}: {len(text.encode())} bytes, {'HTML' if is_html else 'Markdown or text'}")
    rows = [
        ("Mermaid blocks (<pre class=\"mermaid\">)", count(r"<pre[^>]*class=\"[^\"]*\bmermaid\b")),
        ("Mermaid fences (```mermaid)", count(r"```mermaid")),
        ("Diagram kinds", ", ".join(sorted(set(re.findall(r"(?:<pre[^>]*mermaid[^>]*>|```mermaid)\s*([A-Za-z-]+)", text)))) or "-"),
        ("legato.diagrams calls", count(r"legato\.diagrams\s*\(")),
        ("onNode handlers", count(r"\bonNode\b")),
        ("legato.selectNode calls", count(r"legato\.selectNode\s*\(")),
        ("legato.proposeMessage calls", count(r"legato\.proposeMessage\s*\(")),
        ("Named node colors", ", ".join(sorted(set(re.findall(r":::(blue|cyan|green|amber|red|pink|purple|gray)\b", text)))) or "-"),
        ("classDef lines", count(r"\bclassDef\b")),
        ("Loads or runs its own Mermaid", count(r"mermaid(\.min)?\.(m?js)|mermaid\.(run|initialize|render)\s*\(")),
        ("External scripts", ", ".join(re.findall(r"<script[^>]*\bsrc=\"([^\"]+)\"", text)) or "-"),
        ("Inline <svg>", count(r"<svg\b")),
        ("<style> bytes", inner("style")),
        ("<script> bytes", inner("script")),
        ("Buttons", count(r"<button\b")),
        ("<details>", count(r"<details\b")),
        ("Tables (HTML or Markdown rows)", count(r"<table\b") + count(r"^\|.*\|\s*$\n^\|[ :|-]+\|\s*$")),
        ("Arrows typed as text", count(r"[→↓↑←⇒⇨↳]")),
        ("Script uses storage, fetch or a worker (fails in the frame)", len(re.findall(r"localStorage|sessionStorage|\bfetch\s*\(|new Worker\s*\(", code))),
    ]
    width = max(len(name) for name, _ in rows)
    for name, value in rows:
        print(f"  {name.ljust(width)}  {value}")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    for file in sys.argv[1:]:
        measure(file)
