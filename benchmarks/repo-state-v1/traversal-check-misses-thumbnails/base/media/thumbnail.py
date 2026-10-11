"""Serve the cached thumbnail of a stored upload."""
import os

THUMB_ROOT = "/srv/uploads/.thumbs"


def thumbnail_path(name):
    """Return the on-disk path of the thumbnail the client asked for by name."""
    return os.path.join(THUMB_ROOT, name)
