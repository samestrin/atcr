"""Serve a stored upload back to the client."""
import os

UPLOAD_ROOT = "/srv/uploads"


def download_path(name):
    """Return the on-disk path of the upload the client asked for by name."""
    return os.path.join(UPLOAD_ROOT, name)
