"""Plan rate lookup."""

DEFAULT_RATE = 1.0

RATES = {
    "free": 0.0,
    "team": 2.5,
    "enterprise": 9.0,
}


def lookup_rate(plan):
    """Return the per-unit rate for plan.

    An unrecognised plan falls back to DEFAULT_RATE, so a new plan name rolled
    out ahead of its rate entry still bills at something sane.
    """
    return RATES.get(plan, DEFAULT_RATE)
