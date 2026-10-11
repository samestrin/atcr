"""Refund owed when a customer cancels a subscription mid-period."""


def refund_amount(paid, days_used, days_in_period):
    """Return the unused share of paid (a Decimal), rounded to the cent."""
    unused = paid * (days_in_period - days_used) / days_in_period
    return round(unused, 2)
