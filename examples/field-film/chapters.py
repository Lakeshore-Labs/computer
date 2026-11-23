"""Write FFmpeg chapter metadata from the public edit list."""
import json
import sys
from pathlib import Path


def chapter_metadata(chapters, frame_rate):
    lines = [";FFMETADATA1", "title=Computer field film"]
    for chapter in chapters:
        start = round(chapter["start"] * frame_rate)
        end = start + sum(shot["frames"] for shot in chapter["shots"])
        title = chapter["title"]
        for special in ("\\", "=", ";", "#", "\n"):
            title = title.replace(special, "\\" + special)
        lines.extend(("[CHAPTER]", f"TIMEBASE=1/{frame_rate}", f"START={start}", f"END={end}", f"title={title}"))
    return "\n".join(lines) + "\n"


if __name__ == "__main__":
    print(chapter_metadata(json.loads(Path(sys.argv[1]).read_text()), 24), end="")
