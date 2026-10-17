"""The Colab notebook only drives the scripts, so its calls must parse against their CLIs.

Nothing here runs the notebook: it needs a Colab GPU and a mounted Drive.
"""

import argparse
import importlib
import json
import re
import shlex
from pathlib import Path

import pytest
from conftest import SCRIPTS

NOTEBOOK = SCRIPTS / "embed_colab.ipynb"
SETTINGS_HEADER = "# Settings"


def code_cells() -> list[str]:
    notebook = json.loads(NOTEBOOK.read_text(encoding="utf-8"))
    return [
        "".join(cell["source"])
        for cell in notebook["cells"]
        if cell["cell_type"] == "code"
    ]


def settings() -> dict[str, object]:
    """Run the notebook's settings cell, which holds plain assignments and nothing else."""
    (cell,) = [source for source in code_cells() if source.startswith(SETTINGS_HEADER)]
    namespace: dict[str, object] = {}
    exec(cell, {}, namespace)  # noqa: S102  # the notebook's own settings cell, plain assignments
    return namespace


def script_calls() -> list[tuple[str, argparse.Namespace]]:
    calls = []
    namespace = settings()
    for source in code_cells():
        joined = re.sub(r"\\\n\s*", " ", source)
        for line in joined.splitlines():
            if not line.startswith("!python "):
                continue
            argv = shlex.split(line.removeprefix("!").format_map(namespace))
            script = Path(argv[1]).name
            module = importlib.import_module(Path(script).stem)
            try:
                calls.append((script, module.parse_args(argv[2:])))
            except SystemExit:
                pytest.fail(f"{script} rejects the notebook's arguments: {argv[2:]}")
    return calls


def test_every_script_call_in_the_notebook_parses_against_its_cli():
    calls = script_calls()

    assert {script for script, _ in calls} >= {"embed_corpus.py"}


def test_the_notebook_embeds_a_package_on_the_gpu_with_checkpoints_on_drive():
    (args,) = [args for script, args in script_calls() if script == "embed_corpus.py"]

    assert args.package.name in {"datapackage.json", "slim.datapackage.json"}
    assert args.device == "cuda"
    assert str(args.checkpoint_dir).startswith("/content/drive/")
    assert str(args.output_root).startswith("/content/drive/")
