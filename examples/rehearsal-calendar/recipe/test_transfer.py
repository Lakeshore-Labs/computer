import unittest
from pathlib import Path

import icalendar

from transfer import portable_event


class CalendarTransfer(unittest.TestCase):
    def test_portable_calendar_keeps_the_event_and_uses_utc_wire_values(self):
        source = (
            Path(__file__).resolve().parents[1] / "sample" / "rehearsal.ics"
        ).read_bytes()
        result = portable_event(source)
        self.assertEqual(result.count(b"\r\n"), result.count(b"\n"))
        self.assertIn(b"DTSTART:20261129T110000Z\r\n", result)
        self.assertIn(b"DTEND:20261129T114500Z\r\n", result)
        original = icalendar.Calendar.from_ical(source).walk("VEVENT")[0]
        copied = icalendar.Calendar.from_ical(result).walk("VEVENT")[0]
        self.assertEqual(set(original), set(copied))
        for key in original:
            self.assertEqual(original.decoded(key), copied.decoded(key))

    def test_named_local_time_keeps_its_timezone(self):
        source = b"BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:a\nDTSTART;TZID=Europe/London:20261129T110000\nEND:VEVENT\nEND:VCALENDAR\n"
        self.assertIn(
            b"DTSTART;TZID=Europe/London:20261129T110000\r\n", portable_event(source)
        )


if __name__ == "__main__":
    unittest.main()
