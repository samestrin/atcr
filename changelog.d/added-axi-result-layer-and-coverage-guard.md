- Contributors adding a TOON mode to a command get one shared result shape
  (`ok`, summary fields, `count` and `items` for lists, then `help[]`), with a
  definitive empty state: `count: 0` and a `help[]` line on how to get data,
  never silence. A test now fails for any visible command that has no AXI
  mode, is not a written exception, and is not on the scheduled gap list.
  No command's output changes yet. (epic 165.2 T1)
