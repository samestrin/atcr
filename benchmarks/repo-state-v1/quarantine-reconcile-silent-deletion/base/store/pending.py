"""Durable store for pending escalations."""

import json
import os

STORE_PATH = "pending_today.json"


class CorruptStore(Exception):
    """Raised when the store file is present but cannot be decoded."""


def read_pending():
    """Return every pending escalation record.

    Raises CorruptStore when the file exists but does not decode, so a caller
    cannot mistake a damaged store for an empty one.
    """
    if not os.path.exists(STORE_PATH):
        return []
    with open(STORE_PATH) as fh:
        raw = fh.read()
    try:
        return json.loads(raw)
    except ValueError as exc:
        raise CorruptStore(STORE_PATH) from exc


def write_pending(records):
    """Replace the store contents with records."""
    with open(STORE_PATH, "w") as fh:
        json.dump(records, fh)
