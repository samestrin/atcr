"""GET /users."""
from api.paging import DEFAULT_PAGE_SIZE


def list_users(db, query):
    """Return one page of users; the page size is the client's ?limit=."""
    limit = int(query.get("limit", DEFAULT_PAGE_SIZE))
    return db.fetch("SELECT id, email FROM users ORDER BY id LIMIT ?", limit)
