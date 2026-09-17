"""Evening escalation queue."""

from store.pending import read_pending


def bump_stale_evening_to_today(queue):
    """Move last night's unanswered escalations into today's queue.

    Step 1 reconciles the queue against the pending store: a queue entry whose
    pending record is gone was answered overnight, so the linked entry is
    durably removed. Step 2 bumps whatever survived.
    """
    pending = read_pending()
    live_ids = {record["id"] for record in pending}

    survivors = []
    for entry in queue:
        if entry["pending_id"] not in live_ids:
            delete_entry(entry)
            continue
        survivors.append(entry)

    for entry in survivors:
        entry["due"] = "today"
    return survivors


def delete_entry(entry):
    """Remove a confirmed escalation from durable storage. Not reversible."""
    entry["deleted"] = True
