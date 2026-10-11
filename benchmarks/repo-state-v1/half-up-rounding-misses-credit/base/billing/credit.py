"""Account credit owed when a customer downgrades mid-period."""


def downgrade_credit(price_difference, days_left, days_in_period):
    """Return the credit for the rest of the period (a Decimal), rounded to the cent."""
    share = price_difference * days_left / days_in_period
    return round(share, 2)
