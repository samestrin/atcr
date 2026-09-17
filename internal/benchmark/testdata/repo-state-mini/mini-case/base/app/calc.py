def total(items):
    s = 0
    for i in items:
        s += i
    return s


def average(items):
    return total(items) / len(items)
