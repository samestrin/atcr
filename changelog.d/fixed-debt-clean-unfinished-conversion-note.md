- `atcr debt clean` now says on stderr when the store holds a conversion that
  could not be finished, as `atcr debt list` already does, so a reader knows
  some items may be counted twice and to run `atcr debt convert`. Stdout stays
  one JSON or TOON document. (epic 158 T2)
