"""Identify an installed Linux plugin subprocess without accepting duplicates."""
from pathlib import Path


def plugin_process_identity(executable):
    """Return PID/start time, None for no match, or fail on duplicate processes."""
    executable = executable.resolve()
    identities = []
    for link in Path("/proc").glob("[0-9]*/exe"):
        try:
            target = Path(str(link.readlink()).removesuffix(" (deleted)"))
            if target == executable:
                fields = (link.parent / "stat").read_text().rsplit(")", 1)[1].split()
                identities.append((int(link.parent.name), fields[19]))
        except (OSError, ValueError):
            continue
    if len(identities) > 1:
        raise RuntimeError(f"multiple plugin processes: {identities}")
    return identities[0] if identities else None
