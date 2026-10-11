class ProjectStore:
    """In-memory project store. load() returns the stored object itself."""

    def __init__(self):
        self._projects = {}

    def load(self, project_id):
        return self._projects[project_id]

    def save(self, project):
        self._projects[project.id] = project
