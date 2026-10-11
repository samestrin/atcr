from dataclasses import dataclass


@dataclass
class Project:
    id: str
    name: str
    owner: str
    archived: bool = False
