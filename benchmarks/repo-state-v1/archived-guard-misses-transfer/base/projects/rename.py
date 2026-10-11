"""Rename endpoint for projects."""


def rename_project(store, project_id, new_name):
    """Rename a project and persist it."""
    project = store.load(project_id)
    project.name = new_name.strip()
    store.save(project)
    return project
