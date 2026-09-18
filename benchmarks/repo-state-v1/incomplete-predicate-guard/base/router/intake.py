"""Event intake."""

from router.dispatch import dispatch


def handle(raw_events):
    """Dispatch every inbound event, returning the handler for each."""
    return [dispatch(event) for event in raw_events]
