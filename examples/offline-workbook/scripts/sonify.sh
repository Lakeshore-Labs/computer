set -eo pipefail
python3 - <<'PY'
import csv, math, wave, array
from pathlib import Path

rows = [r for r in csv.DictReader(open("/source/day.csv")) if r["hour"]]
rate = 16000
timestep = 0.62
voices = [("Pier", 261.63), ("Museum", 329.63), ("North", 392.0)]
frames = array.array("h")
for row in rows:
    for i in range(round(rate * timestep)):
        t = i / rate
        envelope = (
            min(1, t / 0.025) * math.exp(-t * 4.2) * min(1, (timestep - t) / 0.07)
        )
        sample = sum(
            int(row[route]) / 65 * math.sin(2 * math.pi * hz * t)
            for route, hz in voices
        )
        frames.append(round(7800 * envelope * sample))
with wave.open("harbor.wav", "wb") as f:
    f.setparams((1, 2, rate, 0, "NONE", "not compressed"))
    f.writeframes(frames.tobytes())
with wave.open("harbor.wav", "rb") as f:
    assert f.getnframes() == len(frames)
    print(
        f"{len(rows)} departures · {f.getnframes() / rate:.2f} seconds · {f.getframerate()} Hz"
    )
ink = ["#287367", "#c45f45", "#6461a4"]
out = [
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 390"><rect width="1000" height="390" fill="#fbf6e8"/><g font-family="sans-serif" fill="#224b40"><text x="30" y="58" font-size="35" font-family="serif">Hear the timetable</text><text x="30" y="93" font-size="17">Each column is one chord. Bar height controls the loudness of each route.</text>'
]
for i, row in enumerate(rows):
    x = 40 + i * 118
    out.append(f'<text x="{x}" y="305" font-size="17">{row["hour"]}</text>')
    for j, (route, hz) in enumerate(voices):
        val = int(row[route])
        out.append(
            f'<rect x="{x + 22 * j}" y="{270 - val * 2.2:.1f}" width="16" height="{val * 2.2:.1f}" fill="{ink[j]}"/>'
        )
for j, (route, hz) in enumerate(voices):
    x = 40 + j * 310
    out.append(
        f'<circle cx="{x}" cy="354" r="7" fill="{ink[j]}"/><text x="{x + 16}" y="359" font-size="16">{route} · {hz:g} Hz</text>'
    )
Path("harbor-score.svg").write_text("".join(out) + "</g></svg>")
PY
