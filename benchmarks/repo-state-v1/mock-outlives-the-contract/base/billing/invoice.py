"""Invoice totals."""

from billing.rates import lookup_rate


def invoice_total(plan, units):
    """Return the amount to bill for units on plan.

    Every plan resolves to a rate, so there is no unknown-plan branch here.
    """
    rate = lookup_rate(plan)
    return rate * units
