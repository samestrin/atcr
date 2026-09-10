"""The only caller of Cursor.begin()."""

from streamer.cursor import Cursor


def drain(batch, cursor=None):
    cursor = cursor or Cursor()
    cursor.load(batch)
    return cursor.begin()
