# Fixture case `mini-case`

A minimal `repo-state-v1` case used by the Go test suite, not a scored benchmark
case. It exists so the loader, materializer, matcher and CLI wiring can be tested
against a case whose numbers are known exactly, without depending on the shipped
suite's content staying fixed.

It plants one `outside_diff: true` finding (`app/calc.py:12`, a context line the
diff does not touch) and one `outside_diff: false` finding (`app/calc.py:8`, an
added line), so a test can tell the two halves of the metric apart.

Both cited lines sit inside the Epic 14.1 grounding window, so a reviewer citing
either one reaches the pool rather than being dropped before scoring. That is
deliberate: this fixture tests the matcher, not the grounding gate.
