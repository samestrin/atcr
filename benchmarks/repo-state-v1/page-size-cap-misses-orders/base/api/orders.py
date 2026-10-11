"""GET /orders."""
from api.paging import DEFAULT_PAGE_SIZE


def list_orders(db, query):
    """Return one page of orders; the page size is the client's ?limit=."""
    limit = int(query.get("limit", DEFAULT_PAGE_SIZE))
    return db.fetch("SELECT id, user_id, total FROM orders ORDER BY id LIMIT ?", limit)
