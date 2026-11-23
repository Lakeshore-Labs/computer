"""Runs INSIDE the Computer guest (WASI Python). Edition 1 writes the whole synthetic library;
edition 2 is the librarian's amendment: it rewrites only hours.csv."""
import csv, random, sys
ed, root = sys.argv[1], sys.argv[2]
HOURS = [("Monday","09:00","17:00"),("Tuesday","09:00","17:00"),("Wednesday","09:00","20:00"),
         ("Thursday","09:00","17:00"),("Friday","09:00","17:00"),("Saturday","10:00","14:00"),("Sunday","closed","closed")]
if ed == "1":
    rng = random.Random(20261122)
    a = ["Quiet","Amber","Paper","Northern","Salt","Velvet","Hollow","Lantern","Copper","Winter"]
    b = ["Orchard","Atlas","Harbour","Almanac","Garden","Compass","Archive","Bridge","Meadow","Clock"]
    names = ["A. Ferrow","M. Quill","T. Ostrander","R. Vale","J. Marlowe","S. Idris","P. Keene","L. Brandt"]
    shelves = ["Atlas","Fiction","Gardens","History","Poetry"]
    seen, rows = set(), []
    while len(rows) < 24:
        t = f"The {rng.choice(a)} {rng.choice(b)}"
        if t in seen: continue
        seen.add(t)
        rows.append((f"SYN-{len(rows)+1:03d}", t, rng.choice(names), rng.choice(shelves), rng.randint(1,4), rng.randint(1950,2020)))
    rows.sort(key=lambda r: (r[3], r[1]))
    with open(root+"/books.csv","w",newline="") as f:
        w = csv.writer(f, lineterminator="\n"); w.writerow(["id","title","author","shelf","copies","year"]); w.writerows(rows)
    with open(root+"/notice.md","w") as f:
        f.write("# Lakeshore Reading Room (synthetic)\n\nWelcome. Every title, author and hour in this archive is invented.\nBorrow up to four books for fourteen days.\n")
    hours = HOURS
else:
    hours = [(d,o,c) for d,o,c in HOURS]
    hours[4] = ("Friday","09:00","19:00"); hours[5] = ("Saturday","09:00","16:00"); hours[6] = ("Sunday","12:00","16:00")
with open(root+"/hours.csv","w",newline="") as f:
    w = csv.writer(f, lineterminator="\n"); w.writerow(["day","opens","closes"]); w.writerows(hours)
print("edition", ed, "wrote", root)
