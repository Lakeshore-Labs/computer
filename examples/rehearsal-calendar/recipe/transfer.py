"""Serialize the studio event for transfer between calendar applications."""

import icalendar


def portable_event(source):
    calendar = icalendar.Calendar.from_ical(source)
    for event in calendar.walk("VEVENT"):
        for key in ("DTSTART", "DTEND"):
            value = event.get(key)
            if value is not None and value.to_ical().endswith(b"Z"):
                value.params.pop("TZID", None)
    return calendar.to_ical()
