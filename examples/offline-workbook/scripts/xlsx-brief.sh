set -eo pipefail
python3 - <<'PY'
import csv, xlsxwriter
from openpyxl import load_workbook

rows = [r for r in csv.reader(open("/source/day.csv")) if r]
with xlsxwriter.Workbook("timetable.xlsx") as book:
    sheet = book.add_worksheet("Boardings")
    head = book.add_format(
        {"bold": True, "bg_color": "#215D59", "font_color": "#FFFFFF"}
    )
    sheet.write_row(0, 0, rows[0] + ["Total"], head)
    sheet.set_column("A:E", 16)
    for i, row in enumerate(rows[1:], 1):
        values = [int(v) for v in row[1:]]
        sheet.write_row(i, 0, [row[0], *values])
        sheet.write_formula(i, 4, f"=SUM(B{i + 1}:D{i + 1})", None, sum(values))
    chart = book.add_chart({"type": "line"})
    for j in range(1, 4):
        chart.add_series(
            {
                "name": rows[0][j],
                "categories": ["Boardings", 1, 0, 8, 0],
                "values": ["Boardings", 1, j, 8, j],
            }
        )
    chart.set_title({"name": "Traffic by departure"})
    sheet.insert_chart("G2", chart)
r = load_workbook("timetable.xlsx", data_only=True, read_only=True)
print(r.sheetnames[0], "·", r.active.max_row - 1, "departures")
print("18:00 total:", r.active["E8"].value)
PY
