"""The recorded holiday and population checks must reject incorrect source values."""

import unittest
from replay import check


class SourceChecks(unittest.TestCase):
    def test_renamed_public_holiday_fails(self):
        rows = [
            {"date": "2026-10-03", "nameEn": "Wrong date label"},
            {"date": "2026-10-09", "nameEn": "Hangul Day"},
        ]
        self.assertFalse(
            all(check("opencurrent-kr-holidays", {"holidays": rows}, "").values())
        )

    def test_census_subtotals_do_not_validate_the_captured_population(self):
        sample = {
            "people": 1,
            "by_sex": {"all": 1},
            "by_age_band": {"all": 1},
            "by_schooling": {"all": 1},
            "coverage_note": "five districts",
        }
        self.assertFalse(all(check("ghana-census-accra", sample, "").values()))


if __name__ == "__main__":
    unittest.main()
