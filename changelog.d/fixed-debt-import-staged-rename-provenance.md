- `atcr debt import` of a README staged as a `git mv` (the migration recipe's
  `README.import.md`) records the original README path in each record's
  `source_report.path` and the HEAD commit holding it in a new optional
  `source_report.commit`, so the old list can still be found after the renamed
  copy is deleted. With no staged rename the path stays the import argument and
  no commit is written. Importing the same table again still writes nothing.
  (epic 122.1 T1)
