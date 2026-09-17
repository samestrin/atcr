from billing import invoice


def fake_lookup_rate(plan):
    """Stand in for billing.rates.lookup_rate."""
    return 2.5


def test_unknown_plan_still_bills(monkeypatch):
    monkeypatch.setattr(invoice, "lookup_rate", fake_lookup_rate)

    assert invoice.invoice_total("plan-that-does-not-exist", 4) == 10.0
