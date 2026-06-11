"""MkDocs macros for dynamic documentation variables."""

import os
import subprocess


def define_env(env):
    """Expose git_branch for GitHub links in docs-site markdown."""
    branch = os.environ.get("GIT_BRANCH")
    if not branch:
        try:
            branch = subprocess.check_output(
                ["git", "rev-parse", "--abbrev-ref", "HEAD"],
                text=True,
            ).strip()
        except Exception:
            branch = "main"
    env.variables["git_branch"] = branch
