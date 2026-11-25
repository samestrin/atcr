- `atcr debate` (and `review --debate`, `atcr_debate`) now sends every seat
  under the project's `secret_guard` from `.atcr/config.yaml`, which overrides
  the registry's as it does for a review; an explicit project `off` is honoured.
  An unreadable or invalid project config fails the debate instead of being
  ignored. (epic 134 T2)
