"""Print a one-day dispatch packet with the Python bundled for Computer."""
import json
from collections import defaultdict
from pathlib import Path

from fpdf import FPDF
from openpyxl import load_workbook
from pypdf import PdfReader
import xlsxwriter

rows = json.loads(Path("/input/boardings.json").read_text())
departures = defaultdict(dict)
for row in rows:
    departures[row["slot"]][row["route"]] = row["riders"]
routes = sorted({row["route"] for row in rows})
times = sorted(departures)
totals = {name: sum(departures[t][name] for t in times) for name in routes}
ranked = sorted(routes, key=totals.get, reverse=True)
colors = {"Pier": "#28695F", "Museum": "#BD573B", "North": "#5E628D"}
ink = (27, 64, 58)

with xlsxwriter.Workbook("dispatch.xlsx") as book:
    sheet = book.add_worksheet("Boardings")
    heading = book.add_format({"bg_color": "#194039", "font_color": "#FFFFFF", "bold": True})
    sheet.freeze_panes(1, 1)
    sheet.set_column(0, len(routes) + 1, 15)
    sheet.write_row(0, 0, ["Departure", *routes, "Total"], heading)
    for i, t in enumerate(times, start=1):
        counts = [departures[t][name] for name in routes]
        sheet.write_row(i, 0, [t, *counts])
        first = xlsxwriter.utility.xl_rowcol_to_cell(i, 1)
        last = xlsxwriter.utility.xl_rowcol_to_cell(i, len(routes))
        sheet.write_formula(i, len(routes) + 1, f"=SUM({first}:{last})", None, sum(counts))
    chart = book.add_chart({"type": "line"})
    for i, name in enumerate(routes, start=1):
        chart.add_series({"name": name, "categories": ["Boardings", 1, 0, len(times), 0],
                          "values": ["Boardings", 1, i, len(times), i],
                          "line": {"color": colors[name], "width": 2.5}})
    chart.set_title({"name": "Passengers by departure"})
    sheet.insert_chart("G2", chart)

pdf = FPDF(unit="mm", format="A4")
pdf.add_page()
pdf.set_fill_color(249, 246, 233)
pdf.rect(0, 0, 210, 297, "F")
pdf.set_fill_color(*ink)
pdf.rect(0, 0, 210, 82, "F")
pdf.set_text_color(195, 221, 195)
pdf.set_font("Helvetica", size=10)
pdf.text(16, 18, "HARBORSIDE  /  FERRY OPERATIONS")
pdf.set_text_color(255, 252, 234)
pdf.set_font("Times", size=39)
pdf.text(15, 42, "The daily crossing")
pdf.set_font("Helvetica", size=14)
pdf.text(16, 61, f"{len(rows)} departures     /     {sum(totals.values())} passenger boardings")
pdf.set_text_color(*ink)
pdf.set_font("Times", size=24)
pdf.text(16, 103, "Boardings by route")
for i, name in enumerate(ranked):
    y = 119 + i * 14
    pdf.set_font("Helvetica", size=12)
    pdf.text(16, y, name)
    pdf.set_fill_color(*tuple(bytes.fromhex(colors[name][1:])))
    pdf.rect(56, y - 4, 103 * totals[name] / max(totals.values()), 5, "F")
    pdf.text(172, y, str(totals[name]))
pdf.set_font("Times", size=24)
pdf.text(16, 179, "Departure ledger")
pdf.set_font("Helvetica", "B", 10)
for j, val in enumerate(["Time", *routes, "Total"]):
    pdf.text(16 + j * 39, 193, val)
pdf.set_font("Helvetica", size=10)
for i, t in enumerate(times):
    y = 204 + i * 7
    counts = [departures[t][name] for name in routes]
    for j, val in enumerate([t, *counts, sum(counts)]):
        pdf.text(16 + j * 39, y, str(val))
pdf.set_font("Helvetica", size=9)
pdf.text(16, 283, "Source: synthetic timetable. Totals are passenger boardings for one day.")
pdf.output("dispatch.pdf")

with open("dispatch.xlsx", "rb") as formulas_file, open("dispatch.xlsx", "rb") as values_file:
    formulas = load_workbook(formulas_file, read_only=True, data_only=False).active
    cached = load_workbook(values_file, read_only=True, data_only=True).active
    for number, (formula_row, value_row) in enumerate(zip(formulas.values, cached.values, strict=True)):
        if number == 0:
            assert formula_row == ("Departure", *routes, "Total")
            continue
        slot = times[number - 1]
        expected = tuple(departures[slot][name] for name in routes)
        last = xlsxwriter.utility.xl_col_to_name(len(routes))
        row = number + 1
        assert formula_row == (slot, *expected, f"=SUM(B{row}:{last}{row})")
        assert value_row == (slot, *expected, sum(expected))
    assert number == len(times)
page = PdfReader("dispatch.pdf").pages[0].extract_text()
assert "The daily crossing" in page
assert f"{sum(totals.values())} passenger boardings" in page
for name, total in totals.items():
    assert name in page and str(total) in page
print(f"Checked {len(times)} workbook formulas and their cached values against the input.")
print(f"Checked the PDF title and all {len(totals)} route totals.")
