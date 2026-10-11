"""Ownership-transfer endpoint for projects."""


def transfer_project(store, project_id, new_owner):
    """Hand a project to a new owner and persist it."""
    project = store.load(project_id)
    project.owner = new_owner
    store.save(project)
    return project
