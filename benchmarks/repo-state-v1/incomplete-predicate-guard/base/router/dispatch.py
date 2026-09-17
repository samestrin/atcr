"""Event dispatch."""

KINDS = ("email", "sms", "push", "webhook")

HANDLERS = {
    "email": "send_email",
    "sms": "send_sms",
    "push": "send_push",
    "webhook": "post_webhook",
}


def dispatch(event):
    """Route event to the handler for its kind.

    Every member of KINDS has an entry in HANDLERS, so a well-formed event
    always resolves.
    """
    kind = event["kind"]
    return HANDLERS[kind]
