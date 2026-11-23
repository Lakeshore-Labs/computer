"""Runs inside Computer's guest (CPython on WASI, standard library only).

  card.py make  --epub BOOK --where PLACE --when TIME --out CARD
  card.py amend --prev CARD --when TIME --out CARD     (one field changes: the time)

The card is written from facts read out of the book itself: its title, chapter count and measurement table.
"""
import argparse, re, sys, zipfile


def book_info(path):
    z = zipfile.ZipFile(path)
    if z.read("mimetype") != b"application/epub+zip":
        sys.exit("not an EPUB: wrong mimetype")
    opf = z.read("EPUB/package.opf").decode()
    title = re.search(r"<dc:title>(.*?)</dc:title>", opf).group(1)
    chapters = len(re.findall(r"<itemref ", opf))
    page = z.read("EPUB/measure.xhtml").decode()
    rows = [(a, b) for a, b in re.findall(r'<th scope="row">(.*?)</th><td>(.*?)</td>', page)]
    return title, chapters, rows


def make(a):
    title, chapters, rows = book_info(a.epub)
    size = {n: float(v.replace(",", "")) for n, v in rows}
    big = max(size, key=size.get)
    small = min(size, key=size.get)
    names = ", ".join(n for n, _ in rows)
    lines = [
        "# Take-home reading card",
        "Gull Island Library (a made-up library for this exhibit)",
        "",
        "Edition: 1",
        f"Book: {title}",
        f"Inside: {chapters} chapters, {len(rows)} moons ({names})",
        f"Page 2 table: largest is {big} at {dict(rows)[big]} km, smallest is {small} at {dict(rows)[small]} km.",
        "",
        "Before we meet:",
        "1. Open the cover page and find the credited spacecraft picture.",
        "2. On page 2, put the four moons in order by radius.",
        "3. Bring one question for the reading circle.",
        "",
        f"Where: {a.where}",
        f"When: {a.when}",
    ]
    open(a.out, "w").write("\n".join(lines) + "\n")


def amend(a):
    lines = open(a.prev).read().rstrip("\n").split("\n")
    out, old = [], None
    for ln in lines:
        if ln.startswith("Edition: "):
            ln = f"Edition: {int(ln.split(': ')[1]) + 1}"
        elif ln.startswith("When: "):
            old = ln[len("When: "):]
            ln = f"When: {a.when}"
        elif ln.startswith("Changed: "):
            continue
        out.append(ln)
    if old is None:
        sys.exit("previous card has no When line")
    out.append(f"Changed: time only. It was {old}.")
    open(a.out, "w").write("\n".join(out) + "\n")


p = argparse.ArgumentParser()
sub = p.add_subparsers(dest="cmd", required=True)
m = sub.add_parser("make")
m.add_argument("--epub", required=True)
m.add_argument("--where", required=True)
m.add_argument("--when", required=True)
m.add_argument("--out", required=True)
m.set_defaults(run=make)
x = sub.add_parser("amend")
x.add_argument("--prev", required=True)
x.add_argument("--when", required=True)
x.add_argument("--out", required=True)
x.set_defaults(run=amend)
args = p.parse_args()
args.run(args)
