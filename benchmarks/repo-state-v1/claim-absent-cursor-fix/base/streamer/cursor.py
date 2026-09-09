"""Batch cursor bookkeeping for the event streamer."""


class Cursor:
    """Tracks the read offset across drains of the event log."""

    def __init__(self, offset=0):
        self._offset = offset
        self._batch = []

    def offset(self):
        return self._offset

    def load(self, batch):
        self._batch = list(batch)

    def _drain_offset(self):
        """Return the offset of the last well-formed record in the batch.

        Returns 0 when the batch holds no well-formed record at all, which the
        caller currently cannot tell apart from a genuine offset of 0.
        """
        last = 0
        for record in self._batch:
            if record.get("offset") is None:
                continue
            last = record["offset"]
        return last

    def begin(self):
        """Start a drain, moving the cursor to the batch's last good offset."""
        self._offset = self._drain_offset()
        return self._offset
